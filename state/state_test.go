package state

import (
	"context"
	"errors"
	"testing"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

func TestDesiredStates_OIDs(t *testing.T) {
	set := DesiredStates{
		{OID: ".1.3.6.1.2.1.1.1.0"},
		{OID: ".1.3.6.1.2.1.1.2.0"},
	}

	got := set.OIDs()
	want := []string{".1.3.6.1.2.1.1.1.0", ".1.3.6.1.2.1.1.2.0"}
	if len(got) != len(want) {
		t.Fatalf("OIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("OIDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMatches(t *testing.T) {
	tests := []struct {
		name    string
		desired DesiredState
		actual  gosnmp.SnmpPDU
		want    bool
		wantErr error
	}{
		{
			name:    "integer exact match",
			desired: DesiredState{OID: "oid", Type: gosnmp.Integer, Value: 42},
			actual:  gosnmp.SnmpPDU{Value: 42},
			want:    true,
		},
		{
			name:    "integer within tolerance",
			desired: DesiredState{OID: "oid", Type: gosnmp.Integer, Value: 100, Tolerance: 5},
			actual:  gosnmp.SnmpPDU{Value: 103},
			want:    true,
		},
		{
			name:    "integer outside tolerance",
			desired: DesiredState{OID: "oid", Type: gosnmp.Integer, Value: 100, Tolerance: 5},
			actual:  gosnmp.SnmpPDU{Value: 110},
			want:    false,
		},
		{
			name:    "gauge32 within tolerance",
			desired: DesiredState{OID: "oid", Type: gosnmp.Gauge32, Value: uint(50), Tolerance: 2},
			actual:  gosnmp.SnmpPDU{Value: uint(51)},
			want:    true,
		},
		{
			name:    "counter32 mismatch",
			desired: DesiredState{OID: "oid", Type: gosnmp.Counter32, Value: uint(50)},
			actual:  gosnmp.SnmpPDU{Value: uint(51)},
			want:    false,
		},
		{
			name:    "octet string match",
			desired: DesiredState{OID: "oid", Type: gosnmp.OctetString, Value: []byte("hello")},
			actual:  gosnmp.SnmpPDU{Value: []byte("hello")},
			want:    true,
		},
		{
			name:    "octet string mismatch ignores tolerance",
			desired: DesiredState{OID: "oid", Type: gosnmp.OctetString, Value: "hello", Tolerance: 100},
			actual:  gosnmp.SnmpPDU{Value: []byte("world")},
			want:    false,
		},
		{
			name:    "object identifier match",
			desired: DesiredState{OID: "oid", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1"},
			actual:  gosnmp.SnmpPDU{Value: ".1.3.6.1"},
			want:    true,
		},
		{
			name:    "ip address mismatch",
			desired: DesiredState{OID: "oid", Type: gosnmp.IPAddress, Value: "10.0.0.1"},
			actual:  gosnmp.SnmpPDU{Value: "10.0.0.2"},
			want:    false,
		},
		{
			name:    "unsupported type wraps ErrNoSuchOID",
			desired: DesiredState{OID: "oid", Type: gosnmp.Null, Value: nil},
			actual:  gosnmp.SnmpPDU{Value: nil},
			wantErr: snmperrors.ErrNoSuchOID,
		},
		{
			name:    "non-numeric desired value errors",
			desired: DesiredState{OID: "oid", Type: gosnmp.Integer, Value: "not a number"},
			actual:  gosnmp.SnmpPDU{Value: 1},
			wantErr: errAny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := matches(tt.desired, tt.actual)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("matches() err = nil, want error")
				}
				if tt.wantErr != errAny && !errors.Is(err, tt.wantErr) {
					t.Errorf("matches() err = %v, want wrapping %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("matches() err = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

// errAny is a sentinel used in table tests to assert "any error", without
// pinning down which one.
var errAny = errors.New("any error")

type mockSNMPSetter struct {
	getFunc func(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error)
	setFunc func(ctx context.Context, pdus []gosnmp.SnmpPDU) error

	setCalls [][]gosnmp.SnmpPDU
}

func (m *mockSNMPSetter) Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error) {
	return m.getFunc(ctx, oids)
}

func (m *mockSNMPSetter) Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error {
	m.setCalls = append(m.setCalls, pdus)
	if m.setFunc != nil {
		return m.setFunc(ctx, pdus)
	}
	return nil
}

func TestReconciler_Apply_NoStatesIsNoop(t *testing.T) {
	mock := &mockSNMPSetter{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			t.Fatal("Get should not be called for an empty state list")
			return nil, nil
		},
	}
	r := NewReconciler(mock)

	result, err := r.Apply(context.Background(), nil)
	if err != nil {
		t.Fatalf("Apply() err = %v", err)
	}
	if len(result.Applied) != 0 || len(result.Drifted) != 0 || len(result.Unchanged) != 0 {
		t.Errorf("Apply() with no states returned non-empty result: %+v", result)
	}
}

func TestReconciler_Apply_GetError(t *testing.T) {
	wantErr := errors.New("network unreachable")
	mock := &mockSNMPSetter{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return nil, wantErr
		},
	}
	r := NewReconciler(mock)

	_, err := r.Apply(context.Background(), []DesiredState{{OID: "oid1", Type: gosnmp.Integer, Value: 1}})
	if !errors.Is(err, wantErr) {
		t.Errorf("Apply() err = %v, want wrapping %v", err, wantErr)
	}
}

func TestReconciler_Apply_UnchangedWithinTolerance(t *testing.T) {
	mock := &mockSNMPSetter{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return &gosnmp.SnmpPacket{Variables: []gosnmp.SnmpPDU{
				{Name: "oid1", Type: gosnmp.Integer, Value: 103},
			}}, nil
		},
	}
	r := NewReconciler(mock)

	result, err := r.Apply(context.Background(), []DesiredState{
		{OID: "oid1", Type: gosnmp.Integer, Value: 100, Tolerance: 5},
	})
	if err != nil {
		t.Fatalf("Apply() err = %v", err)
	}
	if len(result.Unchanged) != 1 || result.Unchanged[0] != "oid1" {
		t.Errorf("Unchanged = %v, want [oid1]", result.Unchanged)
	}
	if len(mock.setCalls) != 0 {
		t.Errorf("Set was called %d times, want 0 (value is within tolerance)", len(mock.setCalls))
	}
}

func TestReconciler_Apply_CallsSetOnlyForDriftedOIDs(t *testing.T) {
	mock := &mockSNMPSetter{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return &gosnmp.SnmpPacket{Variables: []gosnmp.SnmpPDU{
				{Name: "stable", Type: gosnmp.Integer, Value: 10},
				{Name: "drifted", Type: gosnmp.Integer, Value: 999},
			}}, nil
		},
	}
	r := NewReconciler(mock)

	result, err := r.Apply(context.Background(), []DesiredState{
		{OID: "stable", Type: gosnmp.Integer, Value: 10},
		{OID: "drifted", Type: gosnmp.Integer, Value: 1},
	})
	if err != nil {
		t.Fatalf("Apply() err = %v", err)
	}

	if len(mock.setCalls) != 1 {
		t.Fatalf("Set was called %d times, want 1", len(mock.setCalls))
	}
	pdus := mock.setCalls[0]
	if len(pdus) != 1 || pdus[0].Name != "drifted" {
		t.Errorf("Set called with %v, want only the drifted OID", pdus)
	}

	if len(result.Applied) != 1 || result.Applied[0] != "drifted" {
		t.Errorf("Applied = %v, want [drifted]", result.Applied)
	}
	if len(result.Unchanged) != 1 || result.Unchanged[0] != "stable" {
		t.Errorf("Unchanged = %v, want [stable]", result.Unchanged)
	}
}

func TestReconciler_Apply_PartialFailurePopulatesErrors(t *testing.T) {
	setErr := snmperrors.ErrPartialSet
	mock := &mockSNMPSetter{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return &gosnmp.SnmpPacket{Variables: []gosnmp.SnmpPDU{
				{Name: "oid1", Type: gosnmp.Integer, Value: 999},
				{Name: "oid2", Type: gosnmp.Integer, Value: 999},
			}}, nil
		},
		setFunc: func(_ context.Context, _ []gosnmp.SnmpPDU) error {
			return setErr
		},
	}
	r := NewReconciler(mock)

	result, err := r.Apply(context.Background(), []DesiredState{
		{OID: "oid1", Type: gosnmp.Integer, Value: 1},
		{OID: "oid2", Type: gosnmp.Integer, Value: 2},
	})
	if !errors.Is(err, setErr) {
		t.Errorf("Apply() err = %v, want wrapping %v", err, setErr)
	}

	for _, oid := range []string{"oid1", "oid2"} {
		if !errors.Is(result.Errors[oid], setErr) {
			t.Errorf("Errors[%q] = %v, want wrapping %v", oid, result.Errors[oid], setErr)
		}
	}
	if len(result.RolledBack) != 2 {
		t.Errorf("RolledBack = %v, want both OIDs", result.RolledBack)
	}
	if len(result.Applied) != 0 {
		t.Errorf("Applied = %v, want empty after Set failure", result.Applied)
	}
}

func TestReconciler_Apply_MissingOIDInResponse(t *testing.T) {
	mock := &mockSNMPSetter{
		getFunc: func(_ context.Context, _ []string) (*gosnmp.SnmpPacket, error) {
			return &gosnmp.SnmpPacket{Variables: []gosnmp.SnmpPDU{}}, nil
		},
	}
	r := NewReconciler(mock)

	result, err := r.Apply(context.Background(), []DesiredState{
		{OID: "missing", Type: gosnmp.Integer, Value: 1},
	})
	if err != nil {
		t.Fatalf("Apply() err = %v, want nil (per-OID errors go in result.Errors)", err)
	}
	if !errors.Is(result.Errors["missing"], snmperrors.ErrNoSuchOID) {
		t.Errorf("Errors[missing] = %v, want wrapping ErrNoSuchOID", result.Errors["missing"])
	}
}
