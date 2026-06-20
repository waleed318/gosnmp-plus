package state

import (
	"fmt"
	"math"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

// matches reports whether actual already holds the value described by
// desired, applying desired.Tolerance for numeric types. It returns an
// error wrapping errors.ErrNoSuchOID if desired.Type is not one of the
// supported ASN.1 types.
func matches(desired DesiredState, actual gosnmp.SnmpPDU) (bool, error) {
	switch desired.Type {
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Gauge32:
		want, err := toFloat64(desired.Value)
		if err != nil {
			return false, fmt.Errorf("gosnmp-plus/state: %s: desired value: %w", desired.OID, err)
		}
		got, err := toFloat64(actual.Value)
		if err != nil {
			return false, fmt.Errorf("gosnmp-plus/state: %s: actual value: %w", desired.OID, err)
		}
		return math.Abs(got-want) <= desired.Tolerance, nil

	case gosnmp.OctetString:
		want, err := toBytes(desired.Value)
		if err != nil {
			return false, fmt.Errorf("gosnmp-plus/state: %s: desired value: %w", desired.OID, err)
		}
		got, err := toBytes(actual.Value)
		if err != nil {
			return false, fmt.Errorf("gosnmp-plus/state: %s: actual value: %w", desired.OID, err)
		}
		return string(want) == string(got), nil

	case gosnmp.ObjectIdentifier, gosnmp.IPAddress:
		want, err := toString(desired.Value)
		if err != nil {
			return false, fmt.Errorf("gosnmp-plus/state: %s: desired value: %w", desired.OID, err)
		}
		got, err := toString(actual.Value)
		if err != nil {
			return false, fmt.Errorf("gosnmp-plus/state: %s: actual value: %w", desired.OID, err)
		}
		return want == got, nil

	default:
		return false, fmt.Errorf("gosnmp-plus/state: %s: unsupported type %v: %w", desired.OID, desired.Type, snmperrors.ErrNoSuchOID)
	}
}

func toFloat64(v interface{}) (float64, error) {
	switch n := v.(type) {
	case int:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case uint:
		return float64(n), nil
	case uint32:
		return float64(n), nil
	case uint64:
		return float64(n), nil
	case float64:
		return n, nil
	default:
		return 0, fmt.Errorf("value %v (%T) is not numeric", v, v)
	}
}

func toBytes(v interface{}) ([]byte, error) {
	switch b := v.(type) {
	case []byte:
		return b, nil
	case string:
		return []byte(b), nil
	default:
		return nil, fmt.Errorf("value %v (%T) is not a byte string", v, v)
	}
}

func toString(v interface{}) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	case []byte:
		return string(s), nil
	default:
		return "", fmt.Errorf("value %v (%T) is not a string", v, v)
	}
}
