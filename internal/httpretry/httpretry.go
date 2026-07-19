// Package httpretry provides a small, generic retry-with-backoff wrapper for
// HTTP collectors (secwatch, prwatch, market-data-watcher, etc.) so each one
// doesn't have to reinvent exponential backoff + jitter + retryable-status
// classification. secwatch/client.go has its own older, more specialized
// version of this same pattern (tightly coupled to its EDGAR-specific
// Client) — not migrated here to avoid a risky refactor of that
// already-working, heavily-depended-on code; new collectors should use this
// package, and secwatch can adopt it later if there's a reason to.
package httpretry

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"time"
)

// Options configures retry behavior. Zero-value fields fall back to sane
// defaults via WithDefaults.
type Options struct {
	MaxRetries  int           // number of retries after the first attempt (so MaxRetries=3 means up to 4 total attempts)
	BackoffBase time.Duration // base delay before the first retry
	BackoffCap  time.Duration // maximum delay between retries, regardless of attempt count
}

func (o Options) withDefaults() Options {
	if o.MaxRetries <= 0 {
		o.MaxRetries = 3
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = 500 * time.Millisecond
	}
	if o.BackoffCap <= 0 {
		o.BackoffCap = 8 * time.Second
	}
	return o
}

// StatusError wraps a non-2xx HTTP response so callers (and IsRetryableStatus)
// can inspect the status code without re-parsing an error string.
type StatusError struct {
	StatusCode int
	URL        string
}

func (e *StatusError) Error() string {
	return http.StatusText(e.StatusCode) + " (" + e.URLOrUnknown() + ")"
}

func (e *StatusError) URLOrUnknown() string {
	if e.URL == "" {
		return "unknown url"
	}
	return e.URL
}

// IsRetryableStatus reports whether an HTTP status code is worth retrying:
// rate-limited (429), a transient block that often clears (403 — some
// upstreams, including Yahoo Finance's unofficial API, return this for
// what's really a rate limit), or any 5xx server error.
func IsRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusForbidden ||
		(code >= 500 && code <= 599)
}

// IsRetryable reports whether err is worth retrying: a *StatusError with a
// retryable code, or any other non-nil error (network failures, timeouts,
// connection resets — anything that isn't a classified StatusError is
// assumed transient, matching secwatch's existing isRetryable convention).
func IsRetryable(err error) bool {
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return IsRetryableStatus(statusErr.StatusCode)
	}
	return err != nil
}

// backoffFor returns the delay before retry attempt N (0-indexed), doubling
// each attempt up to BackoffCap, with up to 50% jitter to avoid a thundering
// herd of collectors retrying in lockstep.
func backoffFor(opts Options, attempt int) time.Duration {
	base := opts.BackoffBase << attempt
	if base > opts.BackoffCap || base <= 0 { // overflow guard: shifted duration can wrap negative
		base = opts.BackoffCap
	}
	half := base / 2
	jitter := time.Duration(rand.Int63n(int64(half + 1)))
	return half + jitter
}

// Do runs fn with retries and exponential backoff+jitter. fn should return
// (result, nil) on success, or (zero-value, err) on failure — wrap HTTP
// errors in a *StatusError so IsRetryable can classify them correctly.
// Stops retrying (returns immediately) once IsRetryable(err) is false, once
// opts.MaxRetries is exhausted, or once ctx is cancelled.
func Do[T any](ctx context.Context, opts Options, fn func(ctx context.Context, attempt int) (T, error)) (T, error) {
	opts = opts.withDefaults()
	var zero T
	var lastErr error
	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		result, err := fn(ctx, attempt)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !IsRetryable(err) || attempt == opts.MaxRetries {
			break
		}
		if err := sleepWithBackoff(ctx, backoffFor(opts, attempt)); err != nil {
			return zero, err
		}
	}
	return zero, lastErr
}

func sleepWithBackoff(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
