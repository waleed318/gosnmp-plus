package retry

import (
	"math"
	"math/rand"
	"time"
)

// Fixed returns a BackoffStrategy that waits d before every retry attempt.
func Fixed(d time.Duration) BackoffStrategy {
	return fixedBackoff{d: d}
}

type fixedBackoff struct {
	d time.Duration
}

func (f fixedBackoff) Next(_ int) time.Duration {
	return f.d
}

// Exponential returns a BackoffStrategy where the wait grows as
// base × multiplier^attempt, capped at max.
// attempt is 1-based, so the first retry waits base × multiplier.
func Exponential(base time.Duration, multiplier float64, max time.Duration) BackoffStrategy {
	return exponentialBackoff{base: base, multiplier: multiplier, max: max}
}

type exponentialBackoff struct {
	base       time.Duration
	multiplier float64
	max        time.Duration
}

func (e exponentialBackoff) Next(attempt int) time.Duration {
	d := time.Duration(float64(e.base) * math.Pow(e.multiplier, float64(attempt)))
	if d > e.max {
		return e.max
	}
	return d
}

// Jitter wraps strategy and adds uniformly distributed ±25% random jitter to each delay.
// The returned delay is in the range [base×0.75, base×1.25).
func Jitter(strategy BackoffStrategy) BackoffStrategy {
	return jitterBackoff{inner: strategy}
}

type jitterBackoff struct {
	inner BackoffStrategy
}

func (j jitterBackoff) Next(attempt int) time.Duration {
	base := j.inner.Next(attempt)
	scale := 0.75 + rand.Float64()*0.5 //nolint:gosec // jitter does not require cryptographic randomness
	return time.Duration(float64(base) * scale)
}
