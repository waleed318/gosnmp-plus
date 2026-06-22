package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/gosnmp/gosnmp"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
	"github.com/waleed318/gosnmp-plus/rollback"
	"github.com/waleed318/gosnmp-plus/state"
)

// SNMPClient is the capability surface gosnmp-plus exposes for a single
// SNMP target: retried, connection-pooled read/write/walk access. Client
// implements this interface.
type SNMPClient interface {
	Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error)
	Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error
	Walk(ctx context.Context, oid string, fn gosnmp.WalkFunc) error
	Close() error
}

// Client is a resilient SNMP client for a single target. It layers retry,
// connection pooling, desired-state reconciliation, and atomic
// Set-with-rollback on top of gosnmp.
type Client struct {
	target string
	pool   *Pool
	cfg    config
	dial   ConnFactory

	rawMu   sync.Mutex
	rawConn *gosnmp.GoSNMP
}

// NewClient creates a Client for target ("host" or "host:port"; port
// defaults to 161 if omitted). Connections are dialed lazily, on the first
// Get, Set, Walk, or Reconcile call.
func NewClient(target string, opts ...Option) (*Client, error) {
	addr, err := normalizeTarget(target)
	if err != nil {
		return nil, fmt.Errorf("gosnmp-plus/client: %w", err)
	}

	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	factory := func(ctx context.Context, t string) (*gosnmp.GoSNMP, error) {
		host, portStr, splitErr := net.SplitHostPort(t)
		if splitErr != nil {
			return nil, fmt.Errorf("gosnmp-plus/client: %w", splitErr)
		}
		port, parseErr := strconv.ParseUint(portStr, 10, 16)
		if parseErr != nil {
			return nil, fmt.Errorf("gosnmp-plus/client: invalid port %q: %w", portStr, parseErr)
		}

		conn := &gosnmp.GoSNMP{
			Target:    host,
			Port:      uint16(port),
			Community: cfg.community,
			Version:   cfg.version,
			Timeout:   cfg.requestTimeout,
			Retries:   cfg.snmpRetries,
			Context:   ctx,
		}
		if connectErr := conn.Connect(); connectErr != nil {
			return nil, fmt.Errorf("gosnmp-plus/client: connect %s: %w", t, connectErr)
		}
		return conn, nil
	}

	pool := NewPool(factory, WithPool(cfg.maxIdlePerTarget, cfg.idleTimeout), WithLogger(cfg.logger))

	return &Client{target: addr, pool: pool, cfg: cfg, dial: factory}, nil
}

// normalizeTarget appends the default SNMP port to target if it doesn't
// already specify one.
func normalizeTarget(target string) (string, error) {
	if target == "" {
		return "", errors.New("target must not be empty")
	}
	if _, _, err := net.SplitHostPort(target); err == nil {
		return target, nil
	}
	return net.JoinHostPort(target, "161"), nil
}

// connAdapter adapts a *gosnmp.GoSNMP — whose Get/Set methods don't take a
// context — to the context-aware interfaces consumed by the rollback and
// state packages.
type connAdapter struct {
	conn *gosnmp.GoSNMP
}

func (a connAdapter) Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error) {
	a.conn.Context = ctx
	return a.conn.Get(oids)
}

func (a connAdapter) Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error {
	a.conn.Context = ctx
	_, err := a.conn.Set(pdus)
	return err
}

// rollbackAdapter has the same Get as connAdapter, but routes Set through
// rollback.Tx so that anything built on top of it (state.Reconciler, via
// Client.Reconcile) gets the same atomic-Set guarantee as Client.Set.
type rollbackAdapter struct {
	conn *gosnmp.GoSNMP
}

func (a rollbackAdapter) Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error) {
	return connAdapter(a).Get(ctx, oids)
}

func (a rollbackAdapter) Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error {
	tx := rollback.NewTx(connAdapter(a))
	return tx.Apply(ctx, pdus)
}

// wrapSNMPError classifies err as a timeout where possible so callers can
// use errors.Is(err, snmperrors.ErrTimeout) instead of matching on
// gosnmp's plain-text error messages.
func wrapSNMPError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "timeout") {
		return fmt.Errorf("%w: %v", snmperrors.ErrTimeout, err)
	}
	return err
}

