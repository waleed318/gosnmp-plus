package state

import (
	"context"
	"fmt"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

// SNMPSetter is the subset of the gosnmp-plus client capabilities the
// reconciler needs: reading current values and applying changes. It is
// defined here, at the point of use, rather than imported from the client
// package: client imports state for DesiredState/ReconcileResult, so a
// state-to-client import would create a cycle. Any type with this method
// shape (including client.Client) can be passed to NewReconciler without
// either package referencing the other.
type SNMPSetter interface {
	Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error)
	Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error
}

// ReconcileResult reports the outcome of a Reconciler.Apply call, broken
// down by OID.
type ReconcileResult struct {
	Applied    []string
	Drifted    []string
	Unchanged  []string
	RolledBack []string
	Errors     map[string]error
}

// Reconciler applies a set of DesiredState values to an SNMP agent,
// setting only the OIDs whose current value has drifted from the target.
type Reconciler interface {
	Apply(ctx context.Context, states []DesiredState) (ReconcileResult, error)
}

type reconciler struct {
	client SNMPSetter
}

// NewReconciler returns a Reconciler that reads and applies values through
// client.
func NewReconciler(client SNMPSetter) Reconciler {
	return &reconciler{client: client}
}

// Apply fetches the current value of every OID in states, classifies each
// as Unchanged or Drifted using the configured Tolerance, and issues a
// single Set for the drifted OIDs. If that Set fails, every OID in the
// batch is reported as RolledBack — the underlying client.Set is atomic, so
// a failure means none of them actually changed — and recorded in Errors.
func (r *reconciler) Apply(ctx context.Context, states []DesiredState) (ReconcileResult, error) {
	result := ReconcileResult{Errors: make(map[string]error)}
	if len(states) == 0 {
		return result, nil
	}

	set := DesiredStates(states)
	packet, err := r.client.Get(ctx, set.OIDs())
	if err != nil {
		return result, fmt.Errorf("gosnmp-plus/state: get current values: %w", err)
	}

	actual := make(map[string]gosnmp.SnmpPDU, len(packet.Variables))
	for _, v := range packet.Variables {
		actual[v.Name] = v
	}

	var drifted []DesiredState
	for _, ds := range states {
		pdu, ok := actual[ds.OID]
		if !ok {
			result.Errors[ds.OID] = fmt.Errorf("gosnmp-plus/state: %s not returned by agent: %w", ds.OID, snmperrors.ErrNoSuchOID)
			continue
		}

		same, err := matches(ds, pdu)
		if err != nil {
			result.Errors[ds.OID] = err
			continue
		}
		if same {
			result.Unchanged = append(result.Unchanged, ds.OID)
			continue
		}
		result.Drifted = append(result.Drifted, ds.OID)
		drifted = append(drifted, ds)
	}

	if len(drifted) == 0 {
		return result, nil
	}

	pdus := make([]gosnmp.SnmpPDU, len(drifted))
	for i, ds := range drifted {
		pdus[i] = gosnmp.SnmpPDU{Name: ds.OID, Type: ds.Type, Value: ds.Value}
	}

	if err := r.client.Set(ctx, pdus); err != nil {
		for _, ds := range drifted {
			result.RolledBack = append(result.RolledBack, ds.OID)
			result.Errors[ds.OID] = fmt.Errorf("gosnmp-plus/state: set %s: %w", ds.OID, err)
		}
		return result, fmt.Errorf("gosnmp-plus/state: reconcile: %w", err)
	}

	result.Applied = append(result.Applied, result.Drifted...)
	return result, nil
}
