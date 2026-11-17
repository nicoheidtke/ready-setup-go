package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDependencyBudgetsPreserveFingerprint(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "go.mod"), "module example.org/root\n")
	put(t, filepath.Join(root, "nested", "go.sum"), "checksum")
	// The original name/content/NUL framing and sorted order must not change.
	h := sha256.Sum256([]byte("go.mod\x00module example.org/root\n\x00nested/go.sum\x00checksum\x00"))
	want := hex.EncodeToString(h[:])[:24]
	limits := dependencyLimits{2, len("go.modnested/go.sum"), 32, 128}
	for _, selection := range []string{"", "nested/go.sum\ngo.mod", ".\ngo.mod"} {
		got, err := dependencies(context.Background(), root, selection, limits)
		if err != nil || got != want {
			t.Fatalf("selection %q: got %s, %v; want %s", selection, got, err, want)
		}
	}
}

func TestDependencySelectionAndMetadataLimits(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "go.mod"), "1234")
	put(t, filepath.Join(root, "nested", "go.sum"), "5678")
	for _, tc := range []struct {
		name, selection string
		limits          dependencyLimits
		message         string
	}{
		{"selection bytes", "go.mod", dependencyLimits{10, 100, 100, 5}, "selection exceeds size"},
		{"selection count", "go.mod\ngo.mod", dependencyLimits{1, 100, 100, 100}, "selection exceeds path count"},
		{"discovered count", "", dependencyLimits{1, 100, 100, 100}, "metadata exceeds"},
		{"explicit paths", "go.mod\nnested/go.sum", dependencyLimits{10, 17, 100, 100}, "metadata exceeds"},
		{"discovered paths", "", dependencyLimits{10, 17, 100, 100}, "metadata exceeds"},
		{"explicit aggregate bytes", "go.mod\nnested/go.sum", dependencyLimits{10, 100, 7, 100}, "contents exceed"},
		{"discovered aggregate bytes", "", dependencyLimits{10, 100, 7, 100}, "contents exceed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dependencies(context.Background(), root, tc.selection, tc.limits)
			if got != "" || err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestDependencyOversizedFileRejectedBeforeHashing(t *testing.T) {
	root := t.TempDir()
	f, err := os.Create(filepath.Join(root, "large"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1024); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := dependencies(context.Background(), root, "large", dependencyLimits{10, 100, 64, 100})
	if got != "" || err == nil || !strings.Contains(err.Error(), "contents exceed") {
		t.Fatalf("got %q, %v", got, err)
	}
}

type cancellingDependencyReader struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancellingDependencyReader) Read(p []byte) (int, error) {
	r.reads++
	p[0] = 'x'
	if r.reads == 2 {
		r.cancel()
	}
	return 1, nil
}

func TestDependencyHashReadCancellationAndGrowth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancellingDependencyReader{cancel: cancel}
	n, err := copyDependency(ctx, io.Discard, r, 100)
	if !errors.Is(err, context.Canceled) || n != 2 || r.reads != 2 {
		t.Fatalf("read cancellation: bytes=%d reads=%d err=%v", n, r.reads, err)
	}
	// Actual reads enforce the aggregate ceiling even if a file grows after stat.
	var out bytes.Buffer
	n, err = copyDependency(context.Background(), &out, strings.NewReader("123456789"), 4)
	if n != 5 || out.Len() != 5 || err == nil || !strings.Contains(err.Error(), "contents exceed") {
		t.Fatalf("growing file: bytes=%d retained=%d err=%v", n, out.Len(), err)
	}
	for _, contents := range []string{"", "1234"} {
		n, err := copyDependency(context.Background(), io.Discard, strings.NewReader(contents), 4)
		if err != nil || n != int64(len(contents)) {
			t.Fatalf("exact limit %q: bytes=%d err=%v", contents, n, err)
		}
	}
}

func TestDependencyCancellationIncludesExplicitAndEmptySelections(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "go.mod"), "module example.org/root")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, selection := range []string{"", "go.mod"} {
		got, err := Dependencies(ctx, root, selection)
		if got != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("selection %q: got %s, %v", selection, got, err)
		}
	}
}
