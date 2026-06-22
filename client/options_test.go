package client

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/waleed318/gosnmp-plus/retry"
)

func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	if cfg.maxIdlePerTarget != defaultMaxIdlePerTarget {
		t.Errorf("maxIdlePerTarget = %d, want %d", cfg.maxIdlePerTarget, defaultMaxIdlePerTarget)
	}
	if cfg.idleTimeout != defaultIdleTimeout {
		t.Errorf("idleTimeout = %v, want %v", cfg.idleTimeout, defaultIdleTimeout)
	}
	if cfg.community != defaultCommunity {
		t.Errorf("community = %q, want %q", cfg.community, defaultCommunity)
	}
	if cfg.version != gosnmp.Version2c {
		t.Errorf("version = %v, want Version2c", cfg.version)
	}
	if cfg.logger == nil {
		t.Fatal("logger = nil, want noopLogger")
	}
	// Must not panic on a nil-safe no-op logger.
	cfg.logger.Printf("test %s", "value")
}

func TestWithCredentials(t *testing.T) {
	cfg := defaultConfig()

	WithCredentials("private", gosnmp.Version1)(&cfg)

	if cfg.community != "private" {
		t.Errorf("community = %q, want %q", cfg.community, "private")
	}
	if cfg.version != gosnmp.Version1 {
		t.Errorf("version = %v, want Version1", cfg.version)
	}
}

func TestWithRetry(t *testing.T) {
	cfg := defaultConfig()
	policy := retry.Policy{MaxAttempts: 3}

	WithRetry(policy)(&cfg)

	if cfg.retry.MaxAttempts != 3 {
		t.Errorf("retry.MaxAttempts = %d, want 3", cfg.retry.MaxAttempts)
	}
}

func TestWithPool(t *testing.T) {
	cfg := defaultConfig()

	WithPool(5, 10*time.Second)(&cfg)

	if cfg.maxIdlePerTarget != 5 {
		t.Errorf("maxIdlePerTarget = %d, want 5", cfg.maxIdlePerTarget)
	}
	if cfg.idleTimeout != 10*time.Second {
		t.Errorf("idleTimeout = %v, want 10s", cfg.idleTimeout)
	}
}

type recordingLogger struct {
	lines []string
}

func (r *recordingLogger) Printf(format string, args ...interface{}) {
	r.lines = append(r.lines, format)
	_ = args
}

func TestWithLogger(t *testing.T) {
	cfg := defaultConfig()
	rec := &recordingLogger{}

	WithLogger(rec)(&cfg)
	cfg.logger.Printf("hello")

	if len(rec.lines) != 1 {
		t.Fatalf("logger received %d lines, want 1", len(rec.lines))
	}
}

func TestOptionsAppliedInOrder(t *testing.T) {
	cfg := defaultConfig()

	opts := []Option{
		WithPool(1, time.Second),
		WithPool(7, 2*time.Second), // later option overrides earlier one
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.maxIdlePerTarget != 7 {
		t.Errorf("maxIdlePerTarget = %d, want 7 (last option should win)", cfg.maxIdlePerTarget)
	}
}
