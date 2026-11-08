package cache

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"setup-go-better/internal/cachearchive"
	"setup-go-better/internal/cachebackend"
)

type memoryEntry struct {
	key, version string
	data         []byte
}
type memoryBackend struct {
	mu               sync.Mutex
	entries          []memoryEntry
	queries          [][]string
	lookups, uploads int
	err              error
}

type cancellingBackend struct {
	memoryBackend
	cancel context.CancelFunc
}

func (b *cancellingBackend) Lookup(ctx context.Context, key, version string, prefixes []string) (*cachebackend.Entry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lookups++
	b.cancel()
	return nil, ctx.Err()
}

func (b *cancellingBackend) Upload(ctx context.Context, key, version string, r io.ReadSeeker, size int64) error {
	b.uploads++
	b.cancel()
	return ctx.Err()
}

func (b *memoryBackend) Lookup(_ context.Context, key, version string, prefixes []string) (*cachebackend.Entry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lookups++
	b.queries = append(b.queries, append([]string{key}, prefixes...))
	if b.err != nil {
		return nil, b.err
	}
	for _, prefix := range append([]string{key}, prefixes...) {
		for i := len(b.entries) - 1; i >= 0; i-- {
			e := b.entries[i]
			if e.version == version && strings.HasPrefix(e.key, prefix) {
				return &cachebackend.Entry{Key: e.key, URL: fmt.Sprint(i)}, nil
			}
		}
	}
	return nil, nil
}
func (b *memoryBackend) Download(_ context.Context, e cachebackend.Entry, w io.Writer) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, entry := range b.entries {
		if entry.key == e.Key {
			n, err := w.Write(entry.data)
			return int64(n), err
		}
	}
	return 0, errors.New("missing test entry")
}
func (b *memoryBackend) Upload(_ context.Context, key, version string, r io.ReadSeeker, size int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return b.err
	}
	for _, e := range b.entries {
		if e.key == key {
			return cachebackend.ErrConflict
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return errors.New("bad upload size")
	}
	b.entries = append(b.entries, memoryEntry{key, version, data})
	b.uploads++
	return nil
}

