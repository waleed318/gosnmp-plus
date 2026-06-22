// Package rollback implements atomic SNMP Set: every PDU in a batch is
// applied, or the pre-Set snapshot is restored and the failure is reported.
package rollback

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

// SNMPClient is the subset of the gosnmp-plus client capabilities Tx needs:
// reading current values and applying changes. It is defined here, at the
// point of use, rather than imported from the client package: client
// imports rollback to make Set atomic, so a rollback-to-client import would
// create a cycle. Any type with this method shape (including
// client.Client) can be passed to NewTx without either package referencing
// the other.
type SNMPClient interface {
	Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error)
	Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error
}

// Tx applies a batch of PDUs atomically: it snapshots the current value of
// every OID before applying, and restores that snapshot if the Set fails.
type Tx struct {
	client SNMPClient
}

// NewTx returns a Tx that reads and applies values through client.
func NewTx(client SNMPClient) *Tx {
	return &Tx{client: client}
}

// Apply snapshots the current value of every OID in pdus, then applies
// pdus in a single Set. If that Set fails, Apply restores the snapshot
// (covering every OID in the batch, since a partial failure on the agent
// side does not tell the caller which varbinds were actually applied) and
// returns an error wrapping errors.ErrPartialSet together with the
// original Set error. If the restore itself also fails, both the original
// and restore errors are wrapped — neither is dropped.
func (t *Tx) Apply(ctx context.Context, pdus []gosnmp.SnmpPDU) error {
	if len(pdus) == 0 {
		return nil
	}

	oids := make([]string, len(pdus))
	for i, pdu := range pdus {
		oids[i] = pdu.Name
	}

	snapshot, err := t.client.Get(ctx, oids)
	if err != nil {
		return fmt.Errorf("gosnmp-plus/rollback: snapshot %v: %w", oids, err)
	}

	setErr := t.client.Set(ctx, pdus)
	if setErr == nil {
		return nil
	}

	restore := make([]gosnmp.SnmpPDU, len(snapshot.Variables))
	copy(restore, snapshot.Variables)

	if restoreErr := t.client.Set(ctx, restore); restoreErr != nil {
		return fmt.Errorf("gosnmp-plus/rollback: set failed and restore failed: %w",
			errors.Join(setErr, restoreErr, snmperrors.ErrPartialSet))
	}

	return fmt.Errorf("gosnmp-plus/rollback: set failed, restored snapshot: %w",
		errors.Join(setErr, snmperrors.ErrPartialSet))
}
