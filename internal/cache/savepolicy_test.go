package cache

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setBranchContext(t *testing.T, ref string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	put(t, path, `{"repository":{"full_name":"owner/repo","default_branch":"trunk"}}`)
	t.Setenv("GITHUB_EVENT_PATH", path)
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_REF", ref)
}

func TestSaveBranchPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, ref, event, policy string
		allowed                  bool
	}{
		{"default", "refs/heads/trunk", `{"repository":{"full_name":"owner/repo","default_branch":"trunk"}}`, "default", true},
		{"feature", "refs/heads/feature", `{"repository":{"full_name":"owner/repo","default_branch":"trunk"}}`, "default", false},
		{"pr", "refs/pull/1/merge", "", "default", false},
		{"tag", "refs/tags/trunk", "", "default", false},
		{"unknown", "", "", "default", false},
		{"missing", "refs/heads/trunk", "", "default", false},
		{"invalid", "refs/heads/trunk", "private-invalid-event", "default", false},
		{"null", "refs/heads/trunk", "null", "default", false},
		{"trailing", "refs/heads/trunk", `{"repository":{"full_name":"owner/repo","default_branch":"trunk"}} {}`, "default", false},
		{"wrong-repository", "refs/heads/trunk", `{"repository":{"full_name":"other/repo","default_branch":"trunk"}}`, "default", false},
		{"no-branch", "refs/heads/trunk", `{"repository":{"full_name":"owner/repo"}}`, "default", false},
		{"oversized", "refs/heads/trunk", strings.Repeat(" ", 1<<20) + "{}", "default", false},
		{"all", "refs/heads/feature", "", "all", true},
		{"legacy", "refs/heads/feature", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setBranchContext(t, tc.ref)
			if tc.event == "" {
				t.Setenv("GITHUB_EVENT_PATH", filepath.Join(t.TempDir(), "missing"))
			} else {
				put(t, os.Getenv("GITHUB_EVENT_PATH"), tc.event)
			}
			c := DefaultConfig()
			c.SaveBranches, c.Strict = tc.policy, true
			reason := saveBranchSkip(c)
			if (reason == "") != tc.allowed || strings.Contains(reason, "private-invalid-event") {
				t.Fatalf("allowed=%v reason=%q", tc.allowed, reason)
			}
		})
	}
}

func TestLegacySavePolicyState(t *testing.T) {
	c := DefaultConfig()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "save_policy")
	delete(fields, "save_branches")
	data, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var old Config
	if err := json.Unmarshal(data, &old); err != nil || old.Validate() != nil {
		t.Fatalf("legacy state rejected: %v", err)
	}
	t.Setenv("GITHUB_REF", "refs/pull/1/merge")
	t.Setenv("GITHUB_EVENT_PATH", "")
	if reason := saveBranchSkip(old); reason != "" {
		t.Fatal("legacy state changed writer policy", reason)
	}
	rs := roots(t)
	put(t, filepath.Join(rs[1].Path, "object"), "compiled")
	s := state(t, old, "deps", rs)
	s.Snapshots = s.Snapshots[1:]
	s.Snapshots[0].DependencyMatch = true
	b := &memoryBackend{}
	if r, err := Save(context.Background(), b, s); err != nil || b.uploads != 1 {
		t.Fatalf("legacy state changed refresh policy: %+v %v", r, err)
	}
}