func roots(t *testing.T) []cachearchive.Root {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return []cachearchive.Root{{Name: "modules", Path: filepath.Join(dir, "modules")}, {Name: "build", Path: filepath.Join(dir, "build")}}
}
func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func state(t *testing.T, c Config, dep string, rs []cachearchive.Root) State {
	t.Helper()
	s, err := NewState(c, "1.26.8", dep, rs)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSplitLifecycleFreshBuildAndUnchangedModules(t *testing.T) {
	for _, compression := range []string{"none", "gzip"} {
		t.Run(compression, func(t *testing.T) {
			ctx := context.Background()
			c := DefaultConfig()
			c.Mode = "restore-save"
			c.Strict = true
			c.Compression = compression
			seedRoots := roots(t)
			put(t, filepath.Join(seedRoots[0].Path, "example.org", "module@v1", "go.mod"), "module example.org/module\n")
			put(t, filepath.Join(seedRoots[1].Path, "object"), "build")
			b := &memoryBackend{}
			seed := state(t, c, "deps-a", seedRoots)
			if _, err := Save(ctx, b, seed); err != nil {
				t.Fatal(err)
			}
			if b.uploads != 2 {
				t.Fatal(b.uploads)
			}
			next := state(t, c, "deps-a", roots(t))
			if next.Snapshots[0].Key == seed.Snapshots[0].Key {
				t.Fatal("snapshot key reused")
			}
			r, err := Restore(ctx, b, &next)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range r.Snapshots {
				if !m.Hit || m.Bytes == 0 {
					t.Fatalf("bad restore: %+v", m)
				}
			}
			r, err = Save(ctx, b, next)
			if err != nil {
				t.Fatal(err)
			}
			if !r.Snapshots[0].Skipped || r.Snapshots[1].Skipped || b.uploads != 3 {
				t.Fatalf("unchanged modules/build refresh: %+v uploads=%d", r, b.uploads)
			}
			put(t, filepath.Join(next.Snapshots[0].Roots[0].Path, "new"), "new module entry")
			third := state(t, c, "deps-a", nextRoots(next))
			third.Snapshots[0].RestoredDigest = next.Snapshots[0].RestoredDigest
			if _, err = Save(ctx, b, third); err != nil {
				t.Fatal(err)
			}
			if b.uploads != 5 {
				t.Fatalf("changed modules not uploaded: %d", b.uploads)
			}
		})
	}
}
func nextRoots(s State) []cachearchive.Root {
	var rs []cachearchive.Root
	for _, snap := range s.Snapshots {
		rs = append(rs, snap.Roots...)
	}
	return rs
}

func TestDependencyChangeUsesCompatibleFallback(t *testing.T) {
	c := DefaultConfig()
	c.Mode = "restore-save"
	c.Strict = true
	b := &memoryBackend{}
	rs := roots(t)
	put(t, filepath.Join(rs[0].Path, "module"), "cached dependency")
	a := state(t, c, "old-deps", rs)
	if _, err := Save(context.Background(), b, a); err != nil {
		t.Fatal(err)
	}
	changed := state(t, c, "new-deps", roots(t))
	r, err := Restore(context.Background(), b, &changed)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Snapshots[0].Hit {
		t.Fatal("dependency change lost compatible snapshot")
	}
	q := b.queries[0]
	if len(q) != 3 || q[1] != changed.Snapshots[0].Prefix || q[2] != changed.Snapshots[0].CompatiblePrefix {
		t.Fatal(q)
	}
}

func TestCombinedLayoutAndCorruptRestore(t *testing.T) {
	c := DefaultConfig()
	c.Layout = "combined"
	c.Mode = "restore-save"
	c.Strict = true
	rs := roots(t)
	put(t, filepath.Join(rs[0].Path, "modules"), "module")
	put(t, filepath.Join(rs[1].Path, "build"), "build")
	s := state(t, c, "deps", rs)
	if len(s.Snapshots) != 1 {
		t.Fatal(s)
	}
	b := &memoryBackend{}
	if _, err := Save(context.Background(), b, s); err != nil {
		t.Fatal(err)
	}
	dst := roots(t)
	put(t, filepath.Join(dst[1].Path, "local"), "usable")
	next := state(t, c, "deps", dst)
	b.entries[0].data = []byte("invalid archive")
	r, err := Restore(context.Background(), b, &next)
	if err == nil || r.Snapshots[0].Hit {
		t.Fatal("corrupt snapshot accepted")
	}
	data, e := os.ReadFile(filepath.Join(dst[1].Path, "local"))
	if e != nil || string(data) != "usable" {
		t.Fatal("corrupt snapshot changed usable local cache")
	}
}

func TestErrorsModesAndCancellation(t *testing.T) {
	c := DefaultConfig()
	c.Mode = "restore-save"
	b := &memoryBackend{err: errors.New("offline")}
	s := state(t, c, "deps", roots(t))
	r, err := Restore(context.Background(), b, &s)
	if err != nil || r.Snapshots[0].Error == "" {
		t.Fatal("cache failure was not a warning")
	}
	s.Config.Strict = true
	if _, err = Restore(context.Background(), b, &s); err == nil {
		t.Fatal("strict error ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := b.lookups
	if _, err = Restore(ctx, b, &s); !errors.Is(err, context.Canceled) || b.lookups != before {
		t.Fatal("cancelled restore touched backend")
	}
	if _, err = Save(ctx, b, s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_OUTPUT", filepath.Join(t.TempDir(), "nonexistent-output"))
	t.Setenv("GITHUB_STATE", filepath.Join(t.TempDir(), "nonexistent-state"))
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "")
	c = DefaultConfig()
	if err = Start(context.Background(), c, "1.26.8", "nonexistent-go"); err != nil {
		t.Fatal("cache off did work", err)
	}
}

func TestNamespaceAndRunIsolation(t *testing.T) {
	c := DefaultConfig()
	rs := roots(t)
	t.Setenv("GITHUB_REPOSITORY", "owner/repo")
	t.Setenv("GITHUB_JOB", "test")
	t.Setenv("GITHUB_WORKFLOW", "same name")
	t.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/a.yml@refs/heads/main")
	a := state(t, c, "deps", rs)
	t.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/a.yml@refs/heads/branch")
	branch := state(t, c, "deps", rs)
	if a.Snapshots[0].Prefix != branch.Snapshots[0].Prefix {
		t.Fatal("branch ref blocked default branch fallback")
	}
	t.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/b.yml@refs/heads/main")
	if state(t, c, "deps", rs).Snapshots[0].Prefix == a.Snapshots[0].Prefix {
		t.Fatal("different workflow files collided")
	}
	c.Scope = "variant-b"
	scoped := state(t, c, "deps", rs)
	c.Scope = "variant-c"
	if state(t, c, "deps", rs).Snapshots[0].Prefix == scoped.Snapshots[0].Prefix {
		t.Fatal("matrix scope collided")
	}
	t.Setenv("GITHUB_RUN_ID", "42")
	t.Setenv("GITHUB_RUN_ATTEMPT", "1")
	one := state(t, c, "deps", rs)
	two := state(t, c, "deps", rs)
	if one.Snapshots[0].Key == two.Snapshots[0].Key {
		t.Fatal("repeated action instances collided")
	}
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	three := state(t, c, "deps", rs)
	if one.Snapshots[0].Key == three.Snapshots[0].Key || one.Snapshots[0].Prefix != three.Snapshots[0].Prefix {
		t.Fatal("rerun isolation changed compatibility")
	}
}

func TestPrivateStateContainsNoCredentialsAndPostCleans(t *testing.T) {
	c := DefaultConfig()
	c.Mode = "restore-save"
	s := state(t, c, "deps", roots(t))
	t.Setenv("RUNNER_TEMP", t.TempDir())
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "must-not-persist")
	t.Setenv("ACTIONS_RESULTS_URL", "")
	path, err := persist(s)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("must-not-persist")) {
		t.Fatal("token persisted")
	}
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(path)
		if st.Mode().Perm() != 0600 {
			t.Fatal(st.Mode())
		}
		st, _ = os.Stat(filepath.Dir(path))
		if st.Mode().Perm() != 0700 {
			t.Fatal(st.Mode())
		}
	}
	t.Setenv("STATE_cache_state", path)
	if err = Post(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("post state not cleaned", err)
	}
}

