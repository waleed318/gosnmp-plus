package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
	"github.com/waleed318/gosnmp-plus/state"
	"github.com/waleed318/gosnmp-plus/testdata/agent"
)

func newTestAgent(t *testing.T) *agent.Agent {
	t.Helper()
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("agent.NewAgent() err = %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func newTestClient(t *testing.T, a *agent.Agent, opts ...Option) *Client {
	t.Helper()
	c, err := NewClient(a.Addr(), opts...)
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestNormalizeTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		want    string
		wantErr bool
	}{
		{name: "host only gets default port", target: "192.0.2.1", want: "192.0.2.1:161"},
		{name: "host:port is unchanged", target: "192.0.2.1:1161", want: "192.0.2.1:1161"},
		{name: "empty target errors", target: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTarget(tt.target)
			if tt.wantErr {
				if err == nil {
					t.Fatal("normalizeTarget() err = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeTarget() err = %v", err)
			}
			if got != tt.want {
				t.Errorf("normalizeTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewClient_EmptyTarget(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("NewClient(\"\") err = nil, want error")
	}
}

func TestClient_GetSet_Integration(t *testing.T) {
	a := newTestAgent(t)
	a.SetOID(".1.3.6.1.2.1.1.5.0", "router-1")

	c := newTestClient(t, a)
	ctx := context.Background()

	packet, err := c.Get(ctx, []string{".1.3.6.1.2.1.1.5.0"})
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if len(packet.Variables) != 1 || string(packet.Variables[0].Value.([]byte)) != "router-1" {
		t.Fatalf("Get() = %+v, want value %q", packet.Variables, "router-1")
	}

	err = c.Set(ctx, []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("router-2")},
	})
	if err != nil {
		t.Fatalf("Set() err = %v", err)
	}

	packet, err = c.Get(ctx, []string{".1.3.6.1.2.1.1.5.0"})
	if err != nil {
		t.Fatalf("Get() after Set err = %v", err)
	}
	if string(packet.Variables[0].Value.([]byte)) != "router-2" {
		t.Errorf("Get() after Set = %q, want %q", packet.Variables[0].Value, "router-2")
	}
}

func TestClient_Get_PoolClosedReturnsErrPoolClosed(t *testing.T) {
	a := newTestAgent(t)
	c, err := NewClient(a.Addr())
	if err != nil {
		t.Fatalf("NewClient() err = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	_, err = c.Get(context.Background(), []string{".1.3.6.1.2.1.1.5.0"})
	if !errors.Is(err, snmperrors.ErrPoolClosed) {
		t.Errorf("Get() after Close() err = %v, want wrapping ErrPoolClosed", err)
	}
}

func TestClient_Set_NetworkFailureWrapsError(t *testing.T) {
	a := newTestAgent(t)
	c := newTestClient(t, a)

	// Closing the agent makes both the real Set and the rollback's restore
	// Set fail, exercising Client.Set's error-wrapping path end to end.
	if err := a.Close(); err != nil {
		t.Fatalf("agent.Close() err = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := c.Set(ctx, []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("x")},
	})
	if err == nil {
		t.Fatal("Set() err = nil, want error (agent is unreachable)")
	}
}

func TestClient_Reconcile_Integration(t *testing.T) {
	a := newTestAgent(t)
	a.SetOID(".1.3.6.1.2.1.1.5.0", "stable-name")
	a.SetOID(".1.3.6.1.2.1.2.2.1.7.1", 0)

	c := newTestClient(t, a)

	result, err := c.Reconcile(context.Background(), []state.DesiredState{
		{OID: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("stable-name")},
		{OID: ".1.3.6.1.2.1.2.2.1.7.1", Type: gosnmp.Integer, Value: 1},
	})
	if err != nil {
		t.Fatalf("Reconcile() err = %v", err)
	}

	if len(result.Unchanged) != 1 || result.Unchanged[0] != ".1.3.6.1.2.1.1.5.0" {
		t.Errorf("Unchanged = %v, want [.1.3.6.1.2.1.1.5.0]", result.Unchanged)
	}
	if len(result.Applied) != 1 || result.Applied[0] != ".1.3.6.1.2.1.2.2.1.7.1" {
		t.Errorf("Applied = %v, want [.1.3.6.1.2.1.2.2.1.7.1]", result.Applied)
	}

	packet, err := c.Get(context.Background(), []string{".1.3.6.1.2.1.2.2.1.7.1"})
	if err != nil {
		t.Fatalf("Get() err = %v", err)
	}
	if got := packet.Variables[0].Value; got != 1 {
		t.Errorf("OID value after Reconcile = %v, want 1", got)
	}
}

func TestClient_Raw(t *testing.T) {
	a := newTestAgent(t)
	a.SetOID(".1.3.6.1.2.1.1.5.0", "router-1")
	c := newTestClient(t, a)

	raw := c.Raw()
	if raw == nil {
		t.Fatal("Raw() = nil, want a connected *gosnmp.GoSNMP")
	}

	// The connection is real and usable directly, bypassing pooling/retry.
	packet, err := raw.Get([]string{".1.3.6.1.2.1.1.5.0"})
	if err != nil {
		t.Fatalf("raw.Get() err = %v", err)
	}
	if string(packet.Variables[0].Value.([]byte)) != "router-1" {
		t.Errorf("raw.Get() = %q, want %q", packet.Variables[0].Value, "router-1")
	}

	if raw2 := c.Raw(); raw2 != raw {
		t.Error("Raw() dialed a new connection instead of reusing the cached one")
	}
}

func TestClient_Walk_ReturnsConnectionToPool(t *testing.T) {
	a := newTestAgent(t)
	c := newTestClient(t, a, WithCredentials("public", gosnmp.Version2c))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// The test agent only handles GetRequest/SetRequest, not the
	// GetNext/GetBulk PDUs a real Walk sends, so this is expected to fail
	// (or time out) — the point of this test is that the connection still
	// gets returned to the pool afterward, not that the walk succeeds.
	_ = c.Walk(ctx, ".1.3.6.1.2.1.1", func(_ gosnmp.SnmpPDU) error { return nil })

	if got := c.pool.IdleLen(c.target); got != 1 {
		t.Errorf("IdleLen() after Walk = %d, want 1 (connection must be returned to the pool)", got)
	}
}
