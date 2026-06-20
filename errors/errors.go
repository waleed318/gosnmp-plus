// Package errors defines typed sentinel errors for gosnmp-plus.
// All errors returned by other packages in this module must wrap one of these sentinels.
package errors

import stderrors "errors"

var (
	ErrTimeout    = stderrors.New("gosnmp-plus: request timed out")                         // ErrTimeout is returned when an SNMP request exceeds its deadline.
	ErrAuthFailed = stderrors.New("gosnmp-plus: authentication failed")                     // ErrAuthFailed is returned when SNMP authentication is rejected by the target.
	ErrNoSuchOID  = stderrors.New("gosnmp-plus: OID not present on device")                 // ErrNoSuchOID is returned when the target device does not have the requested OID.
	ErrPartialSet = stderrors.New("gosnmp-plus: set partially applied; rollback triggered") // ErrPartialSet is returned when a Set was only partially applied and rollback was triggered.
	ErrPoolClosed = stderrors.New("gosnmp-plus: connection pool is closed")                 // ErrPoolClosed is returned when an operation is attempted on a closed connection pool.
)
