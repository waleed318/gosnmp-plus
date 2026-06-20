// Package agent provides a lightweight in-process SNMP v2c UDP agent for use in tests.
//
// The agent responds to GetRequest and SetRequest PDUs using values pre-loaded
// via SetOID. It binds to a random free port and exposes that address via Addr.
// Goroutine lifecycle: a single serve goroutine is started by NewAgent and exits
// when Close is called, which closes the underlying UDP connection.
package agent

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/gosnmp/gosnmp"
)

// Agent is a lightweight in-process SNMP v2c agent for integration testing.
type Agent struct {
	conn *net.UDPConn
	oids map[string]gosnmp.SnmpPDU
	mu   sync.RWMutex
}

// NewAgent creates and starts an SNMP v2c agent on a random free UDP port.
func NewAgent() (*Agent, error) {
	addr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return nil, fmt.Errorf("agent: resolve address: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("agent: listen: %w", err)
	}
	a := &Agent{
		conn: conn,
		oids: make(map[string]gosnmp.SnmpPDU),
	}
	go a.serve()
	return a, nil
}

// Addr returns the UDP address the agent is listening on (e.g. "127.0.0.1:51234").
func (a *Agent) Addr() string {
	return a.conn.LocalAddr().String()
}

// SetOID stores a value for oid so that subsequent SNMP Get requests return it.
// value may be: int (→ Integer), uint32 (→ Gauge32), string or []byte (→ OctetString),
// net.IP (→ IPAddress), or gosnmp.SnmpPDU for explicit type control.
func (a *Agent) SetOID(oid string, value interface{}) {
	var pdu gosnmp.SnmpPDU
	switch v := value.(type) {
	case gosnmp.SnmpPDU:
		pdu = v
	case int:
		pdu = gosnmp.SnmpPDU{Type: gosnmp.Integer, Value: v}
	case uint32:
		pdu = gosnmp.SnmpPDU{Type: gosnmp.Gauge32, Value: v}
	case string:
		pdu = gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: []byte(v)}
	case []byte:
		pdu = gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: v}
	case net.IP:
		pdu = gosnmp.SnmpPDU{Type: gosnmp.IPAddress, Value: v.To4().String()}
	default:
		pdu = gosnmp.SnmpPDU{Type: gosnmp.Null}
	}
	pdu.Name = normalizeOID(oid)
	a.mu.Lock()
	a.oids[normalizeOID(oid)] = pdu
	a.mu.Unlock()
}

// Close stops the agent and releases its UDP port.
// The serve goroutine exits when the connection is closed.
func (a *Agent) Close() error {
	return a.conn.Close()
}

// serve is the main packet-dispatch loop. It exits when the connection is closed.
func (a *Agent) serve() {
	buf := make([]byte, 65535)
	gs := &gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: "public"}
	for {
		n, src, err := a.conn.ReadFromUDP(buf)
		if err != nil {
			return // connection closed
		}
		packet, err := gs.SnmpDecodePacket(buf[:n])
		if err != nil {
			continue // skip malformed packets
		}
		var resp []byte
		switch packet.PDUType {
		case gosnmp.GetRequest:
			resp, err = a.handleGet(packet)
		case gosnmp.SetRequest:
			resp, err = a.handleSet(packet)
		default:
			continue
		}
		if err != nil {
			continue
		}
		_, _ = a.conn.WriteToUDP(resp, src)
	}
}

func (a *Agent) handleGet(req *gosnmp.SnmpPacket) ([]byte, error) {
	a.mu.RLock()
	vars := make([]gosnmp.SnmpPDU, 0, len(req.Variables))
	for _, v := range req.Variables {
		pdu, ok := a.oids[normalizeOID(v.Name)]
		if !ok {
			pdu = gosnmp.SnmpPDU{Name: normalizeOID(v.Name), Type: gosnmp.NoSuchObject}
		}
		vars = append(vars, pdu)
	}
	a.mu.RUnlock()
	return marshalResponse(req.Community, int(req.RequestID), vars)
}