func TestMissingCredentialsReportsMiss(t *testing.T) {
	c := DefaultConfig()
	c.Mode = "restore"
	c.Report = filepath.Join(t.TempDir(), "report.json")
	out := filepath.Join(t.TempDir(), "output")
	put(t, out, "")
	t.Setenv("GITHUB_OUTPUT", out)
	t.Setenv("ACTIONS_RESULTS_URL", "")
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "")
	if err := Start(context.Background(), c, "1.26.8", "unused"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "cache-hit=false\n") {
		t.Fatal(string(b))
	}
	b, _ = os.ReadFile(c.Report)
	if !bytes.Contains(b, []byte("skipped_reason")) {
		t.Fatal(string(b))
	}
	c.Strict = true
	if err := Start(context.Background(), c, "1.26.8", "unused"); err == nil {
		t.Fatal("strict missing credentials ignored")
	}
}

func TestRunnerPermissions(t *testing.T) {
	for mode, want := range map[string][2]bool{"none": {false, false}, "read": {true, false}, "write-only": {false, true}, "write": {true, true}, "": {true, true}, "future": {true, true}} {
		t.Setenv("ACTIONS_CACHE_MODE", mode)
		r, w := permissions()
		if r != want[0] || w != want[1] {
			t.Fatal(mode, r, w)
		}
	}
}

