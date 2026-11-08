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
	"os"
	"strings"
	"sync"

	"setup-go-better/internal/cachebackend"
)

const chunkFormat = "sgb-chunks-v1"
const chunkMinimum = 1 << 20
const chunkMaximum = 8 << 20
const maxChunks = 16384
const maxManifest = 4 << 20

type chunkRef struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
	Raw  int64  `json:"raw"`
}
type chunkManifest struct {
	Format string     `json:"format"`
	Size   int64      `json:"size"`
	Chunks []chunkRef `json:"chunks"`
}

func chunkKey(s Snapshot, hash string) string { return s.CompatiblePrefix + "chunk-" + hash }
func chunkVersion(s Snapshot) string          { return versionDigest(chunkFormat + s.Version) }

// A rolling hash forgets preceding bytes after at most 64 bytes. Boundaries
// resynchronize after insertions, unlike fixed blocks or a monolithic gzip stream.
func chunkEnd(p []byte) int {
	var rolling uint64
	for i, value := range p {
		rolling = (rolling << 1) + (uint64(value)+1)*0x9e3779b185ebca87
		if i+1 >= chunkMinimum && (rolling&((1<<22)-1) == 0 || i+1 == chunkMaximum) {
			return i + 1
		}
	}
	return len(p)
}

func exactLookup(ctx context.Context, b Backend, key, version string) (*cachebackend.Entry, error) {
	e, err := b.Lookup(ctx, key, version, nil)
	if err == nil && e != nil && e.Key != key {
		return nil, errors.New("chunk lookup returned a different key")
	}
	return e, err
}

// chunkTask carries one bounded raw buffer on save or a unique-reference index
// on restore. The unbuffered queue admits at most one task per worker, plus the
// producer's current chunk; no payload reorder queue grows with snapshot size.
// Chunk workers already bound request concurrency. Avoid multiplying this by
// the blob-block worker setting, preserving custom HTTP clients and limits.
func chunkBackend(b Backend) Backend {
	if client, ok := b.(*cachebackend.Client); ok {
		copy := *client
		copy.Concurrency = 1
		return &copy
	}
	return b
}

type chunkTask struct {
	index int
	raw   []byte
}

func chunkWorkers(c Config) int {
	return c.ChunkConcurrency
}

