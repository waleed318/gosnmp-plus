// Command reconcile demonstrates desired-state reconciliation: gosnmp-plus
// reads the current value of each OID and issues a Set only for the ones
// that have drifted from the desired value.
//
// Usage:
//
//	go run ./examples/reconcile -target 192.168.1.1:161 -community public
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/waleed318/gosnmp-plus/client"
	"github.com/waleed318/gosnmp-plus/state"
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

	desired := []state.DesiredState{
		{OID: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("router-1")},
		{OID: ".1.3.6.1.2.1.2.2.1.7.1", Type: gosnmp.Integer, Value: 1},
	}

	result, err := c.Reconcile(ctx, desired)
	if err != nil {
		log.Fatalf("reconcile: %v", err)
	}

	fmt.Printf("unchanged:   %v\n", result.Unchanged)
	fmt.Printf("drifted:     %v\n", result.Drifted)
	fmt.Printf("applied:     %v\n", result.Applied)
	fmt.Printf("rolled back: %v\n", result.RolledBack)
	for oid, oidErr := range result.Errors {
		fmt.Printf("error[%s]: %v\n", oid, oidErr)
	}
}
