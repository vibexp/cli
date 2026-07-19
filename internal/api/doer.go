// Package api is the shared authenticated-client layer over
// github.com/vibexp/api-client-go: a retry/timeout transport, an auth request
// editor sourced from the credential store, a uniform RFC 7807 error mapper,
// and team/project resolution. Commands never touch the generated client
// directly — they go through New.
package api

import (
	crand "crypto/rand"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/vibexp/cli/internal/version"
)

const (
	defaultTimeout = 30 * time.Second
	defaultRetries = 3 // total attempts (initial + retries)
	baseBackoff    = 500 * time.Millisecond
	maxBackoff     = 5 * time.Second
)

// userAgent identifies the CLI on every request.
func userAgent() string {
	return "vibexp-cli/" + version.Version
}

// Doer implements the generated HttpRequestDoer interface, adding a request
// timeout, a User-Agent, and bounded exponential-backoff retries for transient
// failures on safe (retryable) methods only.
type Doer struct {
	base        *http.Client
	maxAttempts int
	// sleep and jitter are injectable for deterministic tests.
	sleep  func(time.Duration)
	jitter func(time.Duration) time.Duration
}

// NewDoer builds a Doer with the given per-request timeout (0 → default 30s).
func NewDoer(timeout time.Duration) *Doer {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Doer{
		base:        &http.Client{Timeout: timeout},
		maxAttempts: defaultRetries,
		sleep:       time.Sleep,
		jitter:      defaultJitter,
	}
}

// Do sends the request, retrying transient failures (429 / 5xx / transport
// errors) on retryable methods with exponential backoff + jitter, honoring
// Retry-After.
func (d *Doer) Do(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", userAgent())
	}
	retryable := isRetryableMethod(req.Method)

	var resp *http.Response
	var err error
	for attempt := 1; attempt <= d.maxAttempts; attempt++ {
		if rErr := rewindBody(req, attempt); rErr != nil {
			return nil, rErr
		}
		resp, err = d.base.Do(req)

		if !retryable || attempt == d.maxAttempts || !shouldRetry(resp, err) {
			return resp, err
		}
		d.waitBeforeRetry(attempt, resp)
	}
	return resp, err
}

// rewindBody resets a retried request's body from GetBody.
func rewindBody(req *http.Request, attempt int) error {
	if attempt == 1 || req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return err
	}
	req.Body = body
	return nil
}

// shouldRetry reports whether a transport error or a transient status warrants
// another attempt.
func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return isRetryableStatus(resp.StatusCode)
}

// waitBeforeRetry drains the response and sleeps for the computed backoff.
func (d *Doer) waitBeforeRetry(attempt int, resp *http.Response) {
	wait := d.backoff(attempt, resp)
	if resp != nil {
		drain(resp)
	}
	d.sleep(wait)
}

// backoff computes the wait before the next attempt, honoring Retry-After when
// present, else min(base·2^(n-1), max) plus jitter.
func (d *Doer) backoff(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if ra := retryAfter(resp); ra > 0 {
			return ra
		}
	}
	backoff := baseBackoff << (attempt - 1) // base * 2^(attempt-1)
	if backoff > maxBackoff || backoff <= 0 {
		backoff = maxBackoff
	}
	return backoff + d.jitter(backoff)
}

// isRetryableMethod reports whether a method is safe to retry. Only safe methods
// are retried; POST/PUT/PATCH/DELETE are never retried even though some are
// idempotent, to avoid duplicating side effects.
func isRetryableMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// retryAfter parses a Retry-After header (delta-seconds or HTTP date).
func retryAfter(resp *http.Response) time.Duration {
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// defaultJitter returns a random duration in [0, d/2). It uses crypto/rand —
// jitter needs no cryptographic strength, but this avoids flagging a weak PRNG
// and the volume is negligible (only on retries).
func defaultJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(d/2)+1))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64())
}

// drain reads and closes a response body so the connection can be reused.
func drain(resp *http.Response) {
	if resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
}
