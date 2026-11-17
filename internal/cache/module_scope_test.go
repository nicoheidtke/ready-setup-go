package cache

import (
	"context"
	"path/filepath"
	"testing"
)

func TestModuleScopeSharesOnlyModules(t *testing.T) {
	for _, storage := range []string{"archive", "chunked"} {
		t.Run(storage, func(t *testing.T) {
			t.Setenv("GITHUB_REPOSITORY", "owner/repo")
			t.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/test.yml@refs/heads/main")
			t.Setenv("GITHUB_JOB", "test")
			c := DefaultConfig()
			c.Mode, c.Strict, c.Storage = "restore-save", true, storage
			c.ModuleScope, c.Scope = "trusted-modules", "race=false"
			seedRoots := roots(t)
			put(t, filepath.Join(seedRoots[0].Path, "module"), "module source")
			put(t, filepath.Join(seedRoots[1].Path, "object"), "build object")
			seed, err := NewState(c, "1.26.8", "deps", seedRoots)
			if err != nil {
				t.Fatal(err)
			}
			b := &memoryBackend{}
			if _, err := Save(context.Background(), b, seed); err != nil {
				t.Fatal(err)
			}

			t.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/lint.yml@refs/heads/main")
			t.Setenv("GITHUB_JOB", "lint")
			c.Scope = "race=true"
			next, err := NewState(c, "1.27.1", "deps", roots(t))
			if err != nil {
				t.Fatal(err)
			}
			if next.Snapshots[0].CompatiblePrefix != seed.Snapshots[0].CompatiblePrefix || next.Snapshots[1].CompatiblePrefix == seed.Snapshots[1].CompatiblePrefix {
				t.Fatal("module sharing changed build isolation or failed to share modules")
			}
			r, err := Restore(context.Background(), b, &next)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Snapshots[0].Hit || r.Snapshots[1].Hit {
				t.Fatalf("unexpected shared restore: %+v", r)
			}
		})
	}
}

func TestModuleScopeNamespaceBoundaries(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	c := DefaultConfig()
	original := state(t, c, "deps", roots(t))
	c.ModuleScope = "trusted-modules"
	shared := state(t, c, "deps", roots(t))
	if shared.Snapshots[0].CompatiblePrefix == original.Snapshots[0].CompatiblePrefix {
		t.Fatal("opt-in reused default namespace")
	}
	if shared.Snapshots[1].CompatiblePrefix != original.Snapshots[1].CompatiblePrefix || shared.Snapshots[1].Version != original.Snapshots[1].Version {
		t.Fatal("opt-in changed build identity")
	}
	for _, change := range []string{"scope", "repository", "compression", "storage"} {
		t.Run(change, func(t *testing.T) {
			changed := c
			switch change {
			case "scope":
				changed.ModuleScope = "other-trust-group"
			case "repository":
				t.Setenv("GITHUB_REPOSITORY", "other/repo")
			case "compression":
				changed.Compression = "none"
			case "storage":
				changed.Storage = "chunked"
			}
			next := state(t, changed, "deps", roots(t))
			if next.Snapshots[0].CompatiblePrefix == shared.Snapshots[0].CompatiblePrefix {
				t.Fatal("shared namespace boundary lost")
			}
		})
	}
	next := state(t, c, "new-deps", roots(t))
	if next.Snapshots[0].Prefix == shared.Snapshots[0].Prefix || next.Snapshots[0].CompatiblePrefix != shared.Snapshots[0].CompatiblePrefix {
		t.Fatal("dependency fallback changed")
	}
}
