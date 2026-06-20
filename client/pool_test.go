package client

import (
	"context"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

// newPipeConn returns a *gosnmp.GoSNMP backed by an in-memory net.Pipe, plus
// the peer end. Closing the GoSNMP closes one side of the pipe, which is
// observable on the peer as a Read returning io.EOF — this lets tests
// verify a connection was actually closed without touching the network.
func newPipeConn(t *testing.T) (*gosnmp.GoSNMP, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	return &gosnmp.GoSNMP{Conn: a}, b
}

func assertClosed(t *testing.T, peer net.Conn) {
	t.Helper()
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	_, err := peer.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("peer.Read() err = %v, want io.EOF (connection should have been closed)", err)
	}
}

func countingFactory(count *atomic.Int32) ConnFactory {
	return func(_ context.Context, _ string) (*gosnmp.GoSNMP, error) {
		count.Add(1)
		return &gosnmp.GoSNMP{}, nil
	}
}

func TestPool_GetDialsWhenEmpty(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials))
	t.Cleanup(func() { _ = p.Close() })

	conn, err := p.Get(context.Background(), "target-a")
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if conn == nil {
		t.Fatal("Get() returned nil connection")
	}
	if got := dials.Load(); got != 1 {
		t.Errorf("dial count = %d, want 1", got)
	}
}

func TestPool_PutThenGetReusesConnection(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials))
	t.Cleanup(func() { _ = p.Close() })

	const target = "target-a"

	conn1, err := p.Get(context.Background(), target)
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}

	p.Put(target, conn1)
	if got := p.IdleLen(target); got != 1 {
		t.Fatalf("IdleLen() after Put = %d, want 1", got)
	}

	conn2, err := p.Get(context.Background(), target)
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if conn2 != conn1 {
		t.Error("Get() did not reuse the pooled connection")
	}
	if got := p.IdleLen(target); got != 0 {
		t.Errorf("IdleLen() after reuse = %d, want 0", got)
	}
	if got := dials.Load(); got != 1 {
		t.Errorf("dial count = %d, want 1 (second Get should have reused, not redialed)", got)
	}

	// Pool size after a full round trip must match the size right after the
	// first Put — i.e. returning a connection and reusing it leaves the
	// pool's idle count exactly where it was.
	p.Put(target, conn2)
	if got := p.IdleLen(target); got != 1 {
		t.Errorf("IdleLen() after second Put = %d, want 1 (pool size should be unchanged across a Get/Put cycle)", got)
	}
}

func TestPool_MaxIdlePerTarget(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials), WithPool(1, time.Minute))
	t.Cleanup(func() { _ = p.Close() })

	const target = "target-a"

	conn1, peer1 := newPipeConn(t)
	conn2, peer2 := newPipeConn(t)
	defer func() { _ = peer1.Close(); _ = peer2.Close() }()

	p.Put(target, conn1)
	if got := p.IdleLen(target); got != 1 {
		t.Fatalf("IdleLen() = %d, want 1", got)
	}

	p.Put(target, conn2)
	if got := p.IdleLen(target); got != 1 {
		t.Fatalf("IdleLen() after exceeding max = %d, want 1 (extra connection should be rejected)", got)
	}
	assertClosed(t, peer2)
}

func TestPool_IdleConnectionExpiresOnGet(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials), WithPool(2, 20*time.Millisecond))
	t.Cleanup(func() { _ = p.Close() })

	const target = "target-a"

	stale, peer := newPipeConn(t)
	defer func() { _ = peer.Close() }()
	p.Put(target, stale)

	time.Sleep(50 * time.Millisecond)

	conn, err := p.Get(context.Background(), target)
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if conn == stale {
		t.Error("Get() returned an expired connection instead of dialing a new one")
	}
	if got := dials.Load(); got != 1 {
		t.Errorf("dial count = %d, want 1 (expired connection should trigger a fresh dial)", got)
	}
	assertClosed(t, peer)
}

func TestPool_BackgroundEvictionRemovesExpired(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials), WithPool(2, 20*time.Millisecond))
	t.Cleanup(func() { _ = p.Close() })

	const target = "target-a"

	conn, peer := newPipeConn(t)
	defer func() { _ = peer.Close() }()
	p.Put(target, conn)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if p.IdleLen(target) == 0 {
			assertClosed(t, peer)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("background eviction did not remove the expired connection in time")
}

func TestPool_GetAfterClose(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials))

	if err := p.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	_, err := p.Get(context.Background(), "target-a")
	if !errors.Is(err, snmperrors.ErrPoolClosed) {
		t.Errorf("Get() after Close() err = %v, want ErrPoolClosed", err)
	}
}

func TestPool_PutAfterClose(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials))

	if err := p.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	conn, peer := newPipeConn(t)
	defer func() { _ = peer.Close() }()

	p.Put("target-a", conn)
	assertClosed(t, peer)
}

func TestPool_PutNilIsNoop(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials))
	t.Cleanup(func() { _ = p.Close() })

	p.Put("target-a", nil)
	if got := p.IdleLen("target-a"); got != 0 {
		t.Errorf("IdleLen() = %d, want 0", got)
	}
}

func TestPool_CloseDrainsIdleAndStopsGoroutine(t *testing.T) {
	var dials atomic.Int32

	baseline := runtime.NumGoroutine()
	p := NewPool(countingFactory(&dials))

	conn1, peer1 := newPipeConn(t)
	conn2, peer2 := newPipeConn(t)
	defer func() { _ = peer1.Close(); _ = peer2.Close() }()

	p.Put("target-a", conn1)
	p.Put("target-b", conn2)

	if err := p.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	assertClosed(t, peer1)
	assertClosed(t, peer2)

	deadline := time.Now().Add(time.Second)
	for {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leak: %d goroutines running, baseline was %d", runtime.NumGoroutine(), baseline)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPool_CloseIsIdempotent(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials))

	if err := p.Close(); err != nil {
		t.Fatalf("first Close() err = %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close() err = %v", err)
	}
}

func TestPool_ConcurrentGetPut(t *testing.T) {
	var dials atomic.Int32
	p := NewPool(countingFactory(&dials), WithPool(4, time.Minute))
	t.Cleanup(func() { _ = p.Close() })

	targets := []string{"target-a", "target-b", "target-c"}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				conn, err := p.Get(context.Background(), target)
				if err != nil {
					t.Errorf("Get() err = %v", err)
					return
				}
				p.Put(target, conn)
			}
		}(targets[i%len(targets)])
	}
	wg.Wait()
}
