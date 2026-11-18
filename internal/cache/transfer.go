package cache

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"ready-setup-go/internal/actionlog"
	"ready-setup-go/internal/cachearchive"
)

// Restore downloads independently of destination discovery, then validates and merges.
func Restore(ctx context.Context, b Backend, s *State) (Report, error) {
	r := Report{Phase: "restore"}
	var first error
	for i := range s.Snapshots {
		if err := ctx.Err(); err != nil {
			r.SkippedReason = err.Error()
			return r, failure(s.Config, err)
		}
		// Sequential mode retains only one staged archive at a time. Overlap mode
		// prefetches both split snapshots while installation runs.
		one := State{Format: s.Format, Config: s.Config, Snapshots: s.Snapshots[i : i+1]}
		p := &Prefetch{state: one, backend: b}
		p.download(ctx)
		part, err := p.merge(ctx, &one)
		p.Close()
		r.Snapshots = append(r.Snapshots, part.Snapshots...)
		r.TotalMS += part.TotalMS
		if err != nil && first == nil {
			first = err
		}
	}
	return r, first
}

func Save(ctx context.Context, b Backend, s State) (Report, error) {
	start := time.Now()
	r := Report{Phase: "save"}
	var first error
	for _, snap := range s.Snapshots {
		if err := ctx.Err(); err != nil {
			r.TotalMS = ms(start)
			r.SkippedReason = err.Error()
			return r, failure(s.Config, err)
		}
		m := Metric{Kind: snap.Kind, Key: snap.Key}
		err := func() (transferErr error) {
			if reason := snapshotSaveSkip(s.Config, snap); reason != "" {
				m.Skipped, m.SaveReason = true, reason
				return nil
			}
			if snap.Kind == "modules" && snap.RestoredDigest != "" && s.Config.SavePolicy != "on-miss" {
				t := time.Now()
				stats, err := cachearchive.Fingerprint(ctx, snap.Roots, s.Config.MaxBytes)
				m.ModuleCheckMS = ms(t)
				m.ArchiveMS = m.ModuleCheckMS
				if err != nil {
					return err
				}
				m.Files, m.PayloadBytes = stats.Files, stats.Bytes
				if stats.ContentDigest == snap.RestoredDigest {
					m.Skipped, m.SaveReason = true, "unchanged-module-content"
					return nil
				}
				m.SaveReason = "module-content-or-mode-changed"
			}
			f, err := os.CreateTemp("", "ready-setup-go-save-*")
			if err != nil {
				return err
			}
			defer os.Remove(f.Name())
			defer f.Close()
			opts := cachearchive.Options{GzipLevel: s.Config.GzipLevel, Compression: s.Config.Compression, CompressionConcurrency: s.Config.CompressionConcurrency, MaxBytes: s.Config.MaxBytes}
			if s.Config.Storage == "chunked" {
				opts.Compression = "none"
			}
			if s.Config.BuildMaxAge > 0 && snap.Kind != "modules" {
				opts.TrimBefore = time.Now().Add(-s.Config.BuildMaxAge)
			}
			t := time.Now()
			stats, err := cachearchive.Pack(ctx, f, snap.Roots, opts)
			m.ArchiveMS += ms(t)
			if err != nil {
				return err
			}
			m.Files = stats.Files
			m.PayloadBytes = stats.Bytes
			if stats.Files == 0 {
				m.Skipped = true
				m.SaveReason = "empty-cache"
				return nil
			}
			size, err := f.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}
			if _, err = f.Seek(0, 0); err != nil {
				return err
			}
			t = time.Now()
			total := size
			if s.Config.Storage == "chunked" {
				total = 0 // Raw tar size is not encoded bytes actually uploaded.
			}
			ctx, finish := actionlog.Default.Transfer(ctx, "Upload "+snap.Kind+" cache (acknowledged payload)", total, false)
			defer func() { finish(transferErr) }()
			if s.Config.Storage == "chunked" {
				err = saveChunks(ctx, b, snap, s.Config, f, &m)
			} else {
				err = b.Upload(ctx, snap.Key, snap.Version, f, size)
			}
			m.TransferMS = ms(t)
			if err == nil && s.Config.Storage != "chunked" {
				m.Bytes = size
			}
			return err
		}()
		if err != nil {
			m.Error = err.Error()
			if first == nil {
				first = err
			}
			if !s.Config.Strict {
				warning(fmt.Errorf("save %s: %w", snap.Kind, err))
			}
		}
		r.Snapshots = append(r.Snapshots, m)
	}
	r.TotalMS = ms(start)
	if !s.Config.Strict {
		first = nil
	}
	return r, first
}
