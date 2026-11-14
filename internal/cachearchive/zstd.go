package cachearchive

import (
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// ZstdWindowSize bounds back-references for both archive and chunked storage.
// Codec buffers and encoder state add overhead beyond this window size.
const ZstdWindowSize = 4 << 20

// NewZstdWriter creates the cache's checksummed, dictionary-free codec profile.
// Concurrent streams use 16 MiB jobs (four windows) with a 512 KiB overlap,
// a bounded ordered queue and at most concurrency compression workers.
// Chunk workers use concurrency 1 to avoid multiplying their worker pools.
func NewZstdWriter(w io.Writer, concurrency int) (*zstd.Encoder, error) {
	if concurrency < 1 || concurrency > 16 {
		return nil, fmt.Errorf("zstd compression concurrency must be 1..16")
	}
	return zstd.NewWriter(w,
		zstd.WithEncoderLevel(zstd.SpeedDefault),
		zstd.WithWindowSize(ZstdWindowSize),
		zstd.WithEncoderConcurrency(concurrency),
		zstd.WithConcurrentBlocks(true),
		zstd.WithEncoderCRC(true),
		zstd.WithLowerEncoderMem(true),
	)
}

// NewZstdReader decodes synchronously with a bounded window. The 8 MiB decoder
// memory option bounds decoded size for DecodeAll and window size for streaming;
// it is not a total process-memory ceiling. Callers must independently bound the
// expanded stream. Disabling whole-buffer decoding keeps that limit streaming.
func NewZstdReader(r io.Reader) (*zstd.Decoder, error) {
	return zstd.NewReader(r,
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
		zstd.WithDecoderMaxWindow(ZstdWindowSize),
		zstd.WithDecoderMaxMemory(8<<20),
		zstd.WithDecodeBuffersBelow(0),
	)
}
