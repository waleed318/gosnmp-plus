// Package client implements a per-target SNMP connection pool and the
// functional options used to configure it.
package client

import (
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/waleed318/gosnmp-plus/retry"
)

const (
	defaultMaxIdlePerTarget = 2
	defaultIdleTimeout      = 30 * time.Second
	defaultCommunity        = "public"
	defaultRequestTimeout   = 2 * time.Second
	defaultSNMPRetries      = 1
)

// Logger is the minimal logging interface accepted by WithLogger. It is
// satisfied by *log.Logger.
type Logger interface {
	Printf(format string, args ...interface{})
}

type noopLogger struct{}

func (noopLogger) Printf(string, ...interface{}) {}

// config holds settings shared by Pool and Client.
type config struct {
	retry            retry.Policy
	maxIdlePerTarget int
	idleTimeout      time.Duration
	logger           Logger

	community      string
	version        gosnmp.SnmpVersion
	requestTimeout time.Duration
	snmpRetries    int
}

func defaultConfig() config {
	return config{
		maxIdlePerTarget: defaultMaxIdlePerTarget,
		idleTimeout:      defaultIdleTimeout,
		logger:           noopLogger{},
		community:        defaultCommunity,
		version:          gosnmp.Version2c,
		requestTimeout:   defaultRequestTimeout,
		snmpRetries:      defaultSNMPRetries,
	}
}

// Option configures a Pool or Client. Options are applied in order, so a
// later option overrides an earlier one.
type Option func(*config)

// WithRetry sets the retry policy applied to Client.Get and Client.Set.
func WithRetry(p retry.Policy) Option {
	return func(c *config) { c.retry = p }
}

// WithPool sets the maximum number of idle connections kept per target and
// how long an idle connection may sit before it is evicted.
func WithPool(maxIdlePerTarget int, idleTimeout time.Duration) Option {
	return func(c *config) {
		c.maxIdlePerTarget = maxIdlePerTarget
		c.idleTimeout = idleTimeout
	}
}

// WithLogger sets the logger used for pool diagnostics such as connection
// dial and close failures. The default is a no-op logger.
func WithLogger(l Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithCredentials sets the SNMP community string and protocol version used
// when dialing a target. The default is community "public" on
// gosnmp.Version2c.
func WithCredentials(community string, version gosnmp.SnmpVersion) Option {
	return func(c *config) {
		c.community = community
		c.version = version
	}
}
