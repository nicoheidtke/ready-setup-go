package cachearchive

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rawWriteCounter struct {
	bytes.Buffer
	writes int
}

func (w *rawWriteCounter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

func TestRawPackCoalescesWritesWithoutChangingTar(t *testing.T) {
	source := tempDir(t)
	stamp := time.Unix(1700000000, 123456700)
	for i := range 512 {
		write(t, source, fmt.Sprintf("dir/file%04d", i), strings.Repeat("payload", 19), 0644, stamp)
	}
	// Include a file spanning multiple buffers, as well as many small entries.
	write(t, source, "large", strings.Repeat("large", 120000), 0600, stamp)
	roots := []Root{{Name: "build", Path: source}}
	var raw rawWriteCounter
	stats, err := Pack(context.Background(), &raw, roots, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 513 || raw.writes >= 64 {
		t.Fatalf("files=%d destination writes=%d", stats.Files, raw.writes)
	}
	t.Logf("raw tar: %d files, %d bytes, %d destination writes", stats.Files, raw.Len(), raw.writes)
	compressed := snapshot(t, roots, Options{Compression: "gzip"})
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	want, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw.Bytes(), want) {
		t.Fatal("raw buffering changed tar bytes")
	}
	destination := filepath.Join(tempDir(t), "restored")
	if _, err := Restore(context.Background(), bytes.NewReader(raw.Bytes()), []Root{{Name: "build", Path: destination}}, Options{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(destination, "dir", "file0000")); got != strings.Repeat("payload", 19) {
		t.Fatal("restored payload differs")
	}
	var limited bytes.Buffer
	if _, err := Pack(context.Background(), &limited, roots, Options{MaxBytes: int64(raw.Len() - 1)}); err == nil {
		t.Fatal("buffered packing ignored tar size limit")
	}
	if limited.Len() >= raw.Len() {
		t.Fatal("size-limited packing wrote complete archive")
	}
}

type rawFailWriter struct {
	err   error
	short bool
	calls int
}

func (w *rawFailWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.short {
		return len(p) - 1, nil
	}
	return 0, w.err
}

func TestRawPackReturnsFinalFlushErrors(t *testing.T) {
	source := tempDir(t)
	write(t, source, "small", "payload", 0644, time.Unix(1700000000, 0))
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprint(short), func(t *testing.T) {
			want := errors.New("destination failure")
			writer := &rawFailWriter{err: want, short: short}
			_, err := Pack(context.Background(), writer, []Root{{Name: "build", Path: source}}, Options{})
			if short {
				want = io.ErrShortWrite
			}
			if !errors.Is(err, want) || writer.calls != 1 {
				t.Fatalf("error=%v calls=%d", err, writer.calls)
			}
		})
	}
}

func TestRawPackCancelledDoesNotFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writer := &rawWriteCounter{}
	if _, err := Pack(ctx, writer, nil, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if writer.writes != 0 {
		t.Fatal("cancelled archive flushed buffered output")
	}
}
