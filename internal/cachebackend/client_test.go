package cachebackend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const secret = "private-runtime-token"

func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func checkControl(t *testing.T, r *http.Request, method string) map[string]any {
	t.Helper()
	if r.Method != "POST" || r.URL.Path != servicePath+method {
		t.Errorf("unexpected control request %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("Content-Type") != "application/json" {
		t.Error("invalid control authentication or content type")
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	return body
}

func TestRoundTripSingleBlob(t *testing.T) {
	for _, data := range []string{"", "archive-bytes"} {
		t.Run(fmt.Sprintf("size%d", len(data)), func(t *testing.T) {
			var stored string
			var finalized bool
			blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("runtime token sent to blob service")
				}
				if r.URL.Query().Get("sig") != "signed-secret" {
					t.Error("missing signed credential")
				}
				switch r.Method {
				case "PUT":
					if r.ContentLength != int64(len(data)) || r.Header.Get("x-ms-blob-type") != "BlockBlob" || r.Header.Get("x-ms-version") == "" {
						t.Error("invalid blob upload headers")
					}
					b, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
					}
					stored = string(b)
					w.WriteHeader(http.StatusCreated)
				case "GET":
					if r.Header.Get("Accept-Encoding") != "identity" {
						t.Error("download must not transform archive")
					}
					_, _ = io.WriteString(w, stored)
				default:
					t.Errorf("unexpected blob method %s", r.Method)
				}
			}))
			defer blob.Close()
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch strings.TrimPrefix(r.URL.Path, servicePath) {
				case "CreateCacheEntry":
					if got := checkControl(t, r, "CreateCacheEntry"); !reflect.DeepEqual(got, map[string]any{"key": "snapshot", "version": "schema"}) {
						t.Errorf("reservation: %#v", got)
					}
					jsonResponse(w, map[string]any{"ok": true, "signed_upload_url": blob.URL + "/archive?sig=signed-secret"})
				case "FinalizeCacheEntryUpload":
					if stored != data {
						t.Error("finalized before complete blob upload")
					}
					if got := checkControl(t, r, "FinalizeCacheEntryUpload"); !reflect.DeepEqual(got, map[string]any{"key": "snapshot", "version": "schema", "size_bytes": strconv.Itoa(len(data))}) {
						t.Errorf("finalization: %#v", got)
					}
					finalized = true
					jsonResponse(w, map[string]any{"ok": true, "entry_id": "123"})
				case "GetCacheEntryDownloadURL":
					if got := checkControl(t, r, "GetCacheEntryDownloadURL"); !reflect.DeepEqual(got, map[string]any{"key": "latest", "version": "schema", "restore_keys": []any{"snap", "fallback"}}) {
						t.Errorf("lookup: %#v", got)
					}
					jsonResponse(w, map[string]any{"ok": true, "matched_key": "snapshot", "signed_download_url": blob.URL + "/archive?sig=signed-secret"})
				default:
					t.Errorf("unknown request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer service.Close()
			c := Client{BaseURL: service.URL + "/ignored/base?ignored=query", Token: secret}
			if err := c.Upload(context.Background(), "snapshot", "schema", strings.NewReader(data), int64(len(data))); err != nil {
				t.Fatal(err)
			}
			if !finalized {
				t.Fatal("entry not finalized")
			}
			entry, err := c.Lookup(context.Background(), "latest", "schema", []string{"snap", "fallback"})
			if err != nil {
				t.Fatal(err)
			}
			if entry.Key != "snapshot" {
				t.Fatal(entry.Key)
			}
			var dst bytes.Buffer
			n, err := c.Download(context.Background(), *entry, &dst)
			if err != nil || n != int64(len(data)) || dst.String() != data {
				t.Fatalf("download n=%d err=%v bytes=%q", n, err, dst.String())
			}
		})
	}
}

// A ReadSeeker without ReaderAt exercises the mutex-protected adapter too.
type seekOnly struct{ io.ReadSeeker }