func TestDisabledRunnerPolicyReportErrorHonorsStrict(t *testing.T) {
	t.Setenv("ACTIONS_CACHE_MODE", "none")
	t.Setenv("GITHUB_OUTPUT", "")
	c := DefaultConfig()
	c.Mode = "restore"
	c.Report = filepath.Join(t.TempDir(), "missing-parent", "report.json")
	if err := Start(context.Background(), c, "1.26.8", "unused"); err != nil {
		t.Fatal("optional report failure became fatal", err)
	}
	c.Strict = true
	if err := Start(context.Background(), c, "1.26.8", "unused"); err == nil {
		t.Fatal("strict report failure ignored")
	}
}

func TestCancellationStopsSnapshotsAndHonorsStrict(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, deadline := range []bool{false, true} {
			t.Run(fmt.Sprintf("strict=%v/deadline=%v", strict, deadline), func(t *testing.T) {
				c := DefaultConfig()
				c.Mode = "restore-save"
				c.Strict = strict
				s := state(t, c, "deps", roots(t))
				b := &memoryBackend{}
				var ctx context.Context
				var cancel context.CancelFunc
				want := context.Canceled
				if deadline {
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					want = context.DeadlineExceeded
				} else {
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
				}
				defer cancel()
				for _, operation := range []string{"restore", "save"} {
					var r Report
					var err error
					if operation == "restore" {
						r, err = Restore(ctx, b, &s)
					} else {
						r, err = Save(ctx, b, s)
					}
					if strict && !errors.Is(err, want) {
						t.Fatalf("strict %s: %v", operation, err)
					}
					if !strict && err != nil {
						t.Fatalf("optional %s: %v", operation, err)
					}
					if b.lookups != 0 || b.uploads != 0 || len(r.Snapshots) != 0 {
						t.Fatalf("cancelled %s performed work: %+v", operation, r)
					}
					if r.Phase != operation || r.TotalMS < 0 || r.SkippedReason != want.Error() {
						t.Fatalf("cancelled %s report incomplete: %+v", operation, r)
					}
				}
			})
		}
	}
}

func TestCancellationBetweenSnapshots(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, operation := range []string{"restore", "save"} {
			t.Run(fmt.Sprintf("strict=%v/%s", strict, operation), func(t *testing.T) {
				c := DefaultConfig()
				c.Mode = "restore-save"
				c.Strict = strict
				rs := roots(t)
				put(t, filepath.Join(rs[0].Path, "module"), "module snapshot")
				put(t, filepath.Join(rs[1].Path, "build"), "build snapshot")
				s := state(t, c, "deps", rs)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				b := &cancellingBackend{cancel: cancel}
				var r Report
				var err error
				if operation == "restore" {
					r, err = Restore(ctx, b, &s)
				} else {
					r, err = Save(ctx, b, s)
				}
				if strict && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if !strict && err != nil {
					t.Fatal(err)
				}
				if len(r.Snapshots) != 1 || r.Snapshots[0].Error == "" || r.SkippedReason != context.Canceled.Error() {
					t.Fatalf("subsequent archive ran or report lost cancellation: %+v", r)
				}
				if operation == "restore" && b.lookups != 1 {
					t.Fatal(b.lookups)
				}
				if operation == "save" && b.uploads != 1 {
					t.Fatal(b.uploads)
				}
			})
		}
	}
}

