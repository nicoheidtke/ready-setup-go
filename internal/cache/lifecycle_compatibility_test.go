package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ready-setup-go/internal/cachebackend"
)

func TestLifecycleAccessModesDoNotEscalateRunnerPolicy(t *testing.T) {
	for _, access := range []string{"none", "read", "write-only", ""} {
		for _, mode := range []string{"restore", "restore-save"} {
			for _, overlap := range []bool{false, true} {
				t.Run(access+"/"+mode+"/"+map[bool]string{false: "sequential", true: "prefetch"}[overlap], func(t *testing.T) {
					var requests atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						if filepath.Base(r.URL.Path) != "GetCacheEntryDownloadURL" {
							t.Error("policy allowed unexpected write", r.URL.Path)
						}
						io.WriteString(w, `{"ok":false}`)
					}))
					defer server.Close()
					workspace, protocol := t.TempDir(), t.TempDir()
					put(t, filepath.Join(workspace, "go.mod"), "module example.org/access\ngo 1.26.0\n")
					t.Setenv("GITHUB_WORKSPACE", workspace)
					t.Setenv("RUNNER_TEMP", t.TempDir())
					t.Setenv("GOCACHE", filepath.Join(protocol, "build"))
					t.Setenv("GOMODCACHE", filepath.Join(protocol, "modules"))
					t.Setenv("ACTIONS_CACHE_MODE", access)
					t.Setenv("ACTIONS_RESULTS_URL", server.URL)
					t.Setenv("ACTIONS_RUNTIME_TOKEN", "access-test-token")
					for _, name := range []string{"GITHUB_STATE", "GITHUB_OUTPUT"} {
						path := filepath.Join(protocol, name)
						put(t, path, "")
						t.Setenv(name, path)
					}
					c := DefaultConfig()
					c.Mode, c.Overlap, c.Strict = mode, overlap, true
					c.SaveBranches = "all" // Exercise runner policy independently of branch policy.
					pending := BeginPrefetch(context.Background(), c, "1.26.8")
					defer pending.Close()
					if err := StartPrefetched(context.Background(), c, "1.26.8", runtime.GOROOT(), pending); err != nil {
						t.Fatal(err)
					}
					wantReads := int32(2)
					if access == "none" || access == "write-only" {
						wantReads = 0
					}
					if requests.Load() != wantReads {
						t.Fatalf("lookup requests=%d want %d", requests.Load(), wantReads)
					}
					data, err := os.ReadFile(os.Getenv("GITHUB_STATE"))
					if err != nil {
						t.Fatal(err)
					}
					wantsState := mode == "restore-save" && (access == "" || access == "write-only")
					if strings.Contains(string(data), "cache_state=") != wantsState {
						t.Fatalf("unexpected post registration: %s", data)
					}
					if wantsState {
						for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
							key, value, _ := strings.Cut(line, "=")
							t.Setenv("STATE_"+key, value)
						}
						// Permissions may narrow between main and post. Fresh post
						// policy wins over previously persisted writable configuration.
						t.Setenv("ACTIONS_CACHE_MODE", "read")
						if err := Post(context.Background()); err != nil {
							t.Fatal(err)
						}
						if requests.Load() != wantReads {
							t.Fatal("post wrote after runner policy became read-only")
						}
						if _, err := os.Stat(os.Getenv("STATE_cache_state")); !os.IsNotExist(err) {
							t.Fatal("restricted post leaked private state", err)
						}
					}
				})
			}
		}
	}
}

// rendezvousBackend forces two independent invocations to observe the same
// absent immutable object before either attempts publication. This exercises a
// real local writer race, not GitHub's branch visibility or reservation timing.
type rendezvousBackend struct {
	memoryBackend
	arrived chan struct{}
	release chan struct{}
	once    sync.Once
	chunks  bool
}

