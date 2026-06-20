package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

// ConnFactory dials and returns a new, already-connected SNMP session for
// target. Implementations are typically backed by (*gosnmp.GoSNMP).Connect.
type ConnFactory func(ctx context.Context, target string) (*gosnmp.GoSNMP, error)

// idleConn is a pooled connection awaiting reuse.
type idleConn struct {
	conn    *gosnmp.GoSNMP
	expires time.Time
}

// targetPool holds the idle connections for a single target. It has no
// knowledge of dialing, closing, or logging; Pool owns those concerns.
type targetPool struct {
	mu   sync.Mutex
	idle []idleConn
}

// popFresh removes and returns the most recently pushed idle connection if
// it has not expired. Any expired connections encountered first are dropped
// from idle and returned in stale for the caller to close.
func (tp *targetPool) popFresh(now time.Time) (fresh *gosnmp.GoSNMP, stale []*gosnmp.GoSNMP) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	for len(tp.idle) > 0 {
		last := len(tp.idle) - 1
		ic := tp.idle[last]
		tp.idle = tp.idle[:last]
		if now.Before(ic.expires) {
			return ic.conn, stale
		}
		stale = append(stale, ic.conn)
	}
	return nil, stale
}

// push adds conn to idle if the per-target limit has not been reached. It
// reports whether conn was accepted.
func (tp *targetPool) push(conn *gosnmp.GoSNMP, max int, ttl time.Duration) bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if len(tp.idle) >= max {
		return false
	}
	tp.idle = append(tp.idle, idleConn{conn: conn, expires: time.Now().Add(ttl)})
	return true
}

// evict removes and returns idle connections that have expired by now.
func (tp *targetPool) evict(now time.Time) []*gosnmp.GoSNMP {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	var expired []*gosnmp.GoSNMP
	kept := tp.idle[:0]
	for _, ic := range tp.idle {
		if now.Before(ic.expires) {
			kept = append(kept, ic)
			continue
		}
		expired = append(expired, ic.conn)
	}
	tp.idle = kept
	return expired
}

// drain removes and returns every idle connection.
func (tp *targetPool) drain() []*gosnmp.GoSNMP {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	conns := make([]*gosnmp.GoSNMP, len(tp.idle))
	for i, ic := range tp.idle {
		conns[i] = ic.conn
	}
	tp.idle = nil
	return conns
}

// len reports the current idle connection count.
func (tp *targetPool) len() int {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	return len(tp.idle)
}

// Pool is a per-target pool of reusable SNMP connections. It is safe for
// concurrent use by multiple goroutines.
type Pool struct {
	factory ConnFactory
	cfg     config

	mu      sync.Mutex
	targets map[string]*targetPool
	closed  bool

	evictStop chan struct{}
	evictDone chan struct{}
}

// NewPool creates a connection pool that dials new connections with factory
// and applies opts on top of the package defaults. NewPool starts a
// background goroutine that evicts idle connections past their TTL; it
// exits when Close is called.
func NewPool(factory ConnFactory, opts ...Option) *Pool {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	p := &Pool{
		factory:   factory,
		cfg:       cfg,
		targets:   make(map[string]*targetPool),
		evictStop: make(chan struct{}),
		evictDone: make(chan struct{}),
	}
	go p.evictLoop()
	return p
}

// Get returns an idle connection for target if one is available and still
// fresh, otherwise it dials a new one via the pool's ConnFactory.
func (p *Pool) Get(ctx context.Context, target string) (*gosnmp.GoSNMP, error) {
	if p.isClosed() {
		return nil, snmperrors.ErrPoolClosed
	}

	tp := p.targetFor(target)
	fresh, stale := tp.popFresh(time.Now())
	p.closeAll(target, stale)
	if fresh != nil {
		return fresh, nil
	}

	conn, err := p.factory(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("gosnmp-plus/client: dial %s: %w", target, err)
	}
	return conn, nil
}

// Put returns conn to the pool for reuse against target. If the pool is
// closed or the per-target idle limit has been reached, conn is closed
// immediately instead.
func (p *Pool) Put(target string, conn *gosnmp.GoSNMP) {
	if conn == nil {
		return
	}

	if p.isClosed() {
		p.closeAll(target, []*gosnmp.GoSNMP{conn})
		return
	}

	tp := p.targetFor(target)
	if !tp.push(conn, p.cfg.maxIdlePerTarget, p.cfg.idleTimeout) {
		p.closeAll(target, []*gosnmp.GoSNMP{conn})
	}
}

// IdleLen reports how many idle connections are currently pooled for
// target. It exists for tests and diagnostics.
func (p *Pool) IdleLen(target string) int {
	return p.targetFor(target).len()
}

// Close closes every idle connection in the pool and stops the
// idle-eviction goroutine. It does not affect connections currently checked
// out via Get; callers must close those themselves. Close is idempotent.
func (p *Pool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	targets := make(map[string]*targetPool, len(p.targets))
	for target, tp := range p.targets {
		targets[target] = tp
	}
	p.mu.Unlock()

	close(p.evictStop)
	<-p.evictDone

	for target, tp := range targets {
		p.closeAll(target, tp.drain())
	}
	return nil
}

func (p *Pool) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *Pool) targetFor(target string) *targetPool {
	p.mu.Lock()
	defer p.mu.Unlock()

	tp, ok := p.targets[target]
	if !ok {
		tp = &targetPool{}
		p.targets[target] = tp
	}
	return tp
}

func (p *Pool) closeAll(target string, conns []*gosnmp.GoSNMP) {
	for _, conn := range conns {
		if err := conn.Close(); err != nil {
			p.cfg.logger.Printf("gosnmp-plus/client: close conn for %s: %v", target, err)
		}
	}
}

// evictLoop periodically closes idle connections that have outlived
// cfg.idleTimeout. It exits when evictStop is closed (from Close).
func (p *Pool) evictLoop() {
	defer close(p.evictDone)

	interval := p.cfg.idleTimeout / 2
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.evictStop:
			return
		case <-ticker.C:
			p.evictExpired()
		}
	}
}

func (p *Pool) evictExpired() {
	p.mu.Lock()
	targets := make(map[string]*targetPool, len(p.targets))
	for target, tp := range p.targets {
		targets[target] = tp
	}
	p.mu.Unlock()

	now := time.Now()
	for target, tp := range targets {
		p.closeAll(target, tp.evict(now))
	}
}
