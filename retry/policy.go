// Package retry provides configurable retry policies with pluggable backoff strategies.
// All retry loops respect context cancellation between attempts.
package retry

import (
	"context"
	"errors"
	"time"

	snmperrors "github.com/waleed318/gosnmp-plus/errors"
)

// BackoffStrategy determines the delay before each retry attempt.
// The attempt parameter is 1-based: 1 = first retry, 2 = second retry, and so on.
type BackoffStrategy interface {
	Next(attempt int) time.Duration
}

// Policy configures retry behaviour for a fallible operation.
type Policy struct {
	// MaxAttempts is the maximum number of retries after the initial call.
	// 0 means no retries — the operation is attempted exactly once.
	MaxAttempts int
	// Backoff determines the wait between attempts. nil means no delay.
	Backoff BackoffStrategy
	// RetryOn reports whether err warrants a retry.
	// nil defaults to retrying on ErrTimeout only.
	RetryOn func(error) bool
}

// Do calls fn and retries on eligible errors up to p.MaxAttempts times.
// It waits between attempts according to p.Backoff and respects ctx cancellation.
// If ctx is cancelled during a wait, Do returns ctx.Err() immediately.
func (p Policy) Do(ctx context.Context, fn func() error) error {
	retryOn := p.RetryOn
	if retryOn == nil {
		retryOn = func(e error) bool {
			return errors.Is(e, snmperrors.ErrTimeout)
		}
	}

	total := p.MaxAttempts + 1 // 1 initial attempt + MaxAttempts retries
	var lastErr error
	for i := 0; i < total; i++ {
		if i > 0 {
			delay := time.Duration(0)
			if p.Backoff != nil {
				delay = p.Backoff.Next(i)
			}
			if delay > 0 {
				t := time.NewTimer(delay)
				select {
				case <-t.C:
				case <-ctx.Done():
					t.Stop()
					return ctx.Err()
				}
			} else {
				// Zero delay: non-blocking context check.
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
		}
		lastErr = fn()
		if lastErr == nil || !retryOn(lastErr) {
			return lastErr
		}
	}
	return lastErr
}
