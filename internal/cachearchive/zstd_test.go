package cachearchive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func encodeZstd(t *testing.T, raw []byte, workers int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	encoder, err := NewZstdWriter(&encoded, workers)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encoder.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestZstdCorruptionAndLimitsPreserveLocalData(t *testing.T) {
	raw := archive(t, []fixture{{name: "build/new", body: strings.Repeat("payload", 300)}})
	encoded := encodeZstd(t, raw, 1)
	var header zstd.Header
	if err := header.Decode(encoded); err != nil || !header.HasCheckSum {
		t.Fatalf("missing checksum: %+v %v", header, err)
	}
	damaged := append([]byte(nil), encoded...)
	damaged[len(damaged)-1] ^= 1
	// A non-single-segment frame declaring an 8 MiB window. The decoder must
	// reject the header before reading payload, regardless of expanded size.
	oversizedWindow := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x04, 0x68}
	cases := []struct {
		name string
		data []byte
		max  int64
		want error
	}{
		{name: "checksum", data: damaged},
		{name: "truncated checksum", data: encoded[:len(encoded)-1]},
		{name: "truncated header", data: encoded[:3]},
		{name: "trailing compressed garbage", data: append(append([]byte(nil), encoded...), 1)},
		{name: "trailing decoded garbage", data: encodeZstd(t, append(append([]byte(nil), raw...), 1), 1)},
		{name: "missing tar terminator", data: encodeZstd(t, raw[:len(raw)-1024], 1)},
		{name: "expanded bound", data: encoded, max: int64(len(raw) - 1)},
		{name: "window bound", data: oversizedWindow, want: zstd.ErrWindowSizeExceeded},
		{name: "unsafe path", data: encodeZstd(t, archive(t, []fixture{{name: "build/../escape", body: "bad"}}), 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dest := tempDir(t)
			write(t, dest, "local", "preserved", 0644, time.Now())
			_, err := Restore(context.Background(), bytes.NewReader(tc.data), []Root{{"build", dest}}, Options{Compression: "zstd", MaxBytes: tc.max})
			if err == nil {
				t.Fatal("accepted invalid zstd archive")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("error %v, want %v", err, tc.want)
			}
			if read(t, filepath.Join(dest, "local")) != "preserved" {
				t.Fatal("changed local contents")
			}
			if _, err := os.Stat(filepath.Join(dest, "new")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("published before validation: %v", err)
			}
		})
	}
	if _, err := Restore(context.Background(), bytes.NewReader(encoded), []Root{{"build", tempDir(t)}}, Options{Compression: "zstd", MaxBytes: int64(len(raw))}); err != nil {
		t.Fatalf("exact expanded bound: %v", err)
	}
}

func TestZstdStreamingBeyondDecoderWindowAndConcurrency(t *testing.T) {
	// Expanded streams can exceed both the window and the decoder memory option;
	// the caller's tar budget, rather than a whole-buffer decode, bounds output.
	source := tempDir(t)
	body := strings.Repeat("cached Go compiler output\x00", 1400000)
	write(t, source, "large", body, 0644, time.Unix(1700000000, 0))
	roots := []Root{{"build", source}}
	for _, workers := range []int{1, 4, 16} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			opts := Options{Compression: "zstd", CompressionConcurrency: workers}
			first, second := snapshot(t, roots, opts), snapshot(t, roots, opts)
			if !bytes.Equal(first, second) {
				t.Fatal("unchanged snapshot is nondeterministic")
			}
			decoder, err := NewZstdReader(bytes.NewReader(first))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(decoder)
			decoder.Close()
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) <= 32<<20 {
				t.Fatal("fixture does not span more than two parallel encoding jobs")
			}
			dest := tempDir(t)
			if _, err := Restore(context.Background(), bytes.NewReader(first), []Root{{"build", dest}}, opts); err != nil {
				t.Fatal(err)
			}
			if got := read(t, filepath.Join(dest, "large")); got != body {
				t.Fatal("large stream differs")
			}
		})
	}
}

func TestZstdPackReturnsFinalizationErrorsAndCancellation(t *testing.T) {
	source := tempDir(t)
	write(t, source, "small", "payload", 0644, time.Unix(1700000000, 0))
	for _, workers := range []int{1, 4} {
		for _, short := range []bool{false, true} {
			t.Run(fmt.Sprintf("workers=%d/short=%t", workers, short), func(t *testing.T) {
				want := errors.New("destination failure")
				writer := &rawFailWriter{err: want, short: short}
				_, err := Pack(context.Background(), writer, []Root{{"build", source}}, Options{Compression: "zstd", CompressionConcurrency: workers})
				if short {
					want = io.ErrShortWrite
				}
				if !errors.Is(err, want) {
					t.Fatalf("finalization error %v, want %v", err, want)
				}
			})
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, operation := range []string{"pack", "restore"} {
		var err error
		if operation == "pack" {
			_, err = Pack(ctx, io.Discard, []Root{{"build", source}}, Options{Compression: "zstd"})
		} else {
			encoded := snapshot(t, []Root{{"build", source}}, Options{Compression: "zstd"})
			_, err = Restore(ctx, bytes.NewReader(encoded), []Root{{"build", tempDir(t)}}, Options{Compression: "zstd"})
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s cancellation: %v", operation, err)
		}
	}
}