func TestParallelBlockUpload(t *testing.T) {
	for _, readerAt := range []bool{true, false} {
		t.Run(fmt.Sprint(readerAt), func(t *testing.T) {
			const data = "abcdefghijklmnopq"
			var mu sync.Mutex
			blocks := map[string]string{}
			var committed string
			var active, peak atomic.Int32
			barrier := make(chan struct{})
			blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.URL.Query().Get("sig") != "signed-secret" {
					t.Error("invalid blob credentials")
				}
				switch r.URL.Query().Get("comp") {
				case "block":
					current := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
					}
					if current == 2 {
						select {
						case <-barrier:
						default:
							close(barrier)
						}
					}
					select {
					case <-barrier:
					case <-time.After(3 * time.Second):
						t.Error("block uploads did not overlap")
					}
					b, _ := io.ReadAll(r.Body)
					if len(b) > 4 || r.ContentLength != int64(len(b)) {
						t.Error("wrong block length")
					}
					id := r.URL.Query().Get("blockid")
					decoded, err := base64.StdEncoding.DecodeString(id)
					if err != nil || len(decoded) != 8 {
						t.Error("invalid block ID")
					}
					mu.Lock()
					blocks[id] = string(b)
					mu.Unlock()
				case "blocklist":
					var list struct {
						IDs []string `xml:"Latest"`
					}
					if err := xml.NewDecoder(r.Body).Decode(&list); err != nil {
						t.Error(err)
					}
					mu.Lock()
					for _, id := range list.IDs {
						committed += blocks[id]
					}
					mu.Unlock()
				default:
					t.Error("expected block protocol")
				}
				w.WriteHeader(201)
			}))
			defer blob.Close()
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "CreateCacheEntry") {
					jsonResponse(w, map[string]any{"ok": true, "signed_upload_url": blob.URL + "?sig=signed-secret"})
					return
				}
				if committed != data {
					t.Errorf("finalize with wrong committed data %q", committed)
				}
				jsonResponse(w, map[string]any{"ok": true})
			}))
			defer service.Close()
			c := Client{BaseURL: service.URL, Token: secret, BlockSize: 4, Concurrency: 2}
			var src io.ReadSeeker = strings.NewReader(data)
			if !readerAt {
				src = seekOnly{src}
			}
			if err := c.Upload(context.Background(), "key", "version", src, int64(len(data))); err != nil {
				t.Fatal(err)
			}
			if peak.Load() != 2 || committed != data {
				t.Fatalf("peak %d data %q", peak.Load(), committed)
			}
		})
	}
}

func TestLookupResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
		miss   bool
	}{
		{"miss", 200, `{"ok":false}`, nil, true},
		{"permission", 403, `{"msg":"` + secret + `"}`, ErrPermission, false},
		{"conflict", 409, `{"msg":"` + secret + `"}`, ErrConflict, false},
		{"twirp-conflict", 400, `{"code":"already_exists","msg":"` + secret + `"}`, ErrConflict, false},
		{"twirp-denied", 400, `{"code":"permission_denied","msg":"` + secret + `"}`, ErrPermission, false},
		{"server", 503, secret, errors.New("HTTP 503"), false},
		{"malformed", 200, secret, errors.New("JSON"), false},
		{"no-key", 200, `{"ok":true,"signed_download_url":"https://blob.example/?sig=` + secret + `"}`, errors.New("matched key"), false},
		{"invalid-url", 200, `{"ok":true,"matched_key":"k","signed_download_url":"http://blob.example/?sig=` + secret + `"}`, errors.New("invalid download URL"), false},
		{"oversized", 200, strings.Repeat("x", maxControlResponse+1), errors.New("size limit"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				checkControl(t, r, "GetCacheEntryDownloadURL")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c := Client{BaseURL: server.URL, Token: secret}
			entry, err := c.Lookup(context.Background(), "key", "version", nil)
			if tc.miss {
				if err != nil || entry != nil {
					t.Fatalf("entry=%v error=%v", entry, err)
				}
				return
			}
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe or missing error: %v", err)
			}
			if tc.want == ErrConflict || tc.want == ErrPermission {
				if !errors.Is(err, tc.want) {
					t.Fatalf("error %v want %v", err, tc.want)
				}
			} else if !strings.Contains(err.Error(), tc.want.Error()) {
				t.Fatalf("error %v want %v", err, tc.want)
			}
		})
	}
}