// Get retrieves oids, retrying according to the configured retry.Policy.
func (c *Client) Get(ctx context.Context, oids []string) (*gosnmp.SnmpPacket, error) {
	var packet *gosnmp.SnmpPacket
	err := c.cfg.retry.Do(ctx, func() error {
		conn, getErr := c.pool.Get(ctx, c.target)
		if getErr != nil {
			return getErr
		}
		defer c.pool.Put(c.target, conn)

		p, snmpErr := connAdapter{conn}.Get(ctx, oids)
		if snmpErr != nil {
			return fmt.Errorf("gosnmp-plus/client: get %v: %w", oids, wrapSNMPError(snmpErr))
		}
		packet = p
		return nil
	})
	return packet, err
}

// Set applies pdus atomically (see package rollback) and retries according
// to the configured retry.Policy.
func (c *Client) Set(ctx context.Context, pdus []gosnmp.SnmpPDU) error {
	return c.cfg.retry.Do(ctx, func() error {
		conn, err := c.pool.Get(ctx, c.target)
		if err != nil {
			return err
		}
		defer c.pool.Put(c.target, conn)

		tx := rollback.NewTx(connAdapter{conn})
		if applyErr := tx.Apply(ctx, pdus); applyErr != nil {
			return fmt.Errorf("gosnmp-plus/client: set: %w", wrapSNMPError(applyErr))
		}
		return nil
	})
}

// Walk invokes fn for every OID under oid. Unlike Get and Set, Walk is not
// retried automatically: fn may already have run for some rows by the time
// an error occurs partway through, and retrying would invoke it again for
// those rows.
func (c *Client) Walk(ctx context.Context, oid string, fn gosnmp.WalkFunc) error {
	conn, err := c.pool.Get(ctx, c.target)
	if err != nil {
		return err
	}
	defer c.pool.Put(c.target, conn)

	conn.Context = ctx
	if err := conn.Walk(oid, fn); err != nil {
		return fmt.Errorf("gosnmp-plus/client: walk %s: %w", oid, wrapSNMPError(err))
	}
	return nil
}

// Reconcile applies desired to the target, setting only OIDs that have
// drifted. The underlying Set goes through the same rollback.Tx as
// Client.Set, so a failure restores every OID in the drifted batch.
func (c *Client) Reconcile(ctx context.Context, desired []state.DesiredState) (state.ReconcileResult, error) {
	conn, err := c.pool.Get(ctx, c.target)
	if err != nil {
		return state.ReconcileResult{}, err
	}
	defer c.pool.Put(c.target, conn)

	reconciler := state.NewReconciler(rollbackAdapter{conn})
	return reconciler.Apply(ctx, desired)
}

// Raw returns a *gosnmp.GoSNMP for direct, low-level access, dialing it on
// first use and reusing it on subsequent calls. It is an escape hatch:
// gosnmp-plus does not apply retry, rollback, or pooling to anything done
// with the returned connection directly. Raw returns nil if dialing fails;
// prefer Get/Set/Walk/Reconcile, which surface that error properly.
func (c *Client) Raw() *gosnmp.GoSNMP {
	c.rawMu.Lock()
	defer c.rawMu.Unlock()

	if c.rawConn != nil {
		return c.rawConn
	}
	conn, err := c.dial(context.Background(), c.target)
	if err != nil {
		c.cfg.logger.Printf("gosnmp-plus/client: Raw() dial %s: %v", c.target, err)
		return nil
	}
	c.rawConn = conn
	return c.rawConn
}

// Close releases the Client's connection pool, closing every idle
// connection, and closes the connection held by Raw if one was dialed.
func (c *Client) Close() error {
	c.rawMu.Lock()
	rawConn := c.rawConn
	c.rawConn = nil
	c.rawMu.Unlock()

	if rawConn != nil {
		if err := rawConn.Close(); err != nil {
			c.cfg.logger.Printf("gosnmp-plus/client: close raw conn: %v", err)
		}
	}
	return c.pool.Close()
}
