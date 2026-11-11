package cachebackend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"setup-go-better/internal/actionlog"
)

func TestRetriesLookupAndGETButNeverPartiallyConsumedBody(t *testing.T) {
	var lookup, download, truncated atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, servicePath) {
			checkControl(t, r, "GetCacheEntryDownloadURL")
			if lookup.Add(1) == 1 {
				w.WriteHeader(503)
				return
			}
			jsonResponse(w, map[string]any{"ok": false})
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("token sent to blob retry")
		}
		if r.URL.Path == "/truncated" {
			truncated.Add(1)
			w.Header().Set("Content-Length", "5")
			_, _ = io.WriteString(w, "ab")
			return
		}
		if download.Add(1) == 1 {
			w.WriteHeader(502)
			return
		}
		_, _ = io.WriteString(w, "abc")
	}))
	defer server.Close()
	c := Client{BaseURL: server.URL, Token: secret, DownloadConcurrency: 1}
	if entry, err := c.Lookup(context.Background(), "key", "version", nil); err != nil || entry != nil || lookup.Load() != 2 {
		t.Fatalf("lookup: %v %v calls=%d", entry, err, lookup.Load())
	}
	var dst bytes.Buffer
	if n, err := c.Download(context.Background(), Entry{URL: server.URL + "/blob?sig=" + secret}, &dst); err != nil || n != 3 || dst.String() != "abc" || download.Load() != 2 {
		t.Fatalf("download: %d %v %q calls=%d", n, err, dst.String(), download.Load())
	}
	dst.Reset()
	if n, err := c.Download(context.Background(), Entry{URL: server.URL + "/truncated"}, &dst); err == nil || n != 2 || truncated.Load() != 1 {
		t.Fatalf("partial response retried: %d %v calls=%d", n, err, truncated.Load())
	}
}

func TestRetriesDeterministicPayloadsAndBlockList(t *testing.T) {
	for _, blockSize := range []int64{100, 2} {
		t.Run(fmt.Sprintf("block-size-%d", blockSize), func(t *testing.T) {
			var mu sync.Mutex
			counts := make(map[string]int)
			bodies := make(map[string]string)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, servicePath) {
					if strings.HasSuffix(r.URL.Path, "CreateCacheEntry") {
						jsonResponse(w, map[string]any{"ok": true, "signed_upload_url": "http://" + r.Host + "/blob?sig=" + secret})
					} else {
						jsonResponse(w, map[string]any{"ok": true})
					}
					return
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("token sent to blob")
				}
				key := r.URL.Query().Get("comp") + r.URL.Query().Get("blockid")
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				defer mu.Unlock()
				counts[key]++
				if counts[key] == 1 {
					bodies[key] = string(body)
					w.WriteHeader(503)
					return
				}
				if string(body) != bodies[key] || r.ContentLength != int64(len(body)) {
					t.Errorf("replayed body changed: %q vs %q", body, bodies[key])
				}
				// Slow successful payload acknowledgement forces the progress
				// line to expose whether a failed attempt was double-counted.
				if key == "" {
					time.Sleep(2100 * time.Millisecond)
				}
				w.WriteHeader(201)
			}))
			defer server.Close()
			var log bytes.Buffer
			ctx, finish := actionlog.New(&log, true).Transfer(context.Background(), "Upload", 6, false)
			c := Client{BaseURL: server.URL, Token: secret, BlockSize: blockSize, Concurrency: 2}
			err := c.Upload(ctx, "key", "v", strings.NewReader("abcdef"), 6)
			finish(err)
			if err != nil {
				t.Fatal(err)
			}
			for key, count := range counts {
				if count != 2 {
					t.Fatalf("%s: %d attempts", key, count)
				}
			}
			if blockSize == 100 && (!strings.Contains(log.String(), "6 B / 6 B · finished") || strings.Contains(log.String(), "12 B")) {
				t.Fatalf("double-counted upload progress: %q", log.String())
			}
		})
	}
}

func TestReservationAndFinalizationAreNotRetried(t *testing.T) {
	for _, method := range []string{"CreateCacheEntry", "FinalizeCacheEntryUpload"} {
		for _, transportFailure := range []bool{false, true} {
			calls := 0
			c := Client{BaseURL: "https://cache.invalid", Token: secret, HTTPClient: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if transportFailure {
					return nil, io.EOF
				}
				return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":"unavailable"}`))}, nil
			})}}
			var output any
			err := c.rpc(context.Background(), method, map[string]string{"key": "k", "version": "v"}, &output)
			if err == nil || calls != 1 || strings.Contains(err.Error(), "cache.invalid") {
				t.Fatalf("ambiguous %s retried: calls=%d err=%v", method, calls, err)
			}
		}
	}
}

func TestRetryDelayCancelsWithoutCredentialExposure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "100")
		w.WriteHeader(429)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var dst bytes.Buffer
	_, err := (&Client{}).Download(ctx, Entry{URL: server.URL + "?sig=" + secret}, &dst)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 || strings.Contains(err.Error(), secret) {
		t.Fatalf("delay cancellation: %v calls=%d", err, calls.Load())
	}
}
