package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
	"github.com/waleed318/gosnmp-plus/retry"
)

// TestPolicySucceedsFirstAttempt verifies that a successful fn is called exactly once
// and no delay is applied.
func TestPolicySucceedsFirstAttempt(t *testing.T) {
	calls := 0
	p := retry.Policy{MaxAttempts: 3, Backoff: retry.Fixed(time.Millisecond)}
	err := p.Do(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

// TestPolicyRetriesMaxAttemptsTimes verifies the total call count is 1+MaxAttempts.
func TestPolicyRetriesMaxAttemptsTimes(t *testing.T) {
	tests := []struct {
		name        string
		maxAttempts int
		wantCalls   int
	}{
		{"zero retries", 0, 1},
		{"one retry", 1, 2},
		{"three retries", 3, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := retry.Policy{
				MaxAttempts: tc.maxAttempts,
				Backoff:     retry.Fixed(time.Millisecond),
			}
			err := p.Do(context.Background(), func() error {
				calls++
				return snmperrors.ErrTimeout
			})
			if !errors.Is(err, snmperrors.ErrTimeout) {
				t.Fatalf("expected ErrTimeout, got %v", err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("expected %d calls, got %d", tc.wantCalls, calls)
			}
		})
	}
}

// TestPolicyRespectsContextCancellation verifies that cancelling ctx during a wait
// causes Do to return ctx.Err() without completing further attempts.
func TestPolicyRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := 0
	p := retry.Policy{
		MaxAttempts: 10,
		Backoff:     retry.Fixed(50 * time.Millisecond),
	}
	err := p.Do(ctx, func() error {
		calls++
		if calls == 2 {
			cancel() // cancel during the second attempt
		}
		return snmperrors.ErrTimeout
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls > 3 {
		t.Fatalf("context not respected: expected ≤3 calls, got %d", calls)
	}
}

// TestPolicyStopsOnNonRetryableError verifies that Do returns immediately when
// RetryOn reports false, without consuming the remaining attempts.
func TestPolicyStopsOnNonRetryableError(t *testing.T) {
	calls := 0
	sentinel := errors.New("not-retryable")
	p := retry.Policy{
		MaxAttempts: 5,
		Backoff:     retry.Fixed(time.Millisecond),
		RetryOn:     func(err error) bool { return errors.Is(err, snmperrors.ErrTimeout) },
	}
	err := p.Do(context.Background(), func() error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (non-retryable stops immediately), got %d", calls)
	}
}

// TestFixedBackoff verifies that Fixed always returns the same duration.
func TestFixedBackoff(t *testing.T) {
	strategy := retry.Fixed(200 * time.Millisecond)
	for attempt := 1; attempt <= 5; attempt++ {
		if d := strategy.Next(attempt); d != 200*time.Millisecond {
			t.Errorf("attempt %d: got %v, want 200ms", attempt, d)
		}
	}
}

// TestExponentialBackoff verifies base × multiplier^attempt growth capped at max.
func TestExponentialBackoff(t *testing.T) {
	strategy := retry.Exponential(100*time.Millisecond, 2.0, 1*time.Second)
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 200 * time.Millisecond},
		{2, 400 * time.Millisecond},
		{3, 800 * time.Millisecond},
		{4, 1 * time.Second}, // 1600ms capped to 1s
		{5, 1 * time.Second}, // still capped
	}
	for _, tc := range tests {
		if got := strategy.Next(tc.attempt); got != tc.want {
			t.Errorf("attempt %d: got %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

// TestJitterWithinBounds verifies that Jitter keeps delays within ±25% of the base.
func TestJitterWithinBounds(t *testing.T) {
	base := 100 * time.Millisecond
	strategy := retry.Jitter(retry.Fixed(base))
	low := time.Duration(float64(base) * 0.75)
	high := time.Duration(float64(base) * 1.25)
	for i := 0; i < 1000; i++ {
		d := strategy.Next(1)
		if d < low || d > high {
			t.Fatalf("iteration %d: jitter %v outside [%v, %v]", i, d, low, high)
		}
	}
}
