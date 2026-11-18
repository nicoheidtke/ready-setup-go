package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ready-setup-go/internal/cachearchive"
	"ready-setup-go/internal/cachebackend"
)

type gatedSnapshotBackend struct {
	memoryBackend
	key     string
	started chan string
	release chan struct{}
	exited  chan struct{}
}

func (b *gatedSnapshotBackend) Download(ctx context.Context, e cachebackend.Entry, w io.Writer) (int64, error) {
	if e.Key == b.key {
		var name string
		if f, ok := w.(*os.File); ok {
			name = f.Name()
		}
		b.started <- name
		defer close(b.exited)
		select {
		case <-b.release:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	return b.memoryBackend.Download(ctx, e, w)
}

func startTestPrefetch(t *testing.T, ctx context.Context, b Backend, s State) *Prefetch {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	p := &Prefetch{cancel: cancel, ready: make(chan struct{}), done: make(chan struct{}), state: s, backend: b}
	p.prepareDownloads()
	close(p.ready)
	go func() { defer close(p.done); p.download(ctx) }()
	t.Cleanup(p.Close)
	return p
}

func waitPublishedFile(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if b, err := os.ReadFile(path); err == nil && string(b) == want {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("first snapshot was not published while the next download was blocked", path)
		}
	}
}

func TestPrefetchRestoresFirstSnapshotBeforeNextDownloadCompletes(t *testing.T) {
	for _, storage := range []string{"archive", "chunked"} {
		for _, compression := range []string{"none", "gzip", "zstd"} {
			t.Run(storage+"/"+compression, func(t *testing.T) {
				c := DefaultConfig()
				c.Mode, c.Strict, c.Storage, c.Compression = "restore", true, storage, compression
				seedRoots := roots(t)
				put(t, filepath.Join(seedRoots[0].Path, "module"), "module payload")
				put(t, filepath.Join(seedRoots[1].Path, "object"), "compiled payload")
				seed := state(t, c, "deps", seedRoots)
				b := &gatedSnapshotBackend{key: seed.Snapshots[1].Key, started: make(chan string, 1), release: make(chan struct{}), exited: make(chan struct{})}
				if _, err := Save(context.Background(), b, seed); err != nil {
					t.Fatal(err)
				}
				dest := roots(t)
				put(t, filepath.Join(dest[0].Path, "local"), "local priority")
				workspace, protocol := t.TempDir(), t.TempDir()
				t.Setenv("GITHUB_WORKSPACE", workspace)
				t.Setenv("GOCACHE", dest[1].Path)
				t.Setenv("GOMODCACHE", dest[0].Path)
				t.Setenv("GITHUB_OUTPUT", "")
				t.Setenv("GITHUB_STEP_SUMMARY", "")
				t.Setenv("ACTIONS_CACHE_MODE", "")
				c.Report = filepath.Join(protocol, "restore.json")
				pendingState := state(t, c, "deps", []cachearchive.Root{{Name: "modules"}, {Name: "build"}})
				p := startTestPrefetch(t, context.Background(), b, pendingState)
				result := make(chan error, 1)
				go func() { result <- StartPrefetched(context.Background(), c, "1.26.8", runtime.GOROOT(), p) }()
				select {
				case <-b.started:
				case <-time.After(5 * time.Second):
					t.Fatal("second download did not start")
				}
				waitPublishedFile(t, filepath.Join(dest[0].Path, "module"), "module payload")
				select {
				case <-p.done:
					t.Fatal("downloader completed before its blocked second snapshot")
				default:
				}
				if _, err := os.Stat(filepath.Join(dest[1].Path, "object")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("incomplete second snapshot was published", err)
				}
				close(b.release)
				if err := waitChunkResult(t, result); err != nil {
					t.Fatal(err)
				}
				waitPublishedFile(t, filepath.Join(dest[1].Path, "object"), "compiled payload")
				waitPublishedFile(t, filepath.Join(dest[0].Path, "local"), "local priority")
				data, err := os.ReadFile(c.Report)
				if err != nil {
					t.Fatal(err)
				}
				var r Report
				if err := json.Unmarshal(data, &r); err != nil {
					t.Fatal(err)
				}
				if len(r.Snapshots) != 2 || r.Snapshots[0].Kind != "modules" || r.Snapshots[1].Kind != "build" || r.PrefetchWaitMS < 0 || r.TotalMS <= 0 {
					t.Fatalf("pipeline report lost ordered metrics: %+v", r)
				}
				for i, m := range r.Snapshots {
					if !m.Hit || m.Error != "" || m.MatchedKey != seed.Snapshots[i].Key || m.MatchType != "dependency-exact" || m.Files != 1 || m.Bytes <= 0 {
						t.Fatalf("snapshot metric changed: %+v", m)
					}
					if len(p.state.Snapshots[i].Roots) != 1 || p.state.Snapshots[i].Roots[0].Path != "" || p.state.Snapshots[i].DependencyMatch {
						t.Fatal("consumer mutated the downloader's snapshot state")
					}
					if _, err := os.Stat(p.snapshots[i].file); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("completed setup retained a prefetch file", err)
					}
				}
			})
		}
	}
}