func TestMissingCorruptPostStatePolicy(t *testing.T) {
	for _, strict := range []bool{false, true} {
		for _, corrupt := range []bool{false, true} {
			t.Run(fmt.Sprintf("strict=%v/corrupt=%v", strict, corrupt), func(t *testing.T) {
				c := DefaultConfig()
				c.Mode = "restore-save"
				s := state(t, c, "deps", roots(t))
				t.Setenv("RUNNER_TEMP", t.TempDir())
				path, err := persist(s)
				if err != nil {
					t.Fatal(err)
				}
				if corrupt {
					if err = os.WriteFile(path, []byte("bad JSON"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err = os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				t.Setenv("STATE_cache_state", path)
				t.Setenv("STATE_cache_strict", fmt.Sprint(strict))
				err = Post(context.Background())
				if (err != nil) != strict {
					t.Fatalf("strict=%v got %v", strict, err)
				}
			})
		}
	}
}

// Exercise the real Start -> GITHUB_STATE -> Post bridge against a loopback v2
// service, including fresh token injection and actual archive upload.
func TestStartPostBridge(t *testing.T) {
	toolchain := runtime.GOROOT()
	rs := roots(t)
	workspace := t.TempDir()
	put(t, filepath.Join(workspace, "go.mod"), "module example.org/test\n")
	output, stateFile := filepath.Join(t.TempDir(), "output"), filepath.Join(t.TempDir(), "state")
	put(t, output, "")
	put(t, stateFile, "")
	t.Setenv("GITHUB_OUTPUT", output)
	t.Setenv("GITHUB_STATE", stateFile)
	t.Setenv("GITHUB_WORKSPACE", workspace)
	t.Setenv("RUNNER_TEMP", t.TempDir())
	t.Setenv("GOMODCACHE", rs[0].Path)
	t.Setenv("GOCACHE", rs[1].Path)
	t.Setenv("ACTIONS_CACHE_MODE", "")
	token := "main-token"
	var uploaded []byte
	var finalized int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blob" {
			if r.Header.Get("Authorization") != "" {
				t.Error("token sent to blob")
			}
			uploaded, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("wrong lifecycle token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch filepath.Base(r.URL.Path) {
		case "GetCacheEntryDownloadURL":
			fmt.Fprint(w, `{"ok":false}`)
		case "CreateCacheEntry":
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "signed_upload_url": server.URL + "/blob"})
		case "FinalizeCacheEntryUpload":
			finalized++
			fmt.Fprint(w, `{"ok":true}`)
		default:
			t.Error("unknown RPC", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	t.Setenv("ACTIONS_RESULTS_URL", server.URL)
	t.Setenv("ACTIONS_RUNTIME_TOKEN", token)
	c := DefaultConfig()
	c.Mode = "restore-save"
	c.Layout = "combined"
	c.Strict = true
	c.Report = filepath.Join(t.TempDir(), "report.json")
	if err := Start(context.Background(), c, "1.26.8", toolchain); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(stateFile)
	if !strings.Contains(string(data), "cache_strict=true") {
		t.Fatal(string(data))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		k, v, _ := strings.Cut(line, "=")
		t.Setenv("STATE_"+k, v)
	}
	put(t, filepath.Join(rs[0].Path, "module"), "module cache content")
	put(t, filepath.Join(rs[1].Path, "build"), "build cache content")
	token = "post-token"
	t.Setenv("ACTIONS_RUNTIME_TOKEN", token)
	if err := Post(context.Background()); err != nil {
		t.Fatal(err)
	}
	if finalized != 1 || len(uploaded) == 0 {
		t.Fatal("post did not publish snapshot", finalized, len(uploaded))
	}
	if _, err := os.Stat(c.Report + ".post.json"); err != nil {
		t.Fatal(err)
	}
}

func TestServiceVersionUsesFullSHA256(t *testing.T) {
	for _, storage := range []string{"archive", "chunked"} {
		c := DefaultConfig()
		c.Storage = storage
		s := state(t, c, "dependencies", roots(t))
		for _, snap := range s.Snapshots {
			for _, version := range []string{snap.Version, chunkVersion(snap)} {
				decoded, err := hex.DecodeString(version)
				if err != nil || len(decoded) != 32 || len(version) != 64 {
					t.Fatalf("%s/%s: cache service version must retain full SHA-256: %q", storage, snap.Kind, version)
				}
			}
		}
	}
}
