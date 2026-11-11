// Package cachearchive creates portable snapshots and validates complete downloads before merging.
package cachearchive

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Root struct{ Name, Path string }
type Options struct {
	Compression string
	// Worker settings are independent of storage/chunk transfer concurrency.
	// Zero selects four workers; one retains sequential processing.
	CompressionConcurrency int
	RestoreConcurrency     int
	// ContentIdentity computes a content/mode digest while validating modules.
	// Build restores do not need this additional hashing work.
	ContentIdentity bool
	// TrimBefore filters old canonical Go build objects only in the "build" root.
	// It never changes local files or trims modules. Zero disables trimming.
	TrimBefore time.Time
	// MaxBytes bounds the uncompressed tar, including headers. Zero means 20 GiB.
	MaxBytes int64
}

// Stats counts regular-file payload in the archive, including existing files skipped during merge.
type Stats struct {
	Files, Bytes                                          int64
	ContentDigest                                         string
	StageMS, PreflightMS, MergeMS, CleanupMS              float64
	PublishedFiles, LinkedFiles, CopiedFiles, CopiedBytes int64
}

func options(o Options) (Options, error) {
	if o.CompressionConcurrency == 0 {
		o.CompressionConcurrency = 4
	}
	if o.RestoreConcurrency == 0 {
		o.RestoreConcurrency = 4
	}
	if o.CompressionConcurrency < 1 || o.CompressionConcurrency > 16 || o.RestoreConcurrency < 1 || o.RestoreConcurrency > 16 {
		return o, errors.New("archive worker concurrency must be 1..16")
	}
	if o.Compression == "" {
		o.Compression = "none"
	}
	if o.Compression != "none" && o.Compression != "gzip" && o.Compression != "zstd" {
		return o, fmt.Errorf("unsupported compression %q", o.Compression)
	}
	if o.MaxBytes < 0 {
		return o, errors.New("negative archive size limit")
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 20 << 30
	}
	return o, nil
}
func rootMap(roots []Root) ([]Root, map[string]Root, error) {
	ordered := append([]Root(nil), roots...)
	result, folded := make(map[string]Root), make(map[string]bool)
	for _, r := range roots {
		if portable(r.Name) != nil || strings.Contains(r.Name, "/") || r.Path == "" {
			return nil, nil, fmt.Errorf("invalid cache root %q", r.Name)
		}
		key := caseKey(r.Name)
		if folded[key] {
			return nil, nil, fmt.Errorf("duplicate cache root %q", r.Name)
		}
		folded[key], result[r.Name] = true, r
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	return ordered, result, nil
}