func (a *Agent) handleSet(req *gosnmp.SnmpPacket) ([]byte, error) {
	a.mu.Lock()
	for _, v := range req.Variables {
		a.oids[normalizeOID(v.Name)] = v
	}
	vars := make([]gosnmp.SnmpPDU, len(req.Variables))
	copy(vars, req.Variables)
	a.mu.Unlock()
	return marshalResponse(req.Community, int(req.RequestID), vars)
}

// normalizeOID strips a leading dot from an OID string for consistent map keys.
func normalizeOID(oid string) string {
	return strings.TrimPrefix(oid, ".")
}

// marshalResponse builds a minimal SNMP v2c GetResponse packet in BER encoding.
func marshalResponse(community string, requestID int, vars []gosnmp.SnmpPDU) ([]byte, error) {
	var varbinds []byte
	for _, pdu := range vars {
		encoded, err := encodePDU(pdu)
		if err != nil {
			return nil, err
		}
		varbinds = append(varbinds, encoded...)
	}
	varbindList := tlv(0x30, varbinds)

	pduBody := berInt(int64(requestID))
	pduBody = append(pduBody, berInt(0)...) // error-status = noError
	pduBody = append(pduBody, berInt(0)...) // error-index  = 0
	pduBody = append(pduBody, varbindList...)
	pduTLV := tlv(0xa2, pduBody) // 0xa2 = GetResponse PDU

	msg := berInt(1) // SNMP version: 1 = v2c
	msg = append(msg, tlv(0x04, []byte(community))...)
	msg = append(msg, pduTLV...)
	return tlv(0x30, msg), nil
}

// encodePDU encodes a single SNMP variable binding to BER.
func encodePDU(pdu gosnmp.SnmpPDU) ([]byte, error) {
	oidBytes, err := encodeOIDBytes(pdu.Name)
	if err != nil {
		return nil, fmt.Errorf("agent: encode OID %q: %w", pdu.Name, err)
	}
	oidTLV := tlv(0x06, oidBytes)

	var valTLV []byte
	switch pdu.Type {
	case gosnmp.Integer:
		v, ok := toInt64(pdu.Value)
		if !ok {
			return nil, fmt.Errorf("agent: Integer value has unexpected type %T", pdu.Value)
		}
		valTLV = berInt(v)
	case gosnmp.OctetString:
		b, ok := toBytes(pdu.Value)
		if !ok {
			return nil, fmt.Errorf("agent: OctetString value has unexpected type %T", pdu.Value)
		}
		valTLV = tlv(0x04, b)
	case gosnmp.ObjectIdentifier:
		s, ok := pdu.Value.(string)
		if !ok {
			return nil, fmt.Errorf("agent: ObjectIdentifier value has unexpected type %T", pdu.Value)
		}
		b, encErr := encodeOIDBytes(s)
		if encErr != nil {
			return nil, fmt.Errorf("agent: encode OID value %q: %w", s, encErr)
		}
		valTLV = tlv(0x06, b)
	case gosnmp.IPAddress:
		s, ok := pdu.Value.(string)
		if !ok {
			return nil, fmt.Errorf("agent: IPAddress value has unexpected type %T", pdu.Value)
		}
		ip := net.ParseIP(s).To4()
		if ip == nil {
			return nil, fmt.Errorf("agent: invalid IPv4 address %q", s)
		}
		valTLV = tlv(0x40, ip) // APPLICATION 0 = IpAddress
	case gosnmp.Gauge32:
		v, ok := toUint32(pdu.Value)
		if !ok {
			return nil, fmt.Errorf("agent: Gauge32 value has unexpected type %T", pdu.Value)
		}
		valTLV = berUnsigned(0x42, v) // APPLICATION 2 = Gauge32
	case gosnmp.Counter32:
		v, ok := toUint32(pdu.Value)
		if !ok {
			return nil, fmt.Errorf("agent: Counter32 value has unexpected type %T", pdu.Value)
		}
		valTLV = berUnsigned(0x41, v) // APPLICATION 1 = Counter32
	case gosnmp.NoSuchObject:
		valTLV = tlv(0x80, nil) // CONTEXT PRIMITIVE 0
	case gosnmp.NoSuchInstance:
		valTLV = tlv(0x81, nil) // CONTEXT PRIMITIVE 1
	default:
		valTLV = tlv(0x05, nil) // Null
	}

	return tlv(0x30, append(oidTLV, valTLV...)), nil
}