func chunkPipeline(parent context.Context, workers int, produce func(context.Context, func(chunkTask) error) error, process func(context.Context, chunkTask) error) error {
	if workers < 1 || workers > 16 {
		return errors.New("cache-chunk-concurrency must be 1..16")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	jobs := make(chan chunkTask)
	var wg sync.WaitGroup
	var once sync.Once
	var first error
	fail := func(err error) {
		if err != nil {
			once.Do(func() { first = err; cancel() })
		}
	}
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := process(ctx, job); err != nil {
					fail(err)
					return
				}
			}
		}()
	}
	fail(produce(ctx, func(job chunkTask) error {
		select {
		case jobs <- job:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	close(jobs)
	wg.Wait()
	if first != nil {
		return first
	}
	return parent.Err()
}

type chunkCommit struct {
	done chan struct{}
	err  error
}

// saveChunks pipelines bounded CDC buffers through compression and transfer.
// The complete packed input remains on disk so unchanged module archives can
// still skip all chunk work before this function is called.
func saveChunks(ctx context.Context, b Backend, s Snapshot, c Config, f *os.File, m *Metric) error {
	b = chunkBackend(b)
	refs := make([]chunkRef, maxChunks)
	metrics := make([]Metric, maxChunks)
	manifest := chunkManifest{Format: chunkFormat}
	var count int
	var encodedTotal int64
	var mu sync.Mutex
	commits := make(map[string]*chunkCommit)
	err := chunkPipeline(ctx, chunkWorkers(c), func(ctx context.Context, emit func(chunkTask) error) error {
		buffer := make([]byte, chunkMaximum)
		buffered := 0
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := io.ReadFull(f, buffer[buffered:])
			buffered += n
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				return err
			}
			if buffered == 0 {
				return nil
			}
			end := chunkEnd(buffer[:buffered])
			manifest.Size += int64(end)
			if manifest.Size > c.MaxBytes || count >= maxChunks {
				return errors.New("chunked snapshot exceeds size or chunk limit")
			}
			job := chunkTask{index: count, raw: append([]byte(nil), buffer[:end]...)}
			if err := emit(job); err != nil {
				return err
			}
			count++
			copy(buffer, buffer[end:buffered])
			buffered -= end
		}
	}, func(ctx context.Context, job chunkTask) error {
		encoded := job.raw
		if c.Compression == "gzip" {
			var out bytes.Buffer
			gz := gzip.NewWriter(&out)
			if _, err := gz.Write(job.raw); err != nil {
				return err
			}
			if err := gz.Close(); err != nil {
				return err
			}
			encoded = out.Bytes()
		}
		sum := sha256.Sum256(encoded)
		ref := chunkRef{Hash: hex.EncodeToString(sum[:]), Size: int64(len(encoded)), Raw: int64(len(job.raw))}
		refs[job.index] = ref
		mu.Lock()
		encodedTotal += ref.Size
		if encodedTotal > c.MaxBytes {
			mu.Unlock()
			return errors.New("chunked snapshot exceeds size limit")
		}
		commit, duplicate := commits[ref.Hash]
		if !duplicate {
			commit = &chunkCommit{done: make(chan struct{})}
			commits[ref.Hash] = commit
		}
		mu.Unlock()
		metric := &metrics[job.index]
		if duplicate {
			select {
			case <-commit.done:
			case <-ctx.Done():
				return ctx.Err()
			}
			if commit.err != nil {
				return commit.err
			}
			metric.ReusedChunks = 1
			metric.ReusedBytes = ref.Size
			return nil
		}
		err := func() error {
			key := chunkKey(s, ref.Hash)
			entry, err := exactLookup(ctx, b, key, chunkVersion(s))
			if err != nil {
				return err
			}
			if entry != nil {
				metric.ReusedChunks = 1
				metric.ReusedBytes = ref.Size
				return nil
			}
			err = b.Upload(ctx, key, chunkVersion(s), bytes.NewReader(encoded), ref.Size)
			if errors.Is(err, cachebackend.ErrConflict) {
				// Reservation alone does not establish a committed reusable chunk.
				entry, lookupErr := exactLookup(ctx, b, key, chunkVersion(s))
				if lookupErr != nil {
					return lookupErr
				}
				if entry != nil {
					metric.ReusedChunks = 1
					metric.ReusedBytes = ref.Size
					return nil
				}
			}
			if err == nil {
				metric.Bytes = ref.Size
			}
			return err
		}()
		commit.err = err
		close(commit.done)
		return err
	})
	for _, metric := range metrics[:count] {
		m.Bytes += metric.Bytes
		m.ReusedBytes += metric.ReusedBytes
		m.ReusedChunks += metric.ReusedChunks
	}
	if err != nil {
		return err
	}
	manifest.Chunks = refs[:count]
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if len(data) > maxManifest || int64(len(data))+encodedTotal > c.MaxBytes {
		return errors.New("chunked manifest exceeds size limit")
	}
	if err := b.Upload(ctx, s.Key, s.Version, bytes.NewReader(data), int64(len(data))); err != nil {
		return err
	}
	m.Bytes += int64(len(data))
	m.Chunks = count
	return nil
}

// cappedBuffer prevents untrusted manifests or compressed chunks allocating
// beyond their declared bounded size, including with injected test backends.
type cappedBuffer struct {
	bytes.Buffer
	limit int64
}

func (w *cappedBuffer) Write(p []byte) (int, error) {
	if int64(w.Len()+len(p)) > w.limit {
		return 0, errors.New("cache object exceeds size limit")
	}
	return w.Buffer.Write(p)
}

var errMissingChunk = errors.New("chunked snapshot is incomplete (cache chunk evicted)")

func downloadChunks(ctx context.Context, b Backend, s Snapshot, c Config, entry cachebackend.Entry, f *os.File, m *Metric) error {
	b = chunkBackend(b)
	data := &cappedBuffer{limit: min(maxManifest, c.MaxBytes)}
	n, err := b.Download(ctx, entry, data)
	m.Bytes += n
	if err != nil {
		return err
	}
	var manifest chunkManifest
	decoder := json.NewDecoder(bytes.NewReader(data.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return errors.New("invalid chunk manifest")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("invalid trailing chunk manifest data")
	}
	if manifest.Format != chunkFormat || manifest.Size <= 0 || manifest.Size > c.MaxBytes || len(manifest.Chunks) == 0 || len(manifest.Chunks) > maxChunks {
		return errors.New("invalid chunk manifest bounds")
	}
	var rawTotal, encodedTotal int64
	type group struct {
		ref     chunkRef
		offsets []int64
	}
	var groups []group
	byHash := make(map[string]int)
	for _, ref := range manifest.Chunks {
		decoded, err := hex.DecodeString(ref.Hash)
		if err != nil || len(decoded) != sha256.Size || strings.ToLower(ref.Hash) != ref.Hash || ref.Raw <= 0 || ref.Raw > chunkMaximum || ref.Size <= 0 || ref.Size > chunkMaximum+(1<<16) {
			return errors.New("invalid chunk reference")
		}
		if index, ok := byHash[ref.Hash]; ok {
			if groups[index].ref != ref {
				return errors.New("inconsistent duplicate chunk reference")
			}
			groups[index].offsets = append(groups[index].offsets, rawTotal)
		} else {
			byHash[ref.Hash] = len(groups)
			groups = append(groups, group{ref: ref, offsets: []int64{rawTotal}})
		}
		rawTotal += ref.Raw
		encodedTotal += ref.Size
		if rawTotal > c.MaxBytes || encodedTotal+int64(data.Len()) > c.MaxBytes {
			return errors.New("chunked snapshot exceeds size limit")
		}
	}
	if rawTotal != manifest.Size {
		return errors.New("invalid chunked snapshot size")
	}
	// Offsets are validated before workers start. Concurrent writes target only
	// this private archive, never the actual module/build cache destinations.
	metrics := make([]Metric, len(groups))
	err = chunkPipeline(ctx, chunkWorkers(c), func(ctx context.Context, emit func(chunkTask) error) error {
		for i := range groups {
			if err := emit(chunkTask{index: i}); err != nil {
				return err
			}
		}
		return nil
	}, func(ctx context.Context, job chunkTask) error {
		g := groups[job.index]
		ref := g.ref
		metric := &metrics[job.index]
		e, err := exactLookup(ctx, b, chunkKey(s, ref.Hash), chunkVersion(s))
		if err != nil {
			return err
		}
		if e == nil {
			return errMissingChunk
		}
		encoded := &cappedBuffer{limit: ref.Size}
		n, err := b.Download(ctx, *e, encoded)
		metric.Bytes = n
		if err != nil {
			return err
		}
		sum := sha256.Sum256(encoded.Bytes())
		if int64(encoded.Len()) != ref.Size || hex.EncodeToString(sum[:]) != ref.Hash {
			return errors.New("chunk checksum or size mismatch")
		}
		var reader io.Reader = bytes.NewReader(encoded.Bytes())
		var gz *gzip.Reader
		if c.Compression == "gzip" {
			gz, err = gzip.NewReader(reader)
			if err != nil {
				return err
			}
			reader = gz
		}
		raw, err := io.ReadAll(io.LimitReader(reader, ref.Raw+1))
		if gz != nil {
			err = errors.Join(err, gz.Close())
		}
		if err != nil {
			return err
		}
		if int64(len(raw)) != ref.Raw {
			return errors.New("chunk expanded size mismatch")
		}
		for _, offset := range g.offsets {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := f.WriteAt(raw, offset); err != nil {
				return err
			}
		}
		metric.Chunks = len(g.offsets)
		return nil
	})
	for _, metric := range metrics {
		m.Bytes += metric.Bytes
		m.Chunks += metric.Chunks
	}
	return err
}
