package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
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

func TestCompressionConfiguration(t *testing.T) {
	if got := DefaultConfig().Compression; got != "gzip" {
		t.Fatalf("existing compression default changed: %s", got)
	}
	for _, compression := range []string{"gzip", "none", "zstd", "", "brotli", "ZSTD"} {
		c := DefaultConfig()
		c.Compression = compression
		valid := compression == "gzip" || compression == "none" || compression == "zstd"
		if err := c.Validate(); (err == nil) != valid {
			t.Errorf("compression %q validation: %v", compression, err)
		}
	}
}

func TestCompressionCacheIdentityAndPostStateCompatibility(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/test.yml@refs/heads/main")
	t.Setenv("GITHUB_JOB", "test")
	t.Setenv("RUNNER_TEMP", t.TempDir())
	versions := map[string]string{
		"gzip": "d1b4aadf7643a77b6a041bfc32cc6e0b7ae83f568869eb4a6ba5462227389556",
		"none": "c093c62ca258493efca78a6f5b51c8807af673d9eb90edf46c5b33b496897ccb",
		"zstd": "365200cde3589f647585601adf8a0ca80944360edcb4e6e8fc00751db5945b35",
	}
	for _, storage := range []string{"archive", "chunked"} {
		for compression, version := range versions {
			t.Run(storage+"/"+compression, func(t *testing.T) {
				c := DefaultConfig()
				c.Mode, c.Storage, c.Compression = "restore-save", storage, compression
				original := state(t, c, "deps", roots(t))
				storagePrefix := ""
				if storage == "chunked" {
					storagePrefix = "chunks-"
				}
				wantPrefix := storagePrefix + fmt.Sprintf("sgf-cache-v1-%s-%s-go1.26.8-ae41d690fe78fcf733180b22-split-%s-modules-deps-", runtime.GOOS, runtime.GOARCH, compression)
				snapshot := original.Snapshots[0]
				if snapshot.Prefix != wantPrefix || !strings.HasPrefix(snapshot.Key, wantPrefix) || snapshot.Version != version {
					t.Fatalf("incompatible cache identity: %+v", snapshot)
				}
				path, err := persist(original)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var restored State
				if err := json.Unmarshal(data, &restored); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(restored, original) {
					t.Fatalf("post state changed config or identity: %+v", restored)
				}
			})
		}
	}
}

func TestGzipLevelConfigurationAndIdentity(t *testing.T) {
	for _, level := range []int{-2, -1, 0, 1, 3, 9, 10} {
		c := DefaultConfig()
		c.GzipLevel = level
		valid := level >= -1 && level <= 9
		if err := c.Validate(); (err == nil) != valid {
			t.Fatalf("level %d: %v", level, err)
		}
	}
	c := DefaultConfig()
	rs := roots(t)
	baseline := state(t, c, "deps", rs)
	for _, level := range []int{0, -1, 1, 9} {
		c.GzipLevel = level
		current := state(t, c, "deps", rs)
		for i := range current.Snapshots {
			if current.Snapshots[i].Prefix != baseline.Snapshots[i].Prefix || current.Snapshots[i].Version != baseline.Snapshots[i].Version {
				t.Fatal("gzip level changed cache namespace")
			}
		}
		data, err := json.Marshal(current)
		if err != nil {
			t.Fatal(err)
		}
		var roundtrip State
		if err := json.Unmarshal(data, &roundtrip); err != nil {
			t.Fatal(err)
		}
		if roundtrip.Config.GzipLevel != level {
			t.Fatal("gzip level lost in post state")
		}
	}
}

func TestBuildSaveConfigurationRequiresSplit(t *testing.T) {
	c := DefaultConfig()
	c.Layout = "combined"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.DisableBuildSave = true
	if err := c.Validate(); err == nil {
		t.Fatal("combined build-save gate accepted")
	}
}

func TestModuleScopeRequiresSplit(t *testing.T) {
	c := DefaultConfig()
	c.ModuleScope, c.Layout = "shared", "combined"
	if err := c.Validate(); err == nil {
		t.Fatal("combined module sharing accepted")
	}
	c.Layout = "split"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
