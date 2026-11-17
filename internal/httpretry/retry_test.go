package httpretry

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

type clientFunc func(*http.Request) (*http.Response, error)

func (f clientFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestRecoveryReplayAndRetryAfter(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPut, "https://blob.invalid/?sig=private", strings.NewReader("identical-body"))
	req.Header.Set("Range", "bytes=0-2")
	failBody := &trackedBody{Reader: strings.NewReader("error")}
	var calls int
	var delays []time.Duration
	resp, err := do(clientFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if string(body) != "identical-body" || r.Header.Get("Range") != "bytes=0-2" || r.ContentLength != 14 {
			t.Fatalf("request replay changed: %s %+v", body, r)
		}
		if calls == 1 {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"999999999999"}}, Body: failBody}, nil
		}
		return &http.Response{StatusCode: 201, Body: http.NoBody}, nil
	}), req, func(_ context.Context, d time.Duration) error { delays = append(delays, d); return nil }, time.Now, func(d time.Duration) time.Duration { return d })
	if err != nil || resp.StatusCode != 201 || calls != 2 || !failBody.closed || len(delays) != 1 || delays[0] != maxDelay {
		t.Fatalf("unexpected retry: %+v %v calls=%d delays=%v closed=%v", resp, err, calls, delays, failBody.closed)
	}
}

func TestStatusAndTransportPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		err    error
		want   int
	}{
		{"busy", 503, nil, 3}, {"timeout", 408, nil, 3}, {"limit", 429, nil, 3},
		{"bad-input", 400, nil, 1}, {"auth", 401, nil, 1}, {"denied", 403, nil, 1},
		{"redirect", 307, nil, 1}, {"conflict", 409, nil, 1}, {"not-found", 404, nil, 1},
		{"unsupported", 501, nil, 1}, {"reset", 0, syscall.ECONNRESET, 3},
		{"eof", 0, io.EOF, 3}, {"arbitrary", 0, errors.New("invalid certificate"), 1},
		{"certificate", 0, &url.Error{Op: "Get", URL: "https://private.invalid", Err: x509.UnknownAuthorityError{}}, 1},
		{"cancel", 0, context.Canceled, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "https://test.invalid", nil)
			calls := 0
			resp, err := do(clientFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Body: http.NoBody}, nil
			}), req, func(context.Context, time.Duration) error { return nil }, time.Now, func(d time.Duration) time.Duration { return d })
			if resp != nil {
				resp.Body.Close()
			}
			if calls != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestCancellationDuringBackoffAndUnreplayableBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://test.invalid", nil)
	calls := 0
	_, err := do(clientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return &http.Response{StatusCode: 503, Body: http.NoBody}, nil
	}), req, wait, time.Now, func(d time.Duration) time.Duration { return d })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("%v calls=%d", err, calls)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err = wait(ctx, maxDelay); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPost, "https://test.invalid", io.NopCloser(strings.NewReader("one-shot")))
	calls = 0
	resp, _ := do(clientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 503, Body: http.NoBody}, nil
	}), req, func(context.Context, time.Duration) error { t.Fatal("one-shot body retried"); return nil }, time.Now, func(d time.Duration) time.Duration { return d })
	resp.Body.Close()
	if calls != 1 {
		t.Fatal(calls)
	}
}

func TestRetryAfterBounds(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"-1", 0}, {"garbage", 0}, {"1", time.Second}, {"999999999999999999999999", 0},
		{"999999999999", maxDelay}, {now.Add(time.Hour).Format(http.TimeFormat), maxDelay},
		{now.Add(-time.Second).Format(http.TimeFormat), 0},
	} {
		if got := retryAfter(tc.value, now); got != tc.want {
			t.Fatalf("%s: %s, want %s", tc.value, got, tc.want)
		}
	}
}
