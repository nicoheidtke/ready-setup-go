package cache

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDownloadConfigurationBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		workers int
		bytes   int64
		valid   bool
	}{
		{"minimum", 1, 1, true},
		{"maximum", 32, 4000 << 20, true},
		{"disabled workers", 0, 32 << 20, false},
		{"excess workers", 33, 32 << 20, false},
		{"empty block", 4, 0, false},
		{"excess block", 4, (4000 << 20) + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			c.DownloadConcurrency, c.DownloadChunkBytes = tc.workers, tc.bytes
			if err := c.Validate(); (err == nil) != tc.valid {
				t.Fatalf("configuration validation: %v", err)
			}
		})
	}
}

func TestArchiveWorkerConfigurationBounds(t *testing.T) {
	for _, field := range []string{"compression", "restore"} {
		for _, workers := range []int{0, 1, 4, 16, 17} {
			c := DefaultConfig()
			if field == "compression" {
				c.CompressionConcurrency = workers
			} else {
				c.RestoreConcurrency = workers
			}
			valid := workers >= 1 && workers <= 16
			if err := c.Validate(); (err == nil) != valid {
				t.Errorf("%s workers=%d: %v", field, workers, err)
			}
		}
	}
}

func TestDependenciesMonorepoAndIgnoredOutputs(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	put(t, filepath.Join(root, "go.work"), "go 1.26\nuse ./nested\n")
	put(t, filepath.Join(root, "nested", "go.mod"), "module example.org/nested\n")
	put(t, filepath.Join(root, "nested", "go.sum"), "checksum")
	before, err := Dependencies(ctx, root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".git", ".cache", "vendor", "node_modules", "dist", "bin"} {
		put(t, filepath.Join(root, name, "nested", "go.mod"), "ignored")
	}
	after, err := Dependencies(ctx, root, "")
	if err != nil || before != after {
		t.Fatal("output/vendor manifests affected default fingerprint", err)
	}
	put(t, filepath.Join(root, "nested", "go.sum"), "new checksum")
	after, err = Dependencies(ctx, root, "")
	if err != nil || before == after {
		t.Fatal("nested dependency change not detected")
	}
	explicit, err := Dependencies(ctx, root, "nested/go.mod\r\nnested/go.sum")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, "go.work"), "changed workspace")
	unchanged, err := Dependencies(ctx, root, "nested/go.mod\nnested/go.sum")
	if err != nil || explicit != unchanged {
		t.Fatal("explicit file scope ignored")
	}
	if _, err = Dependencies(ctx, root, "../outside"); err == nil {
		t.Fatal("escaped workspace")
	}
	if _, err = Dependencies(ctx, root, "missing"); err == nil {
		t.Fatal("missing explicit path accepted")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = Dependencies(ctx, root, ""); err == nil {
		t.Fatal("cancelled discovery continued")
	}
}

func TestGoRootsUseSelectedGoAndUserPaths(t *testing.T) {
	root := runtime.GOROOT()
	if _, err := os.Stat(filepath.Join(root, "bin", "go"+map[bool]string{true: ".exe"}[runtime.GOOS == "windows"])); err != nil {
		t.Skip("running Go compiler root unavailable")
	}
	module, build := filepath.Join(t.TempDir(), "module cache"), filepath.Join(t.TempDir(), "build cache")
	t.Setenv("GOMODCACHE", module)
	t.Setenv("GOCACHE", build)
	t.Setenv("GOROOT", "intentionally-invalid")
	t.Setenv("GOTOOLCHAIN", "intentionally-invalid")
	t.Setenv("GOTELEMETRY", "off")
	rs, err := GoRoots(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if rs[0].Path != module || rs[1].Path != build {
		t.Fatal(rs)
	}
	t.Setenv("GOCACHE", "off")
	if _, err = GoRoots(context.Background(), root); err == nil {
		t.Fatal("GOCACHE=off accepted")
	}
}
