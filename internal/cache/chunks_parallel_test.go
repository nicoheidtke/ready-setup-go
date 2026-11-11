package cache

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"setup-go-better/internal/cachebackend"
)

type gatedChunks struct {
	memoryBackend
	active        atomic.Int32
	peak          atomic.Int32
	started       chan string
	release       chan struct{}
	failure       error
	failOnce      sync.Once
	downloadFirst string
	laterDone     chan struct{}
}

func (b *gatedChunks) enter(ctx context.Context, key string) error {
	active := b.active.Add(1)
	defer b.active.Add(-1)
	for {
		old := b.peak.Load()
		if old >= active || b.peak.CompareAndSwap(old, active) {
			break
		}
	}
	b.started <- key
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
	}
	var err error
	b.failOnce.Do(func() { err = b.failure })
	return err
}
func (b *gatedChunks) Lookup(ctx context.Context, key, version string, prefixes []string) (*cachebackend.Entry, error) {
	if strings.Contains(key, "-chunk-") && b.downloadFirst == "" {
		if err := b.enter(ctx, key); err != nil {
			return nil, err
		}
	}
	return b.memoryBackend.Lookup(ctx, key, version, prefixes)
}
func (b *gatedChunks) Download(ctx context.Context, e cachebackend.Entry, w io.Writer) (int64, error) {
	if strings.Contains(e.Key, "-chunk-") && b.downloadFirst != "" {
		if e.Key == b.downloadFirst || b.downloadFirst == "*" {
			if err := b.enter(ctx, e.Key); err != nil {
				return 0, err
			}
		} else {
			n, err := b.memoryBackend.Download(ctx, e, w)
			select {
			case b.laterDone <- struct{}{}:
			default:
			}
			return n, err
		}
	}
	return b.memoryBackend.Download(ctx, e, w)
}
func chunkTestFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "packed-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
func chunkPayload() []byte { p := make([]byte, 24<<20); rand.New(rand.NewSource(73)).Read(p); return p }
func waitChunk(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case key := <-ch:
		return key
	case <-time.After(5 * time.Second):
		t.Fatal("chunk worker did not start")
		return ""
	}
}
func waitChunkResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("chunk workers failed to drain")
		return nil
	}
}
func TestParallelChunkSaveBoundAndSequentialOption(t *testing.T) {
	payload := chunkPayload()
	for _, workers := range []int{1, 3} {
		t.Run(string(rune('0'+workers)), func(t *testing.T) {
			c := DefaultConfig()
			c.Storage = "chunked"
			c.Compression = "none"
			c.ChunkConcurrency = workers
			snap := state(t, c, "deps", roots(t)).Snapshots[0]
			b := &gatedChunks{started: make(chan string, 32), release: make(chan struct{})}
			result := make(chan error, 1)
			var m Metric
			f := chunkTestFile(t, payload)
			go func() { result <- saveChunks(context.Background(), b, snap, c, f, &m) }()
			for range workers {
				waitChunk(t, b.started)
			}
			if got := b.active.Load(); got != int32(workers) {
				t.Fatalf("active=%d want %d", got, workers)
			}
			close(b.release)
			if err := waitChunkResult(t, result); err != nil {
				t.Fatal(err)
			}
			if got := b.peak.Load(); got != int32(workers) {
				t.Fatalf("peak=%d want %d", got, workers)
			}
			if b.active.Load() != 0 {
				t.Fatal("save left active workers")
			}
			out := chunkTestFile(t, nil)
			entry := cachebackend.Entry{Key: snap.Key}
			if err := downloadChunks(context.Background(), b, snap, c, entry, out, &Metric{}); err != nil {
				t.Fatal(err)
			}
			restored, err := os.ReadFile(out.Name())
			if err != nil || !bytes.Equal(restored, payload) {
				t.Fatal("parallel save order differs", err)
			}
		})
	}
}
func TestParallelChunkFailureCancelsAndDrains(t *testing.T) {
	c := DefaultConfig()
	c.Storage = "chunked"
	c.Compression = "none"
	c.ChunkConcurrency = 3
	sentinel := errors.New("failed chunk lookup")
	b := &gatedChunks{started: make(chan string, 32), release: make(chan struct{}), failure: sentinel}
	snap := state(t, c, "deps", roots(t)).Snapshots[0]
	f := chunkTestFile(t, chunkPayload())
	result := make(chan error, 1)
	go func() { result <- saveChunks(context.Background(), b, snap, c, f, &Metric{}) }()
	for range c.ChunkConcurrency {
		waitChunk(t, b.started)
	}
	close(b.release)
	if err := waitChunkResult(t, result); !errors.Is(err, sentinel) {
		t.Fatalf("failure lost: %v", err)
	}
	if b.active.Load() != 0 {
		t.Fatal("failed save left workers")
	}
	for _, e := range b.entries {
		if e.key == snap.Key {
			t.Fatal("failed save published manifest")
		}
	}
}
func TestParallelChunkRestoreOutOfOrderAndDuplicateMetadata(t *testing.T) {
	c := DefaultConfig()
	c.Storage = "chunked"
	c.Compression = "gzip"
	c.ChunkConcurrency = 3
	snap := state(t, c, "deps", roots(t)).Snapshots[0]
	seed := &memoryBackend{}
	payload := chunkPayload()
	if err := saveChunks(context.Background(), seed, snap, c, chunkTestFile(t, payload), &Metric{}); err != nil {
		t.Fatal(err)
	}
	var manifest chunkManifest
	if err := json.Unmarshal(seed.entries[len(seed.entries)-1].data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Chunks) < 3 {
		t.Fatal("test needs multiple chunks")
	}
	b := &gatedChunks{memoryBackend: memoryBackend{entries: seed.entries}, started: make(chan string, 32), release: make(chan struct{}), downloadFirst: chunkKey(snap, manifest.Chunks[0].Hash), laterDone: make(chan struct{}, 1)}
	out := chunkTestFile(t, nil)
	result := make(chan error, 1)
	go func() {
		result <- downloadChunks(context.Background(), b, snap, c, cachebackend.Entry{Key: snap.Key}, out, &Metric{})
	}()
	waitChunk(t, b.started)
	select {
	case <-b.laterDone:
	case <-time.After(5 * time.Second):
		t.Fatal("restore did not pass blocked first chunk")
	}
	close(b.release)
	if err := waitChunkResult(t, result); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out.Name())
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("out of order restore changed archive", err)
	}
	// Duplicate hash metadata must be identical before dedup can bypass a download.
	bad := manifest.Chunks[0]
	bad.Raw++
	manifest.Chunks = append(manifest.Chunks, bad)
	manifest.Size += bad.Raw
	data, _ := json.Marshal(manifest)
	seed.entries[len(seed.entries)-1].data = data
	before := seed.lookups
	if err := downloadChunks(context.Background(), seed, snap, c, cachebackend.Entry{Key: snap.Key}, chunkTestFile(t, nil), &Metric{}); err == nil {
		t.Fatal("inconsistent duplicate accepted")
	}
	if seed.lookups != before {
		t.Fatal("bad duplicate reached chunk lookup")
	}
}
func TestChunkDedupAndUnchangedModulesSkipWork(t *testing.T) {
	c := DefaultConfig()
	c.Storage = "chunked"
	c.Compression = "none"
	c.Strict = true
	snap := state(t, c, "deps", roots(t)).Snapshots[0]
	b := &memoryBackend{}
	// Zero data has fixed maximum-size boundaries, yielding identical chunks.
	payload := make([]byte, 3*chunkMaximum)
	var m Metric
	if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, payload), &m); err != nil {
		t.Fatal(err)
	}
	if b.uploads != 2 || b.lookups != 1 || m.ReusedChunks != 2 {
		t.Fatalf("duplicate work: uploads=%d lookups=%d metric=%+v", b.uploads, b.lookups, m)
	}
	var restored Metric
	if err := downloadChunks(context.Background(), b, snap, c, cachebackend.Entry{Key: snap.Key}, chunkTestFile(t, nil), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Bytes != int64(chunkMaximum)+int64(len(b.entries[len(b.entries)-1].data)) {
		t.Fatal("duplicate chunk downloaded again", restored.Bytes)
	}
	rs := roots(t)
	put(t, filepath.Join(rs[0].Path, "module"), "same module")
	s := state(t, c, "deps", rs)
	if _, err := Save(context.Background(), b, s); err != nil {
		t.Fatal(err)
	}
	dest := roots(t)
	restoring := state(t, c, "deps", dest)
	if _, err := Restore(context.Background(), b, &restoring); err != nil {
		t.Fatal(err)
	}
	restoring.Snapshots[0].Key += "-new"
	before := b.lookups
	report, err := Save(context.Background(), b, restoring)
	if err != nil || !report.Snapshots[0].Skipped || b.lookups != before {
		t.Fatalf("unchanged module issued requests: %+v %v", report, err)
	}
}
func TestChunkBackendDoesNotMultiplyBlockWorkers(t *testing.T) {
	client := &cachebackend.Client{Concurrency: 16, BlockSize: 1, Token: "test"}
	cloned := chunkBackend(client).(*cachebackend.Client)
	if cloned == client || cloned.Concurrency != 1 || client.Concurrency != 16 || cloned.Token != client.Token || cloned.BlockSize != 1 {
		t.Fatal("chunk backend did not preserve settings/bound block concurrency")
	}
}