func TestDownloadBoundedAndTruncated(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		length string
		flush  bool
		limit  int64
		want   int64
		fails  bool
	}{
		{"exact", "1234", "", false, 4, 4, false},
		{"known-large", "12345", "", false, 4, 0, true},
		{"chunked-large", "12345", "", true, 4, 4, true},
		{"chunked-exact", "1234", "", true, 4, 4, false},
		{"truncated", "12", "4", false, 4, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("token sent on download")
				}
				if tc.length != "" {
					w.Header().Set("Content-Length", tc.length)
				}
				if tc.flush {
					w.(http.Flusher).Flush()
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			c := Client{MaxBytes: tc.limit, Token: secret}
			var dst bytes.Buffer
			n, err := c.Download(context.Background(), Entry{URL: server.URL + "?sig=" + secret}, &dst)
			if (err != nil) != tc.fails || n != tc.want || int64(dst.Len()) > tc.limit {
				t.Fatalf("n %d bytes %q err %v", n, dst.String(), err)
			}
			if err != nil && strings.Contains(err.Error(), secret) {
				t.Fatal("leaked credential")
			}
		})
	}
}

func TestUploadFailureDoesNotFinalize(t *testing.T) {
	for _, tc := range []struct {
		name       string
		reserve    string
		blobStatus int
		final      string
		want       error
	}{
		{"reservation", `{"ok":false,"message":"` + secret + `"}`, 201, `{"ok":true}`, nil},
		{"denied", `{"ok":false,"message":"cache write denied: ` + secret + `"}`, 201, `{"ok":true}`, ErrPermission},
		{"invalid-url", `{"ok":true,"signed_upload_url":"http://example.com/?sig=` + secret + `"}`, 201, `{"ok":true}`, nil},
		{"blob-conflict", "", 409, `{"ok":true}`, ErrConflict},
		{"blob-failed", "", 500, `{"ok":true}`, nil},
		{"final-refused", "", 201, `{"ok":false,"message":"` + secret + `"}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var finalize atomic.Int32
			blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.WriteHeader(tc.blobStatus)
				_, _ = io.WriteString(w, secret)
			}))
			defer blob.Close()
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "CreateCacheEntry") {
					if tc.reserve != "" {
						_, _ = io.WriteString(w, tc.reserve)
					} else {
						jsonResponse(w, map[string]any{"ok": true, "signed_upload_url": blob.URL + "?sig=" + secret})
					}
					return
				}
				finalize.Add(1)
				_, _ = io.WriteString(w, tc.final)
			}))
			defer service.Close()
			c := Client{BaseURL: service.URL, Token: secret, BlockSize: 2}
			err := c.Upload(context.Background(), "key", "version", strings.NewReader("12345"), 5)
			if err == nil || strings.Contains(err.Error(), secret) {
				t.Fatalf("unsafe or missing error %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("want %v got %v", tc.want, err)
			}
			if tc.name != "final-refused" && finalize.Load() != 0 {
				t.Fatal("finalized failed transfer")
			}
		})
	}
}

func TestRedirectsNeverForwardCredentials(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests.Add(1)
		t.Errorf("followed redirect with authorization %q", r.Header.Get("Authorization"))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"?sig="+secret, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	c := Client{BaseURL: redirect.URL, Token: secret, HTTPClient: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}}
	if _, err := c.Lookup(context.Background(), "key", "v", nil); err == nil {
		t.Fatal("accepted control redirect")
	}
	if _, err := c.Download(context.Background(), Entry{URL: redirect.URL + "?sig=" + secret}, io.Discard); err == nil {
		t.Fatal("accepted download redirect")
	}
	if err := c.put(context.Background(), mustURL(t, redirect.URL+"?sig="+secret), strings.NewReader("data"), 4, "application/octet-stream", true); err == nil {
		t.Fatal("accepted upload redirect")
	}
	if targetRequests.Load() != 0 {
		t.Fatal("redirect target contacted")
	}
}

func TestCancellationAndTransportErrorsAreSanitized(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			c := Client{BaseURL: "https://cache.example/", Token: secret, HTTPClient: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) { return nil, fmt.Errorf("%s %s", secret, r.URL.String()) })}}
			_, err := c.Lookup(ctx, "key", "v", nil)
			if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "example") {
				t.Fatalf("unsafe error %v", err)
			}
			if cancelled && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation %v", err)
			}
			_, err = c.Download(ctx, Entry{URL: "https://blob.example/?sig=" + secret}, io.Discard)
			if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "example") {
				t.Fatalf("unsafe download error %v", err)
			}
		})
	}
}

func TestCancellationDuringDownload(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "partial")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	var dst bytes.Buffer
	c := Client{}
	n, err := c.Download(ctx, Entry{URL: server.URL + "?sig=" + secret}, cancelWriter{dst: &dst, cancel: cancel})
	if n == 0 || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), secret) {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

type cancelWriter struct {
	dst    io.Writer
	cancel context.CancelFunc
}

func (w cancelWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.cancel()
	return n, err
}

func TestCancellationDuringUploadNeverFinalizes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var finalized atomic.Int32
	blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		cancel()
		<-r.Context().Done()
	}))
	defer blob.Close()
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "CreateCacheEntry") {
			jsonResponse(w, map[string]any{"ok": true, "signed_upload_url": blob.URL + "?sig=" + secret})
			return
		}
		finalized.Add(1)
		jsonResponse(w, map[string]any{"ok": true})
	}))
	defer service.Close()
	c := Client{BaseURL: service.URL, Token: secret, BlockSize: 2}
	err := c.Upload(ctx, "key", "v", strings.NewReader("12345678"), 8)
	if !errors.Is(err, context.Canceled) || finalized.Load() != 0 {
		t.Fatalf("error %v finalized %d", err, finalized.Load())
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := parseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestValidationBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	c := Client{BaseURL: "https://cache.example/", Token: secret, HTTPClient: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected request")
	})}}
	for _, key := range []string{"", strings.Repeat("k", 513), "a,b", "a\nb"} {
		if _, err := c.Lookup(context.Background(), key, "v", nil); err == nil {
			t.Error("invalid key accepted")
		}
	}
	if _, err := c.Lookup(context.Background(), "k", "", nil); err == nil {
		t.Error("empty version accepted")
	}
	if _, err := c.Lookup(context.Background(), "k", "v", make([]string, 10)); err == nil {
		t.Error("excess restore keys accepted")
	}
	for _, tc := range []struct {
		size   int64
		reader string
	}{{-1, ""}, {MaxArchiveSize + 1, ""}, {4, "123"}} {
		if err := c.Upload(context.Background(), "k", "v", strings.NewReader(tc.reader), tc.size); err == nil {
			t.Error("invalid size accepted")
		}
	}
	for _, settings := range []Client{{MaxBytes: -1}, {MaxBytes: 1 << 41}, {BlockSize: -1}, {BlockSize: 4001 << 20}, {Concurrency: -1}, {Concurrency: 33}} {
		if _, _, _, err := settings.settings(); err == nil {
			t.Error("invalid transfer settings accepted")
		}
	}
	for _, raw := range []string{"", "://bad", "http://example.com", "https://user:password@example.com", "https://example.com/#secret", "file:///tmp/cache"} {
		if _, err := parseURL(raw); err == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid inputs caused network request")
	}
}
