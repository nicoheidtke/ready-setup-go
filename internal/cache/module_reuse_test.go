package cache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestModuleContentReuseIgnoresTimestampsWithoutPacking(t *testing.T) {
	for _, storage := range []string{"archive", "chunked"} {
		t.Run(storage, func(t *testing.T) {
			ctx := context.Background()
			c := DefaultConfig()
			c.Mode, c.Strict, c.Storage = "restore-save", true, storage
			c.SavePolicy = "always"
			seedRoots := roots(t)
			put(t, filepath.Join(seedRoots[0].Path, "module"), "unchanged")
			seed := state(t, c, "deps", seedRoots)
			b := &memoryBackend{}
			if _, err := Save(ctx, b, seed); err != nil {
				t.Fatal(err)
			}
			next := state(t, c, "deps", roots(t))
			if _, err := Restore(ctx, b, &next); err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(next.Snapshots[0].Roots[0].Path, "module")
			stamp := time.Unix(1740000000, 123456700)
			if err := os.Chtimes(name, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			uploads, lookups := b.uploads, b.lookups
			// An unchanged module should not create even a temporary save archive.
			// Select just the module snapshot so empty build packing cannot mask this.
			next.Snapshots = next.Snapshots[:1]
			t.Setenv("TMP", filepath.Join(t.TempDir(), "absent"))
			t.Setenv("TEMP", os.Getenv("TMP"))
			t.Setenv("TMPDIR", os.Getenv("TMP"))
			r, err := Save(ctx, b, next)
			if err != nil {
				t.Fatal(err)
			}
			m := r.Snapshots[0]
			if !m.Skipped || m.SaveReason != "unchanged-module-content" || m.Files != 1 || m.ModuleCheckMS < 0 || b.uploads != uploads || b.lookups != lookups {
				t.Fatalf("bad module reuse: %+v uploads=%d lookups=%d", m, b.uploads, b.lookups)
			}
		})
	}
}

func TestModuleContentReuseDetectsSameSizeAndLocalPriority(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "mutation", true: "existing-local-file"}[local], func(t *testing.T) {
			ctx := context.Background()
			c := DefaultConfig()
			c.Mode, c.Strict = "restore-save", true
			c.SavePolicy = "always"
			seedRoots := roots(t)
			put(t, filepath.Join(seedRoots[0].Path, "module"), "original")
			seed := state(t, c, "deps", seedRoots)
			b := &memoryBackend{}
			if _, err := Save(ctx, b, seed); err != nil {
				t.Fatal(err)
			}
			nextRoots := roots(t)
			name := filepath.Join(nextRoots[0].Path, "module")
			if local {
				put(t, name, "mutated!")
			}
			next := state(t, c, "deps", nextRoots)
			if _, err := Restore(ctx, b, &next); err != nil {
				t.Fatal(err)
			}
			stamp, err := os.Stat(name)
			if err != nil {
				t.Fatal(err)
			}
			if !local {
				put(t, name, "mutated!")
				if err := os.Chtimes(name, stamp.ModTime(), stamp.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			r, err := Save(ctx, b, next)
			if err != nil {
				t.Fatal(err)
			}
			if r.Snapshots[0].Skipped || r.Snapshots[0].SaveReason != "module-content-or-mode-changed" || r.Snapshots[0].Bytes == 0 {
				t.Fatalf("changed modules skipped: %+v", r)
			}
		})
	}
}
