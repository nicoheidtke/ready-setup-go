package installer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSelectors(t *testing.T) {
	for _, s := range []string{"1.26", "1.26.x", "1.26.0", "1.27.42", "1.100.0"} {
		if _, e := ParseSelector(s); e != nil {
			t.Errorf("%s: %v", s, e)
		}
	}
	for _, s := range []string{"", "1.25.9", "go1.26.0", "v1.26.0", "1.26rc1", "1.26.0-beta1", "stable", "^1.26.0", "1.026", "1.26.01", "1.26.999999999999999999999999", "2.0.0", "1.26.0\n"} {
		if _, e := ParseSelector(s); e == nil {
			t.Errorf("accepted %q", s)
		}
	}
}
func testConfig(t *testing.T) Config {
	t.Helper()
	c := DefaultConfig()
	c.OS, c.Arch = "linux", "amd64"
	c.InstallDir = filepath.Join(t.TempDir(), "path with spaces", "toolchains")
	c.ToolCache = t.TempDir()
	return c
}
func mustWrite(t *testing.T, p string, b []byte) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, b, 0755); e != nil {
		t.Fatal(e)
	}
}
func seed(t *testing.T, c Config, version string, runner, marker bool) string {
	t.Helper()
	root := filepath.Join(c.InstallDir, version, c.OS+"-"+c.Arch)
	mark := filepath.Join(root, ".complete")
	if runner {
		arch := c.Arch
		if arch == "amd64" {
			arch = "x64"
		}
		root = filepath.Join(c.ToolCache, "go", version, arch)
		mark = root + ".complete"
	}
	mustWrite(t, filepath.Join(root, "VERSION"), []byte("go"+version+"\ntime irrelevant\n"))
	mustWrite(t, filepath.Join(root, "bin", executable(c.OS)), []byte("fixture"))
	if marker {
		mustWrite(t, mark, nil)
	}
	return root
}

type rejectTransport struct{ calls atomic.Int64 }