func TestPrefetchPipelineCancellationJoinsIndependentDownloader(t *testing.T) {
	for _, strict := range []bool{false, true} {
		t.Run(fmt.Sprint(strict), func(t *testing.T) {
			c := DefaultConfig()
			c.Strict = strict
			seedRoots := roots(t)
			put(t, filepath.Join(seedRoots[0].Path, "module"), "module payload")
			put(t, filepath.Join(seedRoots[1].Path, "object"), "compiled payload")
			seed := state(t, c, "deps", seedRoots)
			b := &gatedSnapshotBackend{key: seed.Snapshots[1].Key, started: make(chan string, 1), release: make(chan struct{}), exited: make(chan struct{})}
			if _, err := Save(context.Background(), b, seed); err != nil {
				t.Fatal(err)
			}
			dest := roots(t)
			pendingState := state(t, c, "deps", []cachearchive.Root{{Name: "modules"}, {Name: "build"}})
			p := startTestPrefetch(t, context.Background(), b, pendingState)
			restoring := state(t, c, "deps", dest)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type outcome struct {
				report Report
				err    error
			}
			result := make(chan outcome, 1)
			go func() { r, err := p.merge(ctx, &restoring); result <- outcome{r, err} }()
			var partial string
			select {
			case partial = <-b.started:
			case <-time.After(5 * time.Second):
				t.Fatal("second download did not start")
			}
			waitPublishedFile(t, filepath.Join(dest[0].Path, "module"), "module payload")
			cancel()
			var got outcome
			select {
			case got = <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("consumer cancellation did not stop the independently-contexted downloader")
			}
			if strict && !errors.Is(got.err, context.Canceled) || !strict && got.err != nil {
				t.Fatalf("cancellation changed strictness: %v", got.err)
			}
			select {
			case <-b.exited:
			default:
				t.Fatal("merge returned before the cancelled download joined")
			}
			if len(got.report.Snapshots) != 2 || !got.report.Snapshots[0].Hit || got.report.Snapshots[1].Hit || got.report.Snapshots[1].Error == "" || got.report.SkippedReason != context.Canceled.Error() {
				t.Fatalf("cancellation lost partial outcomes: %+v", got.report)
			}
			p.Close()
			if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cancelled pipeline retained partial download", err)
			}
			if _, err := os.Stat(dest[1].Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cancelled second snapshot published destination", err)
			}
		})
	}
}

type closedDownloadBackend struct{ memoryBackend }

func (b *closedDownloadBackend) Download(ctx context.Context, e cachebackend.Entry, w io.Writer) (int64, error) {
	n, err := b.memoryBackend.Download(ctx, e, w)
	return n, errors.Join(err, w.(*os.File).Close())
}

func TestPrefetchDownloadCloseFailureRejectsSnapshot(t *testing.T) {
	c := DefaultConfig()
	c.Strict = true
	seedRoots := roots(t)
	put(t, filepath.Join(seedRoots[0].Path, "module"), "module payload")
	b := &closedDownloadBackend{}
	if _, err := Save(context.Background(), b, state(t, c, "deps", seedRoots)); err != nil {
		t.Fatal(err)
	}
	dest := roots(t)
	restoring := state(t, c, "deps", dest)
	p := &Prefetch{state: restoring, backend: b}
	p.download(context.Background())
	defer p.Close()
	r, err := p.merge(context.Background(), &restoring)
	if err == nil || len(r.Snapshots) != 2 || r.Snapshots[0].Hit || r.Snapshots[0].Error == "" || !strings.Contains(r.Snapshots[0].Error, "closed") {
		t.Fatalf("download finalization failure reported a hit: %+v %v", r, err)
	}
	if _, err := os.Stat(dest[0].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("snapshot published before download finalization succeeded", err)
	}
}

