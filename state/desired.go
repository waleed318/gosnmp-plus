// Package state implements desired-state reconciliation against an SNMP
// agent: comparing target OID/value pairs to the agent's current values and
// applying only the ones that have drifted.
package state

import "github.com/gosnmp/gosnmp"

// DesiredState describes the value an OID is expected to hold. Tolerance is
// only consulted for numeric types (gosnmp.Integer, gosnmp.Counter32,
// gosnmp.Gauge32); OctetString, ObjectIdentifier, and IPAddress always
// require an exact match regardless of Tolerance.
type DesiredState struct {
	OID       string
	Value     interface{}
	Type      gosnmp.Asn1BER
	Tolerance float64
}

// DesiredStates is a named collection of DesiredState values.
type DesiredStates []DesiredState

// OIDs returns the OID of every state in the set, suitable for passing to
// an SNMP Get.
func (s DesiredStates) OIDs() []string {
	oids := make([]string, len(s))
	for i, ds := range s {
		oids[i] = ds.OID
	}
	return oids
}
