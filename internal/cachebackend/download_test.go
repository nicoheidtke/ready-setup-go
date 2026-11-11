package cachebackend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func downloadFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "download-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func requestRange(t *testing.T, r *http.Request) (first, last int64) {
	t.Helper()
	if r.Header.Get("Authorization") != "" || r.Header.Get("Accept-Encoding") != "identity" || r.URL.Query().Get("sig") != secret {
		t.Error("invalid signed-blob headers")
	}
	if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &first, &last); err != nil {
		t.Errorf("invalid range header %q", r.Header.Get("Range"))
	}
	return first, last
}

func TestParallelRangeDownloadPreservesOffsetAndBoundsWorkers(t *testing.T) {
	const data = "abcdefghijklmnopqrstuvwxyz0123456789"
	var active, peak, requests atomic.Int32
	barrier := make(chan struct{})
	var opened sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		first, last := requestRange(t, r)
		if first > 0 {
			if r.Header.Get("If-Match") != `"immutable"` {
				t.Error("subsequent request not pinned to strong ETag")
			}
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			if n == 3 {
				opened.Do(func() { close(barrier) })
			}
			select {
			case <-barrier:
			case <-time.After(3 * time.Second):
				t.Error("range requests did not overlap")
			}
		}
		last = min(last, int64(len(data))-1)
		w.Header().Set("ETag", `"immutable"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, data[first:last+1])
	}))
	defer server.Close()
	f := downloadFile(t)
	_, _ = io.WriteString(f, "prefix")
	c := Client{Token: secret, DownloadConcurrency: 3, DownloadBlockSize: 4}
	n, err := c.Download(context.Background(), Entry{URL: server.URL + "?sig=" + secret}, f)
	if err != nil || n != int64(len(data)) {
		t.Fatalf("n=%d err=%v", n, err)
	}
	position, _ := f.Seek(0, io.SeekCurrent)
	_, _ = f.Seek(0, io.SeekStart)
	got, _ := io.ReadAll(f)
	if string(got) != "prefix"+data || position != int64(len("prefix")+len(data)) || peak.Load() != 3 || requests.Load() != 9 {
		t.Fatalf("data %q position %d peak %d requests %d", got, position, peak.Load(), requests.Load())
	}
}

func TestRangeDownloadFallbackAndSequentialOptOut(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(strconv.Itoa(workers), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if (r.Header.Get("Range") != "") != (workers > 1) {
					t.Error("unexpected range request")
				}
				_, _ = io.WriteString(w, "full response")
			}))
			defer server.Close()
			f := downloadFile(t)
			_, _ = io.WriteString(f, "prefix")
			c := Client{DownloadConcurrency: workers, DownloadBlockSize: 4}
			n, err := c.Download(context.Background(), Entry{URL: server.URL}, f)
			_, _ = f.Seek(0, io.SeekStart)
			got, _ := io.ReadAll(f)
			if err != nil || n != 13 || string(got) != "prefixfull response" || calls.Load() != 1 {
				t.Fatalf("n=%d data=%q calls=%d err=%v", n, got, calls.Load(), err)
			}
		})
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes */0")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		}
	}))
	defer server.Close()
	c := Client{}
	n, err := c.Download(context.Background(), Entry{URL: server.URL}, downloadFile(t))
	if err != nil || n != 0 || calls.Load() != 2 {
		t.Fatalf("empty object n=%d calls=%d err=%v", n, calls.Load(), err)
	}
}

func TestRangeDownloadRejectsMalformedResponses(t *testing.T) {
	for _, kind := range []string{"initial-range", "initial-limit", "range", "total", "length", "truncated", "extra", "encoding", "etag", "status", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				first, last := requestRange(t, r)
				body := "abcd"
				rangeHeader := fmt.Sprintf("bytes %d-%d/8", first, last)
				w.Header().Set("ETag", `"original"`)
				if first == 0 {
					if kind == "initial-range" {
						rangeHeader = "bytes 0-4/8"
					}
					if kind == "initial-limit" {
						rangeHeader = "bytes 0-3/100"
					}
				} else {
					switch kind {
					case "range":
						rangeHeader = "bytes 0-3/8"
					case "total":
						rangeHeader = "bytes 4-7/9"
					case "length":
						body = "abc"
						w.Header().Set("Content-Length", "3")
					case "truncated":
						body = "abc"
						w.Header().Set("Content-Length", "4")
					case "extra":
						body = "abcde"
					case "encoding":
						w.Header().Set("Content-Encoding", "gzip")
					case "etag":
						w.Header().Set("ETag", `"changed"`)
					case "status":
						w.WriteHeader(http.StatusOK)
						_, _ = io.WriteString(w, secret)
						return
					case "redirect":
						w.Header().Set("Location", "https://example.invalid/?sig="+secret)
						w.WriteHeader(http.StatusTemporaryRedirect)
						return
					}
				}
				w.Header().Set("Content-Range", rangeHeader)
				w.WriteHeader(http.StatusPartialContent)
				if kind == "extra" && first > 0 {
					w.(http.Flusher).Flush()
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			f := downloadFile(t)
			c := Client{MaxBytes: 8, DownloadConcurrency: 2, DownloadBlockSize: 4}
			n, err := c.Download(context.Background(), Entry{URL: server.URL + "?sig=" + secret}, f)
			st, _ := f.Stat()
			if err == nil || strings.Contains(err.Error(), secret) || n > 8 || st.Size() > 8 {
				t.Fatalf("unsafe result n=%d size=%d err=%v", n, st.Size(), err)
			}
			if strings.HasPrefix(kind, "initial-") && st.Size() != 0 {
				t.Fatal("invalid initial range wrote destination")
			}
		})
	}
}

func TestRangeDownloadCancellationJoinsWorkers(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(strconv.FormatBool(failure), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var seen atomic.Int32
			barrier := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				first, last := requestRange(t, r)
				if first > 0 {
					if seen.Add(1) == 3 {
						close(barrier)
						if !failure {
							cancel()
						}
					}
					select {
					case <-barrier:
					case <-r.Context().Done():
						return
					}
					if failure && first == 4 {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/32", first, last))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.WriteString(w, "abcd")
			}))
			defer server.Close()
			f := downloadFile(t)
			c := Client{DownloadConcurrency: 3, DownloadBlockSize: 4}
			n, err := c.Download(ctx, Entry{URL: server.URL + "?sig=" + secret}, f)
			wantRequests := int32(3)
			if failure {
				// The failing range exhausts three request attempts while the
				// other two workers remain blocked, then all workers join.
				wantRequests = 5
			}
			if err == nil || (!failure && !errors.Is(err, context.Canceled)) || (failure && !strings.Contains(err.Error(), "HTTP 500")) || n != 4 || seen.Load() != wantRequests {
				t.Fatalf("n=%d requests=%d err=%v", n, seen.Load(), err)
			}
			// Closing immediately after return is safe: client workers have all joined.
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDownloadSettingsValidation(t *testing.T) {
	for _, c := range []Client{{DownloadConcurrency: -1}, {DownloadConcurrency: 33}, {DownloadBlockSize: -1}, {DownloadBlockSize: 4001 << 20}, {MaxBytes: int64(^uint64(0) >> 1)}} {
		c.HTTPClient = &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
			t.Error("invalid settings reached network")
			return nil, errors.New("unexpected request")
		})}
		if _, err := c.Download(context.Background(), Entry{URL: "https://example.invalid/"}, io.Discard); err == nil {
			t.Fatal("accepted invalid download settings")
		}
	}
}

func TestRangeDownloadRejectsExtremeTotalBeforeWriting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-3/9223372036854775807")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "abcd")
	}))
	defer server.Close()
	f := downloadFile(t)
	c := Client{MaxBytes: 1 << 40, DownloadBlockSize: 4}
	n, err := c.Download(context.Background(), Entry{URL: server.URL}, f)
	info, _ := f.Stat()
	if err == nil || !strings.Contains(err.Error(), "size limit") || n != 0 || info.Size() != 0 {
		t.Fatalf("n=%d size=%d err=%v", n, info.Size(), err)
	}
}