func TestParallelChunkCancellationJoinsWorkers(t *testing.T) {
	c := DefaultConfig()
	c.Storage = "chunked"
	c.Compression = "none"
	c.ChunkConcurrency = 3
	b := &gatedChunks{started: make(chan string, 32), release: make(chan struct{})}
	snap := state(t, c, "deps", roots(t)).Snapshots[0]
	f := chunkTestFile(t, chunkPayload())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- saveChunks(ctx, b, snap, c, f, &Metric{}) }()
	for range c.ChunkConcurrency {
		waitChunk(t, b.started)
	}
	cancel()
	if err := waitChunkResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost", err)
	}
	if b.active.Load() != 0 {
		t.Fatal("cancelled save left active workers")
	}
	if len(b.entries) != 0 {
		t.Fatal("cancelled save published objects")
	}
}
func TestParallelChunkRestoreWorkerBound(t *testing.T) {
	c := DefaultConfig()
	c.Storage = "chunked"
	c.Compression = "none"
	c.ChunkConcurrency = 3
	snap := state(t, c, "deps", roots(t)).Snapshots[0]
	seed := &memoryBackend{}
	payload := chunkPayload()
	if err := saveChunks(context.Background(), seed, snap, c, chunkTestFile(t, payload), &Metric{}); err != nil {
		t.Fatal(err)
	}
	b := &gatedChunks{memoryBackend: memoryBackend{entries: seed.entries}, started: make(chan string, 32), release: make(chan struct{}), downloadFirst: "*"}
	out := chunkTestFile(t, nil)
	result := make(chan error, 1)
	go func() {
		result <- downloadChunks(context.Background(), b, snap, c, cachebackend.Entry{Key: snap.Key}, out, &Metric{})
	}()
	for range c.ChunkConcurrency {
		waitChunk(t, b.started)
	}
	if b.active.Load() != int32(c.ChunkConcurrency) {
		t.Fatal("restore failed to overlap downloads")
	}
	close(b.release)
	if err := waitChunkResult(t, result); err != nil {
		t.Fatal(err)
	}
	if b.peak.Load() != int32(c.ChunkConcurrency) || b.active.Load() != 0 {
		t.Fatal("restore exceeded bound or leaked workers")
	}
	got, err := os.ReadFile(out.Name())
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("bounded restore changed bytes", err)
	}
}
func TestChunkConcurrencyInputBounds(t *testing.T) {
	for _, workers := range []int{0, -1, 17} {
		c := DefaultConfig()
		c.ChunkConcurrency = workers
		if c.Validate() == nil {
			t.Fatal("invalid workers accepted", workers)
		}
	}
	for _, workers := range []int{1, 16} {
		c := DefaultConfig()
		c.ChunkConcurrency = workers
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

// Resetting an encoder must retain the original chunk bytes: chunk identities
// hash compressed data, so even a valid gzip stream with different headers or
// compression state would silently lose reuse with existing snapshots.
func TestChunkEncoderResetPreservesEncodedIdentity(t *testing.T) {
	var encoder chunkEncoder
	for _, raw := range [][]byte{
		bytes.Repeat([]byte("module sources"), 10000),
		chunkPayload()[:chunkMaximum],
		{},
		bytes.Repeat([]byte("module sources"), 10000),
	} {
		var want bytes.Buffer
		gz := gzip.NewWriter(&want)
		if _, err := gz.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		got, err := encoder.encode("gzip", raw)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want.Bytes()) {
			t.Fatal("encoder reuse changed gzip bytes and chunk identity")
		}
	}
}

func TestChunkSavePreservesLegacyManifestAcrossWorkers(t *testing.T) {
	// Repeated maximum-size chunks exercise duplicate accounting; a short tail
	// forces encoder reuse with a different length when only one worker runs.
	payload := append(make([]byte, 2*chunkMaximum), []byte("tail")...)
	var want chunkManifest
	want.Format, want.Size = chunkFormat, int64(len(payload))
	for offset := 0; offset < len(payload); {
		end := offset + chunkEnd(payload[offset:min(len(payload), offset+chunkMaximum)])
		raw := payload[offset:end]
		var encoded bytes.Buffer
		gz := gzip.NewWriter(&encoded)
		if _, err := gz.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(encoded.Bytes())
		want.Chunks = append(want.Chunks, chunkRef{Hash: hex.EncodeToString(sum[:]), Size: int64(encoded.Len()), Raw: int64(len(raw))})
		offset = end
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 4} {
		c := DefaultConfig()
		c.Storage, c.Compression, c.ChunkConcurrency = "chunked", "gzip", workers
		snap := state(t, c, "deps", roots(t)).Snapshots[0]
		b := &memoryBackend{}
		var m Metric
		if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, payload), &m); err != nil {
			t.Fatal(err)
		}
		gotJSON := b.entries[len(b.entries)-1].data
		if !bytes.Equal(gotJSON, wantJSON) {
			t.Fatalf("workers=%d changed manifest bytes", workers)
		}
		if b.uploads != 3 || b.lookups != 2 || m.ReusedChunks != 1 || m.ReusedBytes != want.Chunks[0].Size {
			t.Fatalf("workers=%d changed duplicate accounting: uploads=%d lookups=%d metric=%+v", workers, b.uploads, b.lookups, m)
		}
	}
}

