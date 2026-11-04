package nim

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// Retry defaults: 4 attempts, exponential backoff with full jitter.
const (
	defaultMaxAttempts = 4
	retryBaseDelay     = 500 * time.Millisecond
	retryMaxDelay      = 8 * time.Second
)

// retryAfter parses the Retry-After header: seconds or an HTTP date.
// A missing or unparsable header yields 0.
func retryAfter(h http.Header, now time.Time) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(v); err == nil {
		if d := when.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// retryable reports whether a failed request should be tried again.
// 401/403/404/410 and other 4xx client errors are permanent; 429, 5xx and
// transport-level failures are retried.
func retryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
	}
	return true
}

// backoff returns the wait before attempt n (1-based), capped at retryMaxDelay,
// with full jitter so concurrent callers do not stampede.
func backoff(n int, rnd *rand.Rand) time.Duration {
	d := time.Duration(float64(retryBaseDelay) * math.Pow(2, float64(n-1)))
	if d > retryMaxDelay || d <= 0 {
		d = retryMaxDelay
	}
	return time.Duration(rnd.Int63n(int64(d) + 1))
}

// retryWait combines the server's Retry-After with the jittered backoff,
// preferring the server's own advice.
func retryWait(n int, h http.Header, rnd *rand.Rand) time.Duration {
	if h != nil {
		if d := retryAfter(h, time.Now()); d > 0 {
			return d
		}
	}
	return backoff(n, rnd)
}

// wait sleeps for d or until ctx is done, returning ctx's error then.
// It is a variable so tests can fast-forward the sleep.
var wait = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// retry wraps a single request attempt with the retry loop shared by all
// client calls. headers, when non-nil, carries the last response's headers so
// Retry-After is honored.
func (c *Client) retry(ctx context.Context, fn func() error, headers func() http.Header) error {
	maxAttempts := c.maxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = fn()
		if err == nil || !retryable(err) {
			return err
		}
		if attempt == maxAttempts {
			break
		}
		var h http.Header
		if headers != nil {
			h = headers()
		}
		if werr := wait(ctx, retryWait(attempt, h, c.rnd)); werr != nil {
			return fmt.Errorf("retry aborted: %w", werr)
		}
	}
	return err
}