func TestSavePolicyValidation(t *testing.T) {
	for _, policy := range []string{"", "on-miss", "always"} {
		for _, branches := range []string{"", "default", "all"} {
			c := DefaultConfig()
			c.SavePolicy, c.SaveBranches = policy, branches
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, input := range []string{"policy", "branches"} {
		c := DefaultConfig()
		if input == "policy" {
			c.SavePolicy = "invalid"
		} else {
			c.SaveBranches = "invalid"
		}
		if c.Validate() == nil {
			t.Fatal("invalid save configuration accepted", input)
		}
	}
}

func TestBaselineColdExactAndCompatibleLifecycle(t *testing.T) {
	for _, storage := range []string{"archive", "chunked"} {
		for _, layout := range []string{"split", "combined"} {
			t.Run(storage+"/"+layout, func(t *testing.T) {
				c := DefaultConfig()
				c.Mode, c.Storage, c.Layout, c.Compression, c.Strict = "restore-save", storage, layout, "none", true
				ctx, b := context.Background(), &memoryBackend{}
				rs := roots(t)
				put(t, filepath.Join(rs[0].Path, "module"), "module source")
				put(t, filepath.Join(rs[1].Path, "object"), "compiled result")
				seed := state(t, c, "deps-a", rs)
				if _, err := Save(ctx, b, seed); err != nil {
					t.Fatal(err)
				}
				uploads := b.uploads
				warm := state(t, c, "deps-a", roots(t))
				r, err := Restore(ctx, b, &warm)
				if err != nil {
					t.Fatal(err)
				}
				for i, snap := range warm.Snapshots {
					if !r.Snapshots[i].Hit || !snap.DependencyMatch || snap.RestoredDigest != "" {
						t.Fatalf("baseline restore: %+v %+v", r.Snapshots[i], snap)
					}
					// The skip must precede all source access, even with unusable roots.
					for j := range warm.Snapshots[i].Roots {
						invalid := filepath.Join(t.TempDir(), "not-a-directory")
						put(t, invalid, "keep")
						warm.Snapshots[i].Roots[j].Path = invalid
					}
				}
				r, err = Save(ctx, b, warm)
				if err != nil || b.uploads != uploads {
					t.Fatalf("warm baseline uploaded: %+v %v", r, err)
				}
				for _, m := range r.Snapshots {
					if !m.Skipped || m.SaveReason != "dependency-match" || m.ArchiveMS != 0 || m.ModuleCheckMS != 0 {
						t.Fatalf("baseline did save work: %+v", m)
					}
				}
				changed := state(t, c, "deps-b", roots(t))
				r, err = Restore(ctx, b, &changed)
				if err != nil {
					t.Fatal(err)
				}
				for i, snap := range changed.Snapshots {
					if !r.Snapshots[i].Hit || r.Snapshots[i].MatchType != "dependency-compatible" || snap.DependencyMatch {
						t.Fatalf("fallback treated as exact: %+v", r)
					}
				}
				// Even unchanged fallback contents must seed the new dependency identity.
				r, err = Save(ctx, b, changed)
				if err != nil || b.uploads <= uploads {
					t.Fatalf("fallback did not seed: %+v %v", r, err)
				}
				repeat := state(t, c, "deps-b", roots(t))
				if _, err := Restore(ctx, b, &repeat); err != nil {
					t.Fatal(err)
				}
				for _, snap := range repeat.Snapshots {
					if !snap.DependencyMatch {
						t.Fatal("new dependency baseline unavailable")
					}
				}
			})
		}
	}
}

func TestBaselinePartialAndFailedRestore(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial", true: "invalid-archive"}[corrupt], func(t *testing.T) {
			c := DefaultConfig()
			c.Mode, c.Compression = "restore-save", "none"
			b, rs := &memoryBackend{}, roots(t)
			put(t, filepath.Join(rs[0].Path, "module"), "source")
			put(t, filepath.Join(rs[1].Path, "object"), "build")
			if _, err := Save(context.Background(), b, state(t, c, "deps", rs)); err != nil {
				t.Fatal(err)
			}
			b.entries = b.entries[:1] // Missing build must remain eligible independently.
			if corrupt {
				b.entries[0].data = []byte("invalid archive")
			}
			next := state(t, c, "deps", roots(t))
			if _, err := Restore(context.Background(), b, &next); err != nil {
				t.Fatal(err)
			}
			if next.Snapshots[0].DependencyMatch == corrupt || next.Snapshots[1].DependencyMatch {
				t.Fatalf("incorrect restore state: %+v", next.Snapshots)
			}
			put(t, filepath.Join(next.Snapshots[0].Roots[0].Path, "new"), "new source")
			put(t, filepath.Join(next.Snapshots[1].Roots[0].Path, "new"), "new build")
			r, err := Save(context.Background(), b, next)
			if err != nil || r.Snapshots[1].Skipped || r.Snapshots[0].Skipped == corrupt {
				t.Fatalf("bad independent save: %+v %v", r, err)
			}
		})
	}
}