func TestZstdChunkSaveIdentitiesAndReuseAcrossWorkers(t *testing.T) {
	payload := append(make([]byte, 2*chunkMaximum), []byte("tail")...)
	var firstManifest []byte
	for _, workers := range []int{1, 4} {
		c := DefaultConfig()
		c.Storage, c.Compression, c.ChunkConcurrency = "chunked", "zstd", workers
		snap := state(t, c, "deps", roots(t)).Snapshots[0]
		b := &memoryBackend{}
		var m Metric
		if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, payload), &m); err != nil {
			t.Fatal(err)
		}
		manifest := b.entries[len(b.entries)-1].data
		if firstManifest == nil {
			firstManifest = append([]byte(nil), manifest...)
		} else if !bytes.Equal(manifest, firstManifest) {
			t.Fatal("zstd chunk identities depend on worker scheduling")
		}
		if b.uploads != 3 || b.lookups != 2 || m.ReusedChunks != 1 {
			t.Fatalf("duplicate chunk not reused: uploads=%d lookups=%d metric=%+v", b.uploads, b.lookups, m)
		}
		out := chunkTestFile(t, nil)
		if err := downloadChunks(context.Background(), b, snap, c, cachebackend.Entry{Key: snap.Key}, out, &Metric{}); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(out.Name())
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatal("zstd chunk roundtrip differs", err)
		}
	}
}