func (r *rejectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls.Add(1)
	return nil, fmt.Errorf("network disabled")
}
func TestInstalledWithoutNetwork(t *testing.T) {
	for _, runner := range []bool{false, true} {
		for _, latest := range []bool{false, true} {
			t.Run(fmt.Sprintf("runner=%t/latest=%t", runner, latest), func(t *testing.T) {
				c := testConfig(t)
				root := seed(t, c, "1.26.2", runner, true)
				seed(t, c, "1.26.3", runner, false)
				transport := &rejectTransport{}
				c.Client = &http.Client{Transport: transport}
				r, e := c.Setup(context.Background(), "1.26.2", latest)
				if e != nil || r.Root != root {
					t.Fatalf("%+v %v", r, e)
				}
				if transport.calls.Load() != 0 {
					t.Fatal("network was accessed")
				}
			})
		}
	}
}
func TestHighestInstalledPatch(t *testing.T) {
	c := testConfig(t)
	seed(t, c, "1.26.2", false, true)
	root := seed(t, c, "1.26.10", true, true)
	seed(t, c, "1.26.11", false, false)
	c.Client = &http.Client{Transport: &rejectTransport{}}
	r, e := c.Setup(context.Background(), "1.26", false)
	if e != nil || r.Root != root {
		t.Fatalf("%+v %v", r, e)
	}
}
func archiveFixture(t *testing.T, osName, version string) []byte {
	t.Helper()
	files := map[string]string{"go/VERSION": "go" + version + "\n", "go/bin/" + executable(osName): "fixture compiler", "go/src/hello.txt": "hello"}
	var b bytes.Buffer
	if osName == "windows" {
		w := zip.NewWriter(&b)
		for name, body := range files {
			f, e := w.Create(name)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = io.WriteString(f, body); e != nil {
				t.Fatal(e)
			}
		}
		if e := w.Close(); e != nil {
			t.Fatal(e)
		}
	} else {
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		for name, body := range files {
			if e := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body))}); e != nil {
				t.Fatal(e)
			}
			if _, e := io.WriteString(tw, body); e != nil {
				t.Fatal(e)
			}
		}
		if e := tw.Close(); e != nil {
			t.Fatal(e)
		}
		if e := gz.Close(); e != nil {
			t.Fatal(e)
		}
	}
	return b.Bytes()
}
func serve(t *testing.T, c *Config, payload []byte, version string, badHash, truncated, historical bool) *atomic.Int64 {
	t.Helper()
	hash := sha256.Sum256(payload)
	sum := hex.EncodeToString(hash[:])
	if badHash {
		sum = strings.Repeat("0", 64)
	}
	suffix := ".tar.gz"
	if c.OS == "windows" {
		suffix = ".zip"
	}
	a := archive{Filename: "go" + version + "." + c.OS + "-" + c.Arch + suffix, OS: c.OS, Arch: c.Arch, SHA256: sum, Size: int64(len(payload)), Kind: "archive"}
	var requests atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/dl/" {
			if historical && r.URL.Query().Get("include") != "all" {
				io.WriteString(w, "[]")
				return
			}
			json.NewEncoder(w).Encode([]release{{Version: "go" + version, Stable: true, Files: []archive{a}}})
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		body := payload
		if truncated {
			body = body[:len(body)/2]
		}
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	c.Client = s.Client()
	c.MetadataURL = s.URL + "/dl/?mode=json"
	c.DownloadURL = s.URL + "/files/"
	return &requests
}
func TestDownloadAndReuse(t *testing.T) {
	for _, osName := range []string{"linux", "darwin", "windows"} {
		t.Run(osName, func(t *testing.T) {
			c := testConfig(t)
			c.OS = osName
			body := archiveFixture(t, osName, "1.26.2")
			calls := serve(t, &c, body, "1.26.2", false, false, true)
			r, e := c.Setup(context.Background(), "1.26.2", false)
			if e != nil {
				t.Fatal(e)
			}
			if calls.Load() != 3 {
				t.Fatalf("expected current, historical, archive; got %d", calls.Load())
			}
			if r.Metrics.ArchiveBytes != int64(len(body)) || !complete(r.Root, Version{26, 2}, osName) {
				t.Fatalf("bad result %+v", r)
			}
			r2, e := c.Setup(context.Background(), "1.26.2", true)
			if e != nil || r2.Source != "installed" || calls.Load() != 3 {
				t.Fatalf("reuse: %+v %v", r2, e)
			}
			entries, e := os.ReadDir(filepath.Dir(r.Root))
			if e != nil {
				t.Fatal(e)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".staging-") {
					t.Fatal("staging leaked")
				}
			}
		})
	}
}

