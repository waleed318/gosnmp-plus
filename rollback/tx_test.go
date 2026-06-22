package rollback

import (
	"context"
	"errors"
	"testing"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

// mockSNMPClient records Set calls in order and lets each call's behaviour
// be scripted independently, so tests can simulate "first Set fails,
// second (restore) Set succeeds" and similar sequences.
type mockSNMPClient struct {
	getFunc  func(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error)
	setFuncs []func(ctx context.Context, pdus []gosnmp.SnmpPDU) error

	setCalls [][]gosnmp.SnmpPDU
}

func (m *mockSNMPClient) Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error) {
	return m.getFunc(ctx, oids)
}

func (m *mockSNMPClient) Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error {
	idx := len(m.setCalls)
	m.setCalls = append(m.setCalls, pdus)
	if idx < len(m.setFuncs) {
		return m.setFuncs[idx](ctx, pdus)
	}
	return nil
}

func snapshotOf(pdus []gosnmp.SnmpPDU) *gosnmp.SnmpPacket {
	return &gosnmp.SnmpPacket{Variables: pdus}
}

func TestTx_Apply_NoPDUsIsNoop(t *testing.T) {
	mock := &mockSNMPClient{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			t.Fatal("Get should not be called for an empty PDU list")
			return nil, nil
		},
	}
	tx := NewTx(mock)

	if err := tx.Apply(context.Background(), nil); err != nil {
		t.Fatalf("Apply() err = %v", err)
	}
}

func TestTx_Apply_HappyPathNoRestore(t *testing.T) {
	snapshot := []gosnmp.SnmpPDU{
		{Name: "oid1", Type: gosnmp.Integer, Value: 1},
		{Name: "oid2", Type: gosnmp.Integer, Value: 2},
	}
	mock := &mockSNMPClient{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return snapshotOf(snapshot), nil
		},
		setFuncs: []func(context.Context, []gosnmp.SnmpPDU) error{
			func(_ context.Context, _ []gosnmp.SnmpPDU) error { return nil },
		},
	}
	tx := NewTx(mock)

	pdus := []gosnmp.SnmpPDU{
		{Name: "oid1", Type: gosnmp.Integer, Value: 10},
		{Name: "oid2", Type: gosnmp.Integer, Value: 20},
	}
	if err := tx.Apply(context.Background(), pdus); err != nil {
		t.Fatalf("Apply() err = %v", err)
	}
	if len(mock.setCalls) != 1 {
		t.Errorf("Set was called %d times, want 1 (no restore on success)", len(mock.setCalls))
	}
}

func TestTx_Apply_GetSnapshotError(t *testing.T) {
	wantErr := errors.New("agent unreachable")
	mock := &mockSNMPClient{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return nil, wantErr
		},
	}
	tx := NewTx(mock)

	err := tx.Apply(context.Background(), []gosnmp.SnmpPDU{{Name: "oid1", Type: gosnmp.Integer, Value: 1}})
	if !errors.Is(err, wantErr) {
		t.Errorf("Apply() err = %v, want wrapping %v", err, wantErr)
	}
	if len(mock.setCalls) != 0 {
		t.Errorf("Set was called %d times, want 0 (snapshot must fail before any Set)", len(mock.setCalls))
	}
}

func TestTx_Apply_PartialFailureRestoresFullSnapshot(t *testing.T) {
	snapshot := []gosnmp.SnmpPDU{
		{Name: "oid1", Type: gosnmp.Integer, Value: 1},
		{Name: "oid2", Type: gosnmp.Integer, Value: 2},
	}
	setErr := errors.New("agent rejected oid2")
	mock := &mockSNMPClient{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return snapshotOf(snapshot), nil
		},
		setFuncs: []func(context.Context, []gosnmp.SnmpPDU) error{
			func(_ context.Context, _ []gosnmp.SnmpPDU) error { return setErr }, // the real Set
			func(_ context.Context, _ []gosnmp.SnmpPDU) error { return nil },    // the restore
		},
	}
	tx := NewTx(mock)

	pdus := []gosnmp.SnmpPDU{
		{Name: "oid1", Type: gosnmp.Integer, Value: 10}, // would have succeeded alone
		{Name: "oid2", Type: gosnmp.Integer, Value: 20}, // caused the batch to fail
	}
	err := tx.Apply(context.Background(), pdus)

	if !errors.Is(err, setErr) {
		t.Errorf("Apply() err = %v, want wrapping the original Set error %v", err, setErr)
	}
	if !errors.Is(err, snmperrors.ErrPartialSet) {
		t.Errorf("Apply() err = %v, want wrapping ErrPartialSet", err)
	}

	if len(mock.setCalls) != 2 {
		t.Fatalf("Set was called %d times, want 2 (apply + restore)", len(mock.setCalls))
	}
	restoreCall := mock.setCalls[1]
	if len(restoreCall) != len(snapshot) {
		t.Fatalf("restore Set called with %d PDUs, want %d (the full snapshot, including OIDs that may have succeeded)", len(restoreCall), len(snapshot))
	}
	for i, pdu := range restoreCall {
		if pdu.Name != snapshot[i].Name || pdu.Value != snapshot[i].Value {
			t.Errorf("restore PDU[%d] = %+v, want %+v", i, pdu, snapshot[i])
		}
	}
}

func TestTx_Apply_RestoreFailureReturnsCompoundError(t *testing.T) {
	snapshot := []gosnmp.SnmpPDU{
		{Name: "oid1", Type: gosnmp.Integer, Value: 1},
	}
	setErr := errors.New("set failed")
	restoreErr := errors.New("restore also failed")
	mock := &mockSNMPClient{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return snapshotOf(snapshot), nil
		},
		setFuncs: []func(context.Context, []gosnmp.SnmpPDU) error{
			func(_ context.Context, _ []gosnmp.SnmpPDU) error { return setErr },
			func(_ context.Context, _ []gosnmp.SnmpPDU) error { return restoreErr },
		},
	}
	tx := NewTx(mock)

	err := tx.Apply(context.Background(), []gosnmp.SnmpPDU{{Name: "oid1", Type: gosnmp.Integer, Value: 99}})

	if !errors.Is(err, setErr) {
		t.Errorf("Apply() err = %v, want wrapping the original Set error %v", err, setErr)
	}
	if !errors.Is(err, restoreErr) {
		t.Errorf("Apply() err = %v, want wrapping the restore error %v (must not be silently dropped)", err, restoreErr)
	}
	if !errors.Is(err, snmperrors.ErrPartialSet) {
		t.Errorf("Apply() err = %v, want wrapping ErrPartialSet", err)
	}
}
