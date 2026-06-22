// Command basic_get demonstrates a single SNMP Get against a target using
// client.Client, with retry and connection pooling already wired in.
//
// Usage:
//
//	go run ./examples/basic_get -target 192.168.1.1:161 -community public -oid .1.3.6.1.2.1.1.5.0
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/waleed318/gosnmp-plus/client"
	"github.com/waleed318/gosnmp-plus/retry"
)

func main() {
	target := flag.String("target", "192.0.2.1:161", "SNMP agent address (host:port)")
	community := flag.String("community", "public", "SNMP community string")
	oid := flag.String("oid", ".1.3.6.1.2.1.1.5.0", "OID to fetch (default: sysName)")
	flag.Parse()

	c, err := client.NewClient(*target,
		client.WithCredentials(*community, gosnmp.Version2c),
		client.WithRetry(retry.Policy{
			MaxAttempts: 3,
			Backoff:     retry.Jitter(retry.Exponential(100*time.Millisecond, 2, 2*time.Second)),
		}),
	)
	if err != nil {
		log.Fatalf("new client: %v", err)
	}
	defer func() {
		if cerr := c.Close(); cerr != nil {
			log.Printf("close: %v", cerr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	packet, err := c.Get(ctx, []string{*oid})
	if err != nil {
		log.Fatalf("get %s: %v", *oid, err)
	}

	for _, v := range packet.Variables {
		fmt.Printf("%s = %s (%v)\n", v.Name, formatValue(v), v.Type)
	}
}

// formatValue renders a PDU value for display, decoding OctetString as text
// since gosnmp reports it as a raw []byte.
func formatValue(pdu gosnmp.SnmpPDU) string {
	if pdu.Type == gosnmp.OctetString {
		if b, ok := pdu.Value.([]byte); ok {
			return string(b)
		}
	}
	return fmt.Sprintf("%v", pdu.Value)
}