func TestResolvedVersionAnnouncedBeforeInstallation(t *testing.T) {
	c := testConfig(t)
	body := archiveFixture(t, c.OS, "1.26.2")
	requests := serve(t, &c, body, "1.26.2", false, false, false)
	called := 0
	r, err := c.SetupResolved(context.Background(), "1.26", true, func(version string) {
		called++
		if version != "1.26.2" || requests.Load() != 1 {
			t.Fatalf("callback after download or wrong version: %q %d", version, requests.Load())
		}
		if complete(filepath.Join(c.InstallDir, version, c.OS+"-"+c.Arch), Version{26, 2}, c.OS) {
			t.Fatal("callback waited for installation")
		}
	})
	if err != nil || called != 1 || r.Version != "1.26.2" {
		t.Fatal(r, called, err)
	}
	called = 0
	_, err = c.SetupResolved(context.Background(), "1.26.2", false, func(version string) {
		called++
		if version != "1.26.2" {
			t.Fatal(version)
		}
	})
	if err != nil || called != 1 || requests.Load() != 2 {
		t.Fatal(called, requests.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.InstallDir = t.TempDir()
	_, err = c.SetupResolved(ctx, "1.26.2", false, func(string) { cancel() })
	if err == nil {
		t.Fatal("cancelled installation succeeded")
	}
}
func TestLatestChecksMetadata(t *testing.T) {
	c := testConfig(t)
	seed(t, c, "1.26.1", false, true)
	body := archiveFixture(t, c.OS, "1.26.2")
	calls := serve(t, &c, body, "1.26.2", false, false, false)
	r, e := c.Setup(context.Background(), "1.26.x", true)
	if e != nil || r.Version != "1.26.2" || calls.Load() != 2 {
		t.Fatalf("%+v %v requests=%d", r, e, calls.Load())
	}
	r, e = c.Setup(context.Background(), "1.26.x", true)
	if e != nil || r.Source != "installed" || calls.Load() != 3 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestFailedDownloadNeverPublished(t *testing.T) {
	for _, osName := range []string{"linux", "windows"} {
		for _, failure := range []string{"checksum", "interrupted"} {
			t.Run(osName+failure, func(t *testing.T) {
				c := testConfig(t)
				c.OS = osName
				old := seed(t, c, "1.26.1", false, true)
				body := archiveFixture(t, osName, "1.26.2")
				serve(t, &c, body, "1.26.2", failure == "checksum", failure == "interrupted", false)
				if _, e := c.Setup(context.Background(), "1.26.2", false); e == nil {
					t.Fatal("expected failure")
				}
				if !complete(old, Version{26, 1}, osName) {
					t.Fatal("existing installation damaged")
				}
				entries, e := os.ReadDir(filepath.Join(c.InstallDir, "1.26.2"))
				if e != nil {
					t.Fatal(e)
				}
				if len(entries) != 0 {
					t.Fatalf("partial installation remains: %v", entries)
				}
			})
		}
	}
}
func TestIncompleteInstallationIsNotOverwritten(t *testing.T) {
	c := testConfig(t)
	root := seed(t, c, "1.26.2", false, false)
	body := archiveFixture(t, c.OS, "1.26.2")
	serve(t, &c, body, "1.26.2", false, false, false)
	if _, e := c.Setup(context.Background(), "1.26.2", false); e == nil || !strings.Contains(e.Error(), "incomplete installation") {
		t.Fatalf("%v", e)
	}
	b, e := os.ReadFile(filepath.Join(root, "bin", "go"))
	if e != nil || string(b) != "fixture" {
		t.Fatal("overwrote incomplete installation")
	}
}
func TestArchivePathsAndLinks(t *testing.T) {
	for _, name := range []string{"../outside", "go/../../outside", "/go/bin/go", "go/a/../b", "go\\bin\\go", "go/C:/evil", "go/a:stream", "go/trailing.", "go/trailing "} {
		if _, e := archivePath(t.TempDir(), name); e == nil {
			t.Errorf("accepted %q", name)
		}
	}
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "go/escape", Typeflag: tar.TypeSymlink, Linkname: "../../outside"})
	tw.Close()
	gz.Close()
	if _, e := untar(t.TempDir(), &b); e == nil {
		t.Fatal("accepted symlink")
	}
	root := t.TempDir()
	if _, e := writeEntry(root, "go/bin/go", 0755, strings.NewReader("one")); e != nil {
		t.Fatal(e)
	}
	if _, e := writeEntry(root, "go/bin/go", 0755, strings.NewReader("two")); e == nil {
		t.Fatal("accepted duplicate")
	}
}
func TestHTTPFailureAndCancellation(t *testing.T) {
	c := testConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) }))
	defer server.Close()
	c.MetadataURL = server.URL + "?mode=json"
	if _, e := c.Setup(context.Background(), "1.26.0", false); e == nil {
		t.Fatal("accepted HTTP failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Setup(ctx, "1.26.0", false); e == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestToolchainGETRecoversBeforeReadingBody(t *testing.T) {
	c := testConfig(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "verified-later")
	}))
	defer server.Close()
	resp, err := c.get(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if string(data) != "verified-later" || calls != 2 {
		t.Fatalf("GET not recovered: %s calls=%d", data, calls)
	}
}

func TestHTTPErrorTextHidesRequestURLAndRetainsCancellation(t *testing.T) {
	c := testConfig(t)
	c.MetadataURL = "http://127.0.0.1:1/private?sig=private-signature"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Setup(ctx, "1.26.0", false)
	if err == nil || !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "private-signature") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("unsafe HTTP error or lost cancellation: %v", err)
	}
}
