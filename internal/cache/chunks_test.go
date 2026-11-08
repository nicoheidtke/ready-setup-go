package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"setup-go-better/internal/cachearchive"
	"setup-go-better/internal/cachebackend"
)

func TestChunkedReuseRestoreEvictionAndCorruption(t *testing.T) {
	for _, compression := range []string{"none", "gzip"} {
		t.Run(compression, func(t *testing.T) {
			ctx := context.Background()
			c := DefaultConfig()
			c.Storage = "chunked"
			c.Layout = "combined"
			c.Compression = compression
			c.Strict = true
			rs := roots(t)
			payload := make([]byte, 24<<20)
			rand.New(rand.NewSource(42)).Read(payload)
			put(t, filepath.Join(rs[1].Path, "object"), string(payload))
			b := &memoryBackend{}
			first := state(t, c, "deps", rs)
			r, err := Save(ctx, b, first)
			if err != nil {
				t.Fatal(err)
			}
			if r.Snapshots[0].Chunks < 3 {
				t.Fatalf("no meaningful chunking: %+v", r)
			}
			// A small early insertion shifts following bytes; CDC should recover and
			// reuse most later chunks even with gzip enabled.
			payload = append([]byte("inserted data"), payload...)
			put(t, filepath.Join(rs[1].Path, "object"), string(payload))
			second := state(t, c, "deps", rs)
			r, err = Save(ctx, b, second)
			if err != nil {
				t.Fatal(err)
			}
			m := r.Snapshots[0]
			if m.ReusedChunks < 2 || m.ReusedBytes == 0 || m.Bytes >= int64(len(payload)) {
				t.Fatalf("unchanged chunks retransferred: %+v", m)
			}
			dest := roots(t)
			s := state(t, c, "deps", dest)
			r, err = Restore(ctx, b, &s)
			if err != nil || !r.Snapshots[0].Hit {
				t.Fatalf("restore %+v %v", r, err)
			}
			got, err := os.ReadFile(filepath.Join(dest[1].Path, "object"))
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatal("restored bytes differ", err)
			}
			var manifest chunkManifest
			if err := json.Unmarshal(b.entries[len(b.entries)-1].data, &manifest); err != nil {
				t.Fatal(err)
			}
			victim := chunkKey(second.Snapshots[0], manifest.Chunks[len(manifest.Chunks)-1].Hash)
			var index int
			for i, e := range b.entries {
				if e.key == victim {
					index = i
					break
				}
			}
			original := append([]byte(nil), b.entries[index].data...)
			b.entries[index].data[0] ^= 1
			dest = roots(t)
			s = state(t, c, "deps", dest)
			if _, err := Restore(ctx, b, &s); err == nil {
				t.Fatal("corrupt chunk accepted")
			}
			if _, err := os.Stat(dest[1].Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("corrupt chunks changed destination")
			}
			b.entries[index].data = original
			b.entries = append(b.entries[:index], b.entries[index+1:]...)
			dest = roots(t)
			s = state(t, c, "deps", dest)
			r, err = Restore(ctx, b, &s)
			if err != nil || r.Snapshots[0].Hit || !r.Snapshots[0].Skipped {
				t.Fatalf("eviction must be clean miss: %+v %v", r, err)
			}
			if _, err := os.Stat(dest[1].Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("incomplete chunks changed destination")
			}
		})
	}
}

func TestChunkManifestBoundsAndReferences(t *testing.T) {
	c := DefaultConfig()
	c.Storage = "chunked"
	c.Strict = true
	c.MaxBytes = 1 << 20
	s := state(t, c, "deps", roots(t))
	snap := s.Snapshots[0]
	for _, manifest := range []chunkManifest{
		{Format: chunkFormat, Size: 1, Chunks: []chunkRef{{Hash: "../unsafe", Size: 1, Raw: 1}}},
		{Format: chunkFormat, Size: c.MaxBytes + 1, Chunks: []chunkRef{{Hash: strings.Repeat("a", 64), Size: 1, Raw: 1}}},
		{Format: chunkFormat, Size: 2, Chunks: []chunkRef{{Hash: strings.Repeat("a", 64), Size: 1, Raw: 1}}},
		{Format: chunkFormat, Size: 1, Chunks: []chunkRef{{Hash: strings.Repeat("a", 64), Size: c.MaxBytes, Raw: 1}}},
	} {
		data, _ := json.Marshal(manifest)
		b := &memoryBackend{entries: []memoryEntry{{snap.Key, snap.Version, data}}}
		if _, err := Restore(context.Background(), b, &s); err == nil {
			t.Fatal("invalid manifest accepted", manifest)
		}
		if b.lookups != len(s.Snapshots) {
			t.Fatal("invalid reference reached chunk lookup", b.lookups)
		}
	}
}

type uncommittedConflict struct{ memoryBackend }