func TestZstdChunkRejectsMalformedAndExpandedBounds(t *testing.T) {
	for _, mutation := range []string{"truncated", "checksum", "expanded-size"} {
		t.Run(mutation, func(t *testing.T) {
			c := DefaultConfig()
			c.Storage, c.Compression = "chunked", "zstd"
			snap := state(t, c, "deps", roots(t)).Snapshots[0]
			b := &memoryBackend{}
			payload := bytes.Repeat([]byte("module sources"), 1000)
			if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, payload), &Metric{}); err != nil {
				t.Fatal(err)
			}
			var manifest chunkManifest
			if err := json.Unmarshal(b.entries[1].data, &manifest); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "truncated":
				b.entries[0].data = b.entries[0].data[:len(b.entries[0].data)-1]
			case "checksum":
				b.entries[0].data[len(b.entries[0].data)-1] ^= 1
			case "expanded-size":
				manifest.Chunks[0].Raw--
				manifest.Size--
			}
			// Update transport identities so malformed streams must be rejected
			// by the codec/output bound, not merely by the encoded SHA-256 check.
			sum := sha256.Sum256(b.entries[0].data)
			manifest.Chunks[0].Hash = hex.EncodeToString(sum[:])
			manifest.Chunks[0].Size = int64(len(b.entries[0].data))
			b.entries[0].key = chunkKey(snap, manifest.Chunks[0].Hash)
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			b.entries[1].data = data
			out := chunkTestFile(t, nil)
			if err := downloadChunks(context.Background(), b, snap, c, cachebackend.Entry{Key: snap.Key}, out, &Metric{}); err == nil {
				t.Fatal("invalid zstd chunk accepted")
			}
			if info, err := out.Stat(); err != nil || info.Size() != 0 {
				t.Fatal("invalid chunk published unvalidated expanded bytes", err)
			}
		})
	}
}
