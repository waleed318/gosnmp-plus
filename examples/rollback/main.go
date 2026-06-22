// Command rollback demonstrates that Client.Set is atomic: if applying any
// PDU in the batch fails, gosnmp-plus restores every OID in the batch to
// its pre-Set value and returns an error wrapping errors.ErrPartialSet.
//
// Usage:
//
//	go run ./examples/rollback -target 192.168.1.1:161 -community public
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/waleed318/gosnmp-plus/client"
	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

func main() {
	target := flag.String("target", "192.0.2.1:161", "SNMP agent address (host:port)")
	community := flag.String("community", "public", "SNMP community string")
	flag.Parse()

	c, err := client.NewClient(*target, client.WithCredentials(*community, gosnmp.Version2c))
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

	// The second PDU targets sysUpTime, a read-only OID on most agents,
	// which most agents will reject. That failure should restore oid1's
	// original value too, even though that varbind alone would have
	// succeeded — Set is all-or-nothing for the whole batch.
	pdus := []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("new-name")},
		{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.Integer, Value: 0},
	}

	if err := c.Set(ctx, pdus); err != nil {
		if errors.Is(err, snmperrors.ErrPartialSet) {
			fmt.Println("set failed partway through; gosnmp-plus restored the original values:", err)
			return
		}
		log.Fatalf("set: %v", err)
	}

	fmt.Println("set succeeded; all OIDs applied")
}