func (b *rendezvousBackend) Lookup(ctx context.Context, key, version string, prefixes []string) (*cachebackend.Entry, error) {
	entry, err := b.memoryBackend.Lookup(ctx, key, version, prefixes)
	if err != nil || entry != nil || !b.chunks || !strings.Contains(key, "-chunk-") {
		return entry, err
	}
	b.arrived <- struct{}{}
	select {
	case <-b.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *rendezvousBackend) Upload(ctx context.Context, key, version string, r io.ReadSeeker, size int64) error {
	if !b.chunks {
		b.arrived <- struct{}{}
		select {
		case <-b.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return b.memoryBackend.Upload(ctx, key, version, r, size)
}

func TestConcurrentImmutableArchiveWritersDoNotReplaceWinner(t *testing.T) {
	c := DefaultConfig()
	c.Mode, c.Layout, c.Compression, c.Strict = "restore-save", "combined", "none", true
	b := &rendezvousBackend{arrived: make(chan struct{}, 2), release: make(chan struct{})}
	defer b.once.Do(func() { close(b.release) })
	results := make(chan error, 2)
	states := make([]State, 2)
	for i, content := range []string{"first writer", "second writer"} {
		rs := roots(t)
		put(t, rs[0].Path+string(os.PathSeparator)+"file", content)
		states[i] = state(t, c, "deps", rs)
	}
	// Deliberately collide the normally fresh keys to exercise immutable backend
	// contention. Normal action instances generate distinct invocation nonces.
	states[1].Snapshots[0].Key = states[0].Snapshots[0].Key
	for _, s := range states {
		go func() { _, err := Save(context.Background(), b, s); results <- err }()
	}
	for range 2 {
		select {
		case <-b.arrived:
		case <-time.After(10 * time.Second):
			t.Fatal("writers did not reach publication")
		}
	}
	b.once.Do(func() { close(b.release) })
	var success, conflicts int
	for range 2 {
		switch err := waitChunkResult(t, results); {
		case err == nil:
			success++
		case errors.Is(err, cachebackend.ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 || len(b.entries) != 1 || b.uploads != 1 {
		t.Fatalf("immutable winner replaced: successes=%d conflicts=%d entries=%d uploads=%d", success, conflicts, len(b.entries), b.uploads)
	}
	if len(b.entries[0].data) == 0 {
		t.Fatal("winner published no content")
	}
}

func TestConcurrentChunkWritersReuseFinalizedWinner(t *testing.T) {
	c := DefaultConfig()
	c.Storage, c.Compression = "chunked", "none"
	c.Mode, c.Layout, c.Strict = "restore-save", "combined", true
	snapshots := []Snapshot{state(t, c, "deps", roots(t)).Snapshots[0], state(t, c, "deps", roots(t)).Snapshots[0]}
	b := &rendezvousBackend{chunks: true, arrived: make(chan struct{}, 2), release: make(chan struct{})}
	defer b.once.Do(func() { close(b.release) })
	payload := []byte("the same chunk in two independent action invocations")
	files := []*os.File{chunkTestFile(t, payload), chunkTestFile(t, payload)}
	metrics := make([]Metric, 2)
	results := make(chan error, 2)
	for i := range 2 {
		go func() { results <- saveChunks(context.Background(), b, snapshots[i], c, files[i], &metrics[i]) }()
	}
	for range 2 {
		select {
		case <-b.arrived:
		case <-time.After(10 * time.Second):
			t.Fatal("writers did not observe absent chunk")
		}
	}
	b.once.Do(func() { close(b.release) })
	for range 2 {
		if err := waitChunkResult(t, results); err != nil {
			t.Fatal(err)
		}
	}
	if b.uploads != 3 || len(b.entries) != 3 || metrics[0].ReusedChunks+metrics[1].ReusedChunks != 1 {
		t.Fatalf("expected one immutable chunk and two fresh manifests: uploads=%d entries=%d metrics=%+v", b.uploads, len(b.entries), metrics)
	}
	for _, snap := range snapshots {
		out := chunkTestFile(t, nil)
		if err := downloadChunks(context.Background(), b, snap, c, cachebackend.Entry{Key: snap.Key}, out, &Metric{}); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(out.Name())
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("concurrent manifest is incomplete: %q %v", got, err)
		}
	}
}
