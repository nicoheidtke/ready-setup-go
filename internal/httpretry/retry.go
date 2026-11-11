// Package httpretry retries explicitly replay-safe HTTP requests before callers
// consume a response. Callers retain responsibility for operation idempotency.
package httpretry

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

const attempts = 3
const maxDelay = 5 * time.Second

type Client interface {
	Do(*http.Request) (*http.Response, error)
}

// Do makes at most three attempts, with cancellable jittered backoff capped at
// five seconds. The request body must be absent or replayable with GetBody.
// Successful responses are returned untouched: partial reads, malformed content
// and integrity failures are never retried here. Errors are returned unchanged
// for the caller to classify/sanitize; this package never logs request details.
func Do(client Client, req *http.Request) (*http.Response, error) {
	return do(client, req, wait, time.Now, func(n time.Duration) time.Duration {
		return n/2 + time.Duration(rand.Int64N(int64(n/2)+1))
	})
}

func do(client Client, req *http.Request, sleep func(context.Context, time.Duration) error, now func() time.Time, jitter func(time.Duration) time.Duration) (*http.Response, error) {
	canReplay := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	for attempt := 0; ; attempt++ {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		call := req
		if attempt > 0 {
			call = req.Clone(req.Context())
			if req.Body != nil && req.Body != http.NoBody {
				var err error
				call.Body, err = req.GetBody()
				if err != nil {
					return nil, err
				}
			}
		}
		resp, err := client.Do(call)
		if req.Context().Err() != nil {
			if resp != nil && resp.Body != nil {
				resp.Body.Close()
			}
			return nil, req.Context().Err()
		}
		if !canReplay || attempt == attempts-1 || !retryable(resp, err) {
			return resp, err
		}
		delay := jitter(500 * time.Millisecond << attempt)
		if resp != nil {
			if serverDelay := retryAfter(resp.Header.Get("Retry-After"), now()); serverDelay > delay {
				delay = serverDelay
			}
			if resp.Body != nil {
				// Close without draining arbitrary error bodies: an unavailable
				// service must not block cancellation or cause unbounded reads.
				resp.Body.Close()
			}
		}
		if err := sleep(req.Context(), min(delay, maxDelay)); err != nil {
			return nil, err
		}
	}
}

func retryable(resp *http.Response, err error) bool {
	if err == nil {
		if resp == nil {
			return false
		}
		switch resp.StatusCode {
		case 408, 429, 500, 502, 503, 504:
			return true
		}
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var network net.Error
	return errors.As(err, &network) && (network.Timeout() || network.Temporary())
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		if seconds >= int64(maxDelay/time.Second) {
			return maxDelay
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(0, min(date.Sub(now), maxDelay))
	}
	return 0
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
