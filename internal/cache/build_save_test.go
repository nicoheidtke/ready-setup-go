package cache

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildSaveDisabledRetainsModuleSaveAndBuildRestore(t *testing.T) {
	for _, storage := range []string{"archive", "chunked"} {
		t.Run(storage, func(t *testing.T) {
			ctx := context.Background()
			c := DefaultConfig()
			c.Mode, c.Strict, c.Storage = "restore-save", true, storage
			c.SavePolicy = "always"
			seedRoots := roots(t)
			put(t, filepath.Join(seedRoots[0].Path, "module"), "original module")
			put(t, filepath.Join(seedRoots[1].Path, "object"), "original build")
			b := &memoryBackend{}
			if _, err := Save(ctx, b, state(t, c, "deps", seedRoots)); err != nil {
				t.Fatal(err)
			}
			c.DisableBuildSave = true
			next := state(t, c, "deps", roots(t))
			restored, err := Restore(ctx, b, &next)
			if err != nil {
				t.Fatal(err)
			}
			if !restored.Snapshots[0].Hit || !restored.Snapshots[1].Hit {
				t.Fatalf("save policy disabled restore: %+v", restored)
			}
			put(t, filepath.Join(next.Snapshots[0].Roots[0].Path, "module"), "changed module")
			put(t, filepath.Join(next.Snapshots[1].Roots[0].Path, "object"), "changed build")
			r, err := Save(ctx, b, next)
			if err != nil {
				t.Fatal(err)
			}
			module, build := r.Snapshots[0], r.Snapshots[1]
			if module.Skipped || module.Bytes == 0 {
				t.Fatalf("module save skipped: %+v", module)
			}
			if !build.Skipped || build.SaveReason != "build-save-disabled" || build.Bytes != 0 || build.ArchiveMS != 0 {
				t.Fatalf("build save was not skipped: %+v", build)
			}
			for _, e := range b.entries {
				if e.key == next.Snapshots[1].Key {
					t.Fatal("disabled build save published a snapshot")
				}
			}
		})
	}
}

func TestBuildSaveDisabledDoesNotPackOrAccessBuildRoot(t *testing.T) {
	c := DefaultConfig()
	c.Mode, c.Strict, c.DisableBuildSave = "restore-save", true, true
	s := state(t, c, "deps", roots(t))
	s.Snapshots = s.Snapshots[1:]
	// A regular file cannot be walked as a cache root. An unavailable temporary
	// directory also proves this skips before creating even an archive file.
	invalid := filepath.Join(t.TempDir(), "not-a-directory")
	put(t, invalid, "keep")
	s.Snapshots[0].Roots[0].Path = invalid
	for _, env := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(env, filepath.Join(t.TempDir(), "absent"))
	}
	b := &memoryBackend{}
	r, err := Save(context.Background(), b, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Snapshots) != 1 || !r.Snapshots[0].Skipped || r.Snapshots[0].SaveReason != "build-save-disabled" || b.lookups != 0 || b.uploads != 0 {
		t.Fatalf("disabled build touched save pipeline: %+v", r)
	}
}

func TestBuildSavePostStateCompatibility(t *testing.T) {
	t.Setenv("RUNNER_TEMP", t.TempDir())
	c := DefaultConfig()
	if c.DisableBuildSave {
		t.Fatal("default build-save behavior changed")
	}
	c.DisableBuildSave = true
	original := state(t, c, "deps", roots(t))
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
	if !restored.Config.DisableBuildSave {
		t.Fatal("post state lost build-save policy")
	}
	// Older states omitted this field and must still save builds by default.
	var old Config
	if err := json.Unmarshal([]byte(`{"mode":"restore-save"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.DisableBuildSave {
		t.Fatal("old post state disabled build saves")
	}
}