func (b *uncommittedConflict) Upload(context.Context, string, string, io.ReadSeeker, int64) error {
	return cachebackend.ErrConflict
}
func TestChunkReservationIsNotCommitted(t *testing.T) {
	c := DefaultConfig()
	c.Storage = "chunked"
	c.Strict = true
	rs := roots(t)
	put(t, filepath.Join(rs[0].Path, "file"), "content")
	b := &uncommittedConflict{}
	if _, err := Save(context.Background(), b, state(t, c, "deps", rs)); !errors.Is(err, cachebackend.ErrConflict) {
		t.Fatal("reservation accepted", err)
	}
	if len(b.entries) != 0 {
		t.Fatal("published incomplete manifest")
	}
}

type completedConflict struct{ memoryBackend }

func (b *completedConflict) Upload(ctx context.Context, key, version string, r io.ReadSeeker, size int64) error {
	if err := b.memoryBackend.Upload(ctx, key, version, r, size); err != nil {
		return err
	}
	if strings.Contains(key, "-chunk-") {
		return cachebackend.ErrConflict
	}
	return nil
}
func TestCommittedChunkRaceDoesNotCountSkippedPayload(t *testing.T) {
	c := DefaultConfig()
	c.Storage = "chunked"
	c.Strict = true
	c.Layout = "combined"
	rs := roots(t)
	put(t, filepath.Join(rs[0].Path, "file"), "content")
	b := &completedConflict{}
	r, err := Save(context.Background(), b, state(t, c, "deps", rs))
	if err != nil {
		t.Fatal(err)
	}
	m := r.Snapshots[0]
	if m.ReusedChunks != 1 || m.Bytes != int64(len(b.entries[len(b.entries)-1].data)) {
		t.Fatalf("raced payload counted as transferred: %+v", m)
	}
}

func TestPrefetchDoesNotNeedOrMutateDestinations(t *testing.T) {
	ctx := context.Background()
	c := DefaultConfig()
	c.Strict = true
	rs := roots(t)
	put(t, filepath.Join(rs[0].Path, "file"), "original")
	b := &memoryBackend{}
	if _, err := Save(ctx, b, state(t, c, "deps", rs)); err != nil {
		t.Fatal(err)
	}
	s := state(t, c, "deps", []cachearchive.Root{{Name: "modules"}, {Name: "build"}})
	p := &Prefetch{state: s, backend: b}
	p.download(ctx)
	files := append([]string(nil), p.files...)
	dest := roots(t)
	if _, err := os.Stat(dest[0].Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("prefetch created destination")
	}
	s.Snapshots[0].Roots = []cachearchive.Root{dest[0]}
	s.Snapshots[1].Roots = []cachearchive.Root{dest[1]}
	if _, err := p.merge(ctx, &s); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest[0].Path, "file"))
	if err != nil || string(got) != "original" {
		t.Fatal(err, string(got))
	}
	p.Close()
	for _, file := range files {
		if file != "" {
			if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("prefetch leaked file", file)
			}
		}
	}
	c.Mode = "off"
	if BeginPrefetch(ctx, c, "1.26.0") != nil {
		t.Fatal("cache off spawned worker")
	}
	c.Mode = "restore"
	c.Overlap = false
	if BeginPrefetch(ctx, c, "1.26.0") != nil {
		t.Fatal("overlap opt-out spawned worker")
	}
}

type blockedDownload struct {
	memoryBackend
	started chan string
}

func (b *blockedDownload) Download(ctx context.Context, e cachebackend.Entry, w io.Writer) (int64, error) {
	b.started <- w.(*os.File).Name()
	<-ctx.Done()
	return 0, ctx.Err()
}
func TestPrefetchCancellationJoinsAndCleansDownloads(t *testing.T) {
	c := DefaultConfig()
	c.Strict = true
	rs := roots(t)
	put(t, filepath.Join(rs[0].Path, "file"), "content")
	b := &blockedDownload{started: make(chan string, 1)}
	if _, err := Save(context.Background(), b, state(t, c, "deps", rs)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Prefetch{state: state(t, c, "deps", rs), backend: b, cancel: cancel, done: make(chan struct{})}
	go func() { defer close(p.done); p.download(ctx) }()
	var file string
	select {
	case file = <-b.started:
	case <-time.After(3 * time.Second):
		t.Fatal("prefetch did not start")
	}
	closed := make(chan struct{})
	go func() { p.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("prefetch did not join on cancellation")
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled prefetch leaked download", err)
	}
}

func TestCompletedPrefetchErrorAvoidsGoQuery(t *testing.T) {
	c := DefaultConfig()
	c.Mode = "restore"
	c.Strict = true
	p := &Prefetch{done: make(chan struct{}), err: errors.New("prefetch credentials unavailable")}
	close(p.done)
	err := StartPrefetched(context.Background(), c, "1.26.0", filepath.Join(t.TempDir(), "no-go"), p)
	if err == nil || !strings.Contains(err.Error(), "prefetch credentials unavailable") {
		t.Fatal("queried Go before completed prefetch error", err)
	}
}