func TestPrefetchToolchainQueryFailureJoinsAndCleansDownload(t *testing.T) {
	c := DefaultConfig()
	c.Mode, c.Strict = "restore", true
	seedRoots := roots(t)
	put(t, filepath.Join(seedRoots[0].Path, "module"), "module payload")
	b := &blockedDownload{started: make(chan string, 1)}
	if _, err := Save(context.Background(), b, state(t, c, "deps", seedRoots)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_OUTPUT", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("ACTIONS_CACHE_MODE", "")
	p := startTestPrefetch(t, context.Background(), b, state(t, c, "deps", []cachearchive.Root{{Name: "modules"}, {Name: "build"}}))
	var partial string
	select {
	case partial = <-b.started:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	if err := StartPrefetched(context.Background(), c, "1.26.8", filepath.Join(t.TempDir(), "missing-toolchain"), p); err == nil {
		t.Fatal("missing selected toolchain did not fail")
	}
	select {
	case <-p.done:
	default:
		t.Fatal("failed setup returned before downloader joined")
	}
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed setup retained download", err)
	}
}

type cancelAfterDownloadBackend struct {
	memoryBackend
	cancel context.CancelFunc
}

func (b *cancelAfterDownloadBackend) Download(ctx context.Context, e cachebackend.Entry, w io.Writer) (int64, error) {
	n, err := b.memoryBackend.Download(ctx, e, w)
	b.cancel()
	return n, err
}

func TestPrefetchUnattemptedSnapshotCannotReportCompleteHit(t *testing.T) {
	c := DefaultConfig()
	c.Mode, c.Strict = "restore", false
	seedRoots := roots(t)
	put(t, filepath.Join(seedRoots[0].Path, "module"), "module payload")
	put(t, filepath.Join(seedRoots[1].Path, "object"), "compiled payload")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &cancelAfterDownloadBackend{cancel: cancel}
	if _, err := Save(context.Background(), b, state(t, c, "deps", seedRoots)); err != nil {
		t.Fatal(err)
	}
	dest, protocol := roots(t), t.TempDir()
	t.Setenv("GOCACHE", dest[1].Path)
	t.Setenv("GOMODCACHE", dest[0].Path)
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("ACTIONS_CACHE_MODE", "")
	output := filepath.Join(protocol, "outputs")
	put(t, output, "")
	t.Setenv("GITHUB_OUTPUT", output)
	c.Report = filepath.Join(protocol, "restore.json")
	p := startTestPrefetch(t, ctx, b, state(t, c, "deps", []cachearchive.Root{{Name: "modules"}, {Name: "build"}}))
	<-p.done
	if err := StartPrefetched(context.Background(), c, "1.26.8", runtime.GOROOT(), p); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil || strings.Contains(string(data), "cache-hit=true") {
		t.Fatalf("partial pipeline reported a complete hit: %s %v", data, err)
	}
	data, err = os.ReadFile(c.Report)
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Snapshots) != 1 || !r.Snapshots[0].Hit || r.SkippedReason != context.Canceled.Error() || p.snapshots[1].attempted {
		t.Fatalf("test did not preserve a partial successful restore: %+v", r)
	}
}

func TestPrefetchElapsedUnionAvoidsOverlapAndToolchainGap(t *testing.T) {
	base := time.Unix(100, 0)
	for _, tc := range []struct {
		name                                                 string
		transferStart, transferEnd, restoreStart, restoreEnd int
		want                                                 float64
	}{
		{"overlap", 0, 20, 10, 30, 30},
		{"toolchain-only gap", 0, 10, 20, 30, 20},
		{"sequential", 0, 10, 10, 30, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := func(ms int) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }
			got := restoreElapsedMS(at(tc.transferStart), at(tc.transferEnd), at(tc.restoreStart), at(tc.restoreEnd))
			if got != tc.want {
				t.Fatalf("restore total includes duplicate/idle time: %v, want %v", got, tc.want)
			}
		})
	}
}
