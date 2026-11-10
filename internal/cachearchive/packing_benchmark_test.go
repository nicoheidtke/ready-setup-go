package cachearchive

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type countGzipBytes struct{ bytes int64 }

func (c *countGzipBytes) Write(p []byte) (int, error) { c.bytes += int64(len(p)); return len(p), nil }

// Opt-in local diagnostic: SETUP_GO_BETTER_COMPRESSION_BENCH_DIR names an
// existing Go build cache. The first 64 MiB of sorted nonempty object payloads
// form one in-memory corpus, removing filesystem variation from compression.
// This is not a hosted workload or end-to-end Pack benchmark.
func TestMeasureGzipGoObjects(t *testing.T) {
	dir := os.Getenv("SETUP_GO_BETTER_COMPRESSION_BENCH_DIR")
	if dir == "" {
		t.Skip("set SETUP_GO_BETTER_COMPRESSION_BENCH_DIR for local compression diagnosis")
	}
	const max = 64 << 20
	var data bytes.Buffer
	files := 0
	stop := fmt.Errorf("corpus complete")
	err := filepath.WalkDir(dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(name, "-d") {
			return nil
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		n, err := io.CopyN(&data, f, int64(max-data.Len()))
		f.Close()
		if n > 0 {
			files++
		}
		if err != nil && err != io.EOF {
			return err
		}
		if data.Len() == max {
			return stop
		}
		return nil
	})
	if err != nil && err != stop {
		t.Fatal(err)
	}
	if data.Len() < 16<<20 {
		t.Fatal("Go object corpus too small")
	}
	t.Logf("corpus_bytes=%d files=%d sha256=%x host_cpus=%d", data.Len(), files, sha256.Sum256(data.Bytes()), runtime.NumCPU())
	old := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(old)
	cpus := []int{2}
	if old != 2 {
		cpus = append(cpus, old)
	}
	for _, cpu := range cpus {
		runtime.GOMAXPROCS(cpu)
		for round := range 3 {
			order := []int{0, 1, 4}
			if round%2 == 1 {
				order = []int{4, 1, 0}
			}
			for _, workers := range order {
				var out countGzipBytes
				var w io.WriteCloser
				started := time.Now()
				if workers == 0 {
					w = gzip.NewWriter(&out)
				} else {
					w = newParallelGzipWriter(context.Background(), &out, workers)
				}
				if _, err := w.Write(data.Bytes()); err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				t.Logf("cpu=%d round=%d workers=%d encoded_bytes=%d elapsed_ms=%.3f", cpu, round, workers, out.bytes, float64(time.Since(started))/float64(time.Millisecond))
			}
		}
	}
}

// TestMeasurePackGoObjects adds filesystem walks, headers and file reads to the
// gzip diagnosis. Every variant reads the same private fixed-mtime snapshot.
func TestMeasurePackGoObjects(t *testing.T) {
	dir := os.Getenv("SETUP_GO_BETTER_COMPRESSION_BENCH_DIR")
	if dir == "" {
		t.Skip("set SETUP_GO_BETTER_COMPRESSION_BENCH_DIR for local packing diagnosis")
	}
	source := t.TempDir()
	const max = 64 << 20
	var corpus bytes.Buffer
	files := 0
	stop := fmt.Errorf("corpus complete")
	stamp := time.Unix(1700000000, 0)
	err := filepath.WalkDir(dir, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(name, "-d") {
			return nil
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(f, int64(max-corpus.Len())))
		f.Close()
		if err != nil {
			return err
		}
		if len(data) == 0 {
			return nil
		}
		rel, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		dest := filepath.Join(source, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			return err
		}
		if err := os.Chtimes(dest, stamp, stamp); err != nil {
			return err
		}
		corpus.Write(data)
		files++
		if corpus.Len() == max {
			return stop
		}
		return nil
	})
	if err != nil && err != stop {
		t.Fatal(err)
	}
	if corpus.Len() < 16<<20 {
		t.Fatal("Go object corpus too small")
	}
	t.Logf("pack_corpus_bytes=%d files=%d sha256=%x", corpus.Len(), files, sha256.Sum256(corpus.Bytes()))
	old := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(old)
	var expected int64
	for round := range 3 {
		order := []int{1, 4}
		if round%2 == 1 {
			order = []int{4, 1}
		}
		for _, workers := range order {
			var out countGzipBytes
			started := time.Now()
			stats, err := Pack(context.Background(), &out, []Root{{"build", source}}, Options{Compression: "gzip", CompressionConcurrency: workers})
			elapsed := time.Since(started)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Bytes != int64(corpus.Len()) || stats.Files != int64(files) {
				t.Fatal("packing fixture changed")
			}
			if expected == 0 {
				expected = out.bytes
			}
			if out.bytes != expected {
				t.Fatal("packing workers changed encoded size")
			}
			t.Logf("pack_cpu=2 round=%d workers=%d encoded_bytes=%d elapsed_ms=%.3f", round, workers, out.bytes, float64(elapsed)/float64(time.Millisecond))
		}
	}
}
