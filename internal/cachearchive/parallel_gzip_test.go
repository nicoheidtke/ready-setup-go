package cachearchive

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gzipPayload(n int) []byte {
	r := rand.New(rand.NewSource(77))
	data := make([]byte, n)
	r.Read(data)
	for i := 0; i < n; i += 4096 {
		copy(data[i:min(i+2048, n)], bytes.Repeat([]byte("Go cached compiler object data\x00"), 80))
	}
	return data
}

func encodeBlocks(t *testing.T, payload []byte, workers, writeSize int) []byte {
	t.Helper()
	var out bytes.Buffer
	w := newBlockedGzipWriter(context.Background(), &out, workers, 64<<10)
	for offset := 0; offset < len(payload); offset += writeSize {
		if _, err := w.Write(payload[offset:min(offset+writeSize, len(payload))]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := w.Write([]byte("after close")); err == nil {
		t.Fatal("write after close succeeded")
	}
	return append([]byte(nil), out.Bytes()...)
}

func TestParallelGzipDeterministicAcrossWorkersAndWrites(t *testing.T) {
	data := gzipPayload(19*(64<<10) + 133)
	want := encodeBlocks(t, data, 1, 7777)
	for _, workers := range []int{2, 4, 16} {
		got := encodeBlocks(t, data, workers, 111)
		if !bytes.Equal(want, got) {
			t.Fatalf("workers=%d changed encoded bytes", workers)
		}
	}
	r, err := gzip.NewReader(bytes.NewReader(want))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatalf("invalid concatenated stream: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{nil, []byte("small")} {
		encoded := encodeBlocks(t, data, 4, 100)
		r, err := gzip.NewReader(bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(r)
		r.Close()
		if err != nil || !bytes.Equal(decoded, data) {
			t.Fatalf("empty/small roundtrip: %v", err)
		}
	}
}

type rejectedGzipWriter struct {
	err   error
	short bool
}

func (w rejectedGzipWriter) Write(p []byte) (int, error) {
	if w.short {
		return len(p) - 1, nil
	}
	return 0, w.err
}

func TestParallelGzipDestinationFailureAndCancellationJoin(t *testing.T) {
	sentinel := errors.New("destination failed")
	for _, target := range []struct {
		dst  io.Writer
		want error
	}{{rejectedGzipWriter{err: sentinel}, sentinel}, {rejectedGzipWriter{short: true}, io.ErrShortWrite}} {
		w := newBlockedGzipWriter(context.Background(), target.dst, 4, 64<<10)
		_, writeErr := w.Write(gzipPayload(20 * (64 << 10)))
		closeErr := w.Close()
		if !errors.Is(closeErr, target.want) {
			t.Fatalf("lost destination error: write=%v close=%v", writeErr, closeErr)
		}
		select {
		case <-w.collected:
		default:
			t.Fatal("returned before collector joined")
		}
		if len(w.slots) != 0 {
			t.Fatalf("retained %d in-flight blocks", len(w.slots))
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := newBlockedGzipWriter(ctx, io.Discard, 4, 64<<10)
	if _, err := w.Write(gzipPayload(5*(64<<10) + 1)); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := w.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	if len(w.slots) != 0 {
		t.Fatal("cancelled blocks were not drained")
	}
	select {
	case <-w.collected:
	default:
		t.Fatal("cancel returned before collector joined")
	}
}

type pausedGzipWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *pausedGzipWriter) Write(p []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return len(p), nil
}

func TestParallelGzipBackpressureBoundsReorderQueue(t *testing.T) {
	dst := &pausedGzipWriter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	w := newBlockedGzipWriter(context.Background(), dst, 3, 64<<10)
	done := make(chan error, 1)
	go func() {
		_, err := w.Write(gzipPayload(32 * (64 << 10)))
		if err == nil {
			err = w.Close()
		}
		done <- err
	}()
	select {
	case <-dst.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("collector did not start")
	}
	// A paused destination must stop the producer after a bounded number of blocks.
	if len(w.slots) > 3 || cap(w.ordered) != 3 {
		t.Fatal("unbounded submitted queue")
	}
	select {
	case err := <-done:
		t.Fatalf("producer bypassed backpressure: %v", err)
	default:
	}
	close(dst.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not drain")
	}
}

func TestPackParallelGzipValidatesEveryMemberBeforeRestore(t *testing.T) {
	source := tempDir(t)
	data := gzipPayload(2*gzipBlockBytes + 100)
	name := filepath.Join(source, "object")
	if err := os.WriteFile(name, data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(name, time.Unix(1700000000, 0), time.Unix(1700000000, 0)); err != nil {
		t.Fatal(err)
	}
	one := snapshot(t, []Root{{"build", source}}, Options{Compression: "gzip", CompressionConcurrency: 1})
	four := snapshot(t, []Root{{"build", source}}, Options{Compression: "gzip", CompressionConcurrency: 4})
	if !bytes.Equal(one, four) {
		t.Fatal("packing workers changed archive bytes")
	}
	dest := filepath.Join(tempDir(t), "restored")
	if _, err := Restore(context.Background(), bytes.NewReader(four), []Root{{"build", dest}}, Options{Compression: "gzip"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "object"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("restore: %v", err)
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte { b[len(b)-5] ^= 1; return b },
		func(b []byte) []byte { return b[:len(b)-4] },
	} {
		corrupt := mutate(append([]byte(nil), four...))
		dest := filepath.Join(tempDir(t), "absent")
		if _, err := Restore(context.Background(), bytes.NewReader(corrupt), []Root{{"build", dest}}, Options{Compression: "gzip"}); err == nil {
			t.Fatal("accepted corrupt/truncated final member")
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Fatalf("corrupt member published destination: %v", err)
		}
	}
	var out bytes.Buffer
	if _, err := Pack(context.Background(), &out, []Root{{"build", source}}, Options{Compression: "gzip", MaxBytes: 1024}); err == nil || !strings.Contains(err.Error(), "archive") {
		t.Fatalf("lost expanded-size limit: %v", err)
	}
}