func TestBranchPolicyRegistrationAndPostRecheck(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		for _, ref := range []string{"refs/heads/trunk", "refs/heads/feature", "refs/pull/1/merge"} {
			t.Run(ref+map[bool]string{false: "/sequential", true: "/prefetch"}[overlap], func(t *testing.T) {
				setBranchContext(t, ref)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if filepath.Base(r.URL.Path) != "GetCacheEntryDownloadURL" {
						t.Error("unexpected write", r.URL.Path)
					}
					io.WriteString(w, `{"ok":false}`)
				}))
				defer server.Close()
				workspace := t.TempDir()
				put(t, filepath.Join(workspace, "go.mod"), "module example.org/policy\ngo 1.26.0\n")
				t.Setenv("GITHUB_WORKSPACE", workspace)
				t.Setenv("RUNNER_TEMP", t.TempDir())
				t.Setenv("GOCACHE", filepath.Join(workspace, "build"))
				t.Setenv("GOMODCACHE", filepath.Join(workspace, "modules"))
				t.Setenv("ACTIONS_RESULTS_URL", server.URL)
				t.Setenv("ACTIONS_RUNTIME_TOKEN", "policy-test-token")
				t.Setenv("ACTIONS_CACHE_MODE", "")
				for _, name := range []string{"GITHUB_STATE", "GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY"} {
					path := filepath.Join(workspace, name)
					put(t, path, "")
					t.Setenv(name, path)
				}
				c := DefaultConfig()
				c.Mode, c.Overlap, c.Strict, c.Report = "restore-save", overlap, true, filepath.Join(workspace, "report.json")
				p := BeginPrefetch(context.Background(), c, "1.26.8")
				defer p.Close()
				if err := StartPrefetched(context.Background(), c, "1.26.8", runtime.GOROOT(), p); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(os.Getenv("GITHUB_STATE"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "cache_state=") != (ref == "refs/heads/trunk") {
					t.Fatalf("incorrect registration: %s", data)
				}
				if ref != "refs/heads/trunk" {
					return
				}
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					key, value, _ := strings.Cut(line, "=")
					t.Setenv("STATE_"+key, value)
				}
				// Post must deny branch changes before asking for credentials.
				t.Setenv("GITHUB_REF", "refs/heads/feature")
				t.Setenv("ACTIONS_RUNTIME_TOKEN", "")
				if err := Post(context.Background()); err != nil {
					t.Fatal(err)
				}
				var report Report
				data, err = os.ReadFile(c.Report + ".post.json")
				if err != nil || json.Unmarshal(data, &report) != nil || report.SkippedReason == "" {
					t.Fatalf("post policy report missing: %s %v", data, err)
				}
			})
		}
	}
}

func TestBaselinePostSkipsWithoutBackendAndRetainsState(t *testing.T) {
	setBranchContext(t, "refs/heads/trunk")
	t.Setenv("RUNNER_TEMP", t.TempDir())
	t.Setenv("ACTIONS_CACHE_MODE", "")
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "")
	c := DefaultConfig()
	c.Mode, c.Strict, c.Report = "restore-save", true, filepath.Join(t.TempDir(), "report.json")
	s := state(t, c, "deps", roots(t))
	for i := range s.Snapshots {
		s.Snapshots[i].DependencyMatch = true
	}
	path, err := persist(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("STATE_cache_state", path)
	t.Setenv("STATE_cache_strict", "true")
	if err := Post(context.Background()); err != nil {
		t.Fatal(err)
	}
	var report Report
	data, err := os.ReadFile(c.Report + ".post.json")
	if err != nil || json.Unmarshal(data, &report) != nil || len(report.Snapshots) != 2 {
		t.Fatalf("post lost baseline outcomes: %s %v", data, err)
	}
	for _, m := range report.Snapshots {
		if !m.Skipped || m.SaveReason != "dependency-match" {
			t.Fatalf("post lost baseline skip: %+v", m)
		}
	}
}