// toInt64 converts common gosnmp integer value types to int64.
func toInt64(v interface{}) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	}
	return 0, false
}

// toUint32 converts common gosnmp unsigned value types to uint32.
func toUint32(v interface{}) (uint32, bool) {
	switch x := v.(type) {
	case uint:
		return uint32(x), true
	case uint32:
		return x, true
	}
	return 0, false
}

// toBytes converts string or []byte to []byte.
func toBytes(v interface{}) ([]byte, bool) {
	switch x := v.(type) {
	case []byte:
		return x, true
	case string:
		return []byte(x), true
	}
	return nil, false
}

// tlv wraps value in a BER TLV (tag-length-value) element.
func tlv(tag byte, value []byte) []byte {
	l := berLength(len(value))
	out := make([]byte, 1+len(l)+len(value))
	out[0] = tag
	copy(out[1:], l)
	copy(out[1+len(l):], value)
	return out
}

// berLength returns the BER encoding of a length value.
func berLength(n int) []byte {
	switch {
	case n < 128:
		return []byte{byte(n)}
	case n < 256:
		return []byte{0x81, byte(n)}
	default:
		return []byte{0x82, byte(n >> 8), byte(n & 0xFF)}
	}
}

// berInt encodes a signed integer as a BER INTEGER (tag 0x02).
func berInt(v int64) []byte {
	if v == 0 {
		return tlv(0x02, []byte{0x00})
	}
	var b []byte
	n := v
	for n != 0 && n != -1 {
		b = append([]byte{byte(n & 0xFF)}, b...)
		n >>= 8
	}
	// Ensure the leading byte correctly represents the sign.
	if v > 0 && b[0]&0x80 != 0 {
		b = append([]byte{0x00}, b...)
	}
	return tlv(0x02, b)
}

// berUnsigned encodes an unsigned 32-bit integer with the given BER tag,
// using the minimal number of bytes and adding a 0x00 prefix if the high bit is set.
func berUnsigned(tag byte, v uint32) []byte {
	b := []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	i := 0
	for i < len(b)-1 && b[i] == 0 {
		i++
	}
	trimmed := b[i:]
	if trimmed[0]&0x80 != 0 {
		trimmed = append([]byte{0x00}, trimmed...)
	}
	return tlv(tag, trimmed)
}

// encodeOIDBytes converts an OID string into BER OID content bytes (without tag or length).
func encodeOIDBytes(oid string) ([]byte, error) {
	oid = strings.TrimPrefix(oid, ".")
	if oid == "" {
		return nil, fmt.Errorf("empty OID")
	}
	parts := strings.Split(oid, ".")
	nums := make([]uint32, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid OID component %q: %w", p, err)
		}
		nums[i] = uint32(n)
	}
	if len(nums) < 2 {
		return nil, fmt.Errorf("OID requires at least 2 components, got %d", len(nums))
	}
	// First two components are combined: first*40 + second.
	content := base128(nums[0]*40 + nums[1])
	for _, n := range nums[2:] {
		content = append(content, base128(n)...)
	}
	return content, nil
}

// base128 encodes a uint32 using base-128 (multi-byte) representation for BER OID encoding.
func base128(n uint32) []byte {
	if n == 0 {
		return []byte{0x00}
	}
	var parts []byte
	for n > 0 {
		parts = append([]byte{byte(n & 0x7F)}, parts...)
		n >>= 7
	}
	// Set the high bit on all bytes except the last.
	for i := 0; i < len(parts)-1; i++ {
		parts[i] |= 0x80
	}
	return parts
}
