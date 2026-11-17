package installer

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func extractionFixture(t *testing.T, zipFormat bool, trailing bool) []byte {
	t.Helper()
	var b bytes.Buffer
	names := []string{"go/VERSION", "go/bin/go", "go/empty"}
	bodies := []string{"go1.26.2\n", "compiler", ""}
	if zipFormat {
		z := zip.NewWriter(&b)
		for i, name := range names {
			w, err := z.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(w, bodies[i]); err != nil {
				t.Fatal(err)
			}
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&b)
		tr := tar.NewWriter(gz)
		for i, name := range names {
			if err := tr.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(bodies[i]))}); err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(tr, bodies[i]); err != nil {
				t.Fatal(err)
			}
		}
		if err := tr.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		if trailing {
			gz = gzip.NewWriter(&b)
			if _, err := gz.Write(make([]byte, 4096)); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	return b.Bytes()
}

// Trigger cancellation from an observable extraction milestone, with no timing
// sleeps or production test hooks. All compressed bytes have already been
// downloaded and authenticated when ZIP extraction writes the first file.
type extractionContext struct {
	context.Context
	cancel    context.CancelFunc
	installed string
	triggered atomic.Bool
}

func (c *extractionContext) Err() error {
	parent := filepath.Join(c.installed, "1.26.2")
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".staging-") {
			continue
		}
		if _, err := os.Stat(filepath.Join(parent, entry.Name(), "go", "VERSION")); err == nil {
			c.triggered.Store(true)
			c.cancel()
		}
	}
	return c.Context.Err()
}

func TestExtractionRejectionNeverPublishes(t *testing.T) {
	for _, zipFormat := range []bool{false, true} {
		for _, kind := range []string{"entries", "bytes", "cancel"} {
			t.Run(stringFormat(zipFormat)+"/"+kind, func(t *testing.T) {
				c := testConfig(t)
				old := seed(t, c, "1.26.1", false, true)
				body := extractionFixture(t, zipFormat, false)
				sum := sha256.Sum256(body)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
				defer server.Close()
				c.Client, c.DownloadURL = server.Client(), server.URL+"/"
				name := "fixture.tar.gz"
				if zipFormat {
					name = "fixture.zip"
				}
				a := archive{Filename: name, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
				limits := toolchainLimits
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				watch := &extractionContext{Context: ctx, cancel: cancel, installed: c.InstallDir}
				var extractCtx context.Context = ctx
				want := "entry limit"
				switch kind {
				case "entries":
					limits.entries = 2
				case "bytes":
					limits.bytes = 4
					want = "size limit"
				case "cancel":
					extractCtx = watch
				}
				var m Metrics
				_, err := c.installWithLimits(extractCtx, Version{26, 2}, a, &m, limits)
				if kind == "cancel" {
					if !watch.triggered.Load() || !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation not observed during extraction: %v", err)
					}
					if zipFormat && m.ArchiveBytes != int64(len(body)) {
						t.Fatal("ZIP cancellation preceded full download")
					}
				} else if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("want %s, got %v", want, err)
				}
				if !complete(old, Version{26, 1}, c.OS) {
					t.Fatal("existing toolchain changed")
				}
				entries, err := os.ReadDir(filepath.Join(c.InstallDir, "1.26.2"))
				if err != nil || len(entries) != 0 {
					t.Fatalf("partial publication or staging leak: %v %v", entries, err)
				}
			})
		}
	}
}

func stringFormat(zipFormat bool) string {
	if zipFormat {
		return "zip"
	}
	return "tar"
}

func TestTarExpansionIncludesTrailingMembers(t *testing.T) {
	data := extractionFixture(t, false, true)
	_, err := untar(context.Background(), t.TempDir(), bytes.NewReader(data), extractionLimits{bytes: 4096, entries: 10})
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("unbounded trailing expansion: %v", err)
	}
}

func TestExpansionReaderExactLimit(t *testing.T) {
	for _, data := range []string{"abc", "abcd"} {
		out, err := io.ReadAll(&expansionReader{r: strings.NewReader(data), left: 3})
		if data == "abc" && (err != nil || string(out) != data) {
			t.Fatalf("exact limit: %q %v", out, err)
		}
		if data == "abcd" && err == nil {
			t.Fatal("accepted expansion beyond limit")
		}
	}
}

type cancellingEntryReader struct{ cancel context.CancelFunc }

func (r cancellingEntryReader) Read(p []byte) (int, error) {
	r.cancel()
	p[0] = 'x'
	return 1, nil
}

func TestWriteEntryCancellationDuringCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n, err := writeEntry(ctx, t.TempDir(), "go/large", 0644, cancellingEntryReader{cancel: cancel})
	if n != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("file copy ignored cancellation: bytes=%d err=%v", n, err)
	}
}
