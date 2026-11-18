package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"ready-setup-go/internal/actionlog"
	"ready-setup-go/internal/cachearchive"
	"ready-setup-go/internal/cachebackend"
)

// Prefetch holds private downloads, never credentials in persisted state. It
// may run before Go exists: destination paths are deliberately absent until
// StartPrefetched queries the selected installed executable.
type Prefetch struct {
	cancel        context.CancelFunc
	ready         chan struct{}
	done          chan struct{}
	state         State
	backend       Backend
	snapshots     []prefetchedSnapshot
	err           error
	transferErr   error
	skippedReason string
	preparationMS float64
	transferStart time.Time
	transferEnd   time.Time
}

// A downloader owns each result until it closes ready, then the restore
// consumer can read it without sharing mutable metrics or archive files.
type prefetchedSnapshot struct {
	ready     chan struct{}
	attempted bool
	metric    Metric
	file      string
	hints     []chunkHint
	err       error
}

func BeginPrefetch(ctx context.Context, c Config, version string) *Prefetch {
	if c.Mode == "off" || !c.Overlap {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &Prefetch{cancel: cancel, ready: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		read := p.prepare(ctx, c, version)
		if p.err == nil && read {
			p.prepareDownloads()
		}
		close(p.ready)
		if p.err == nil && read {
			p.download(ctx)
		}
	}()
	return p
}

func (p *Prefetch) prepare(ctx context.Context, c Config, version string) bool {
	start := time.Now()
	if p.err = c.Validate(); p.err != nil {
		return false
	}
	read, write := permissions()
	if !read && (!write || c.Mode == "restore") {
		return false
	}
	p.backend, p.err = newBackend(c)
	if p.err != nil {
		return false
	}
	workspace := os.Getenv("GITHUB_WORKSPACE")
	if workspace == "" {
		workspace, p.err = os.Getwd()
		if p.err != nil {
			return false
		}
	}
	var deps string
	deps, p.err = Dependencies(ctx, workspace, c.DependencyPaths)
	if p.err != nil {
		return false
	}
	p.state, p.err = NewState(c, version, deps, []cachearchive.Root{{Name: "modules"}, {Name: "build"}})
	p.preparationMS = ms(start)
	return read
}

// Close cancels and joins any pending worker before removing its private files.
func (p *Prefetch) Close() {
	if p == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
	for _, snap := range p.snapshots {
		if snap.file != "" {
			os.Remove(snap.file)
		}
	}
}

func (p *Prefetch) prepareDownloads() {
	p.snapshots = make([]prefetchedSnapshot, len(p.state.Snapshots))
	for i := range p.snapshots {
		p.snapshots[i].ready = make(chan struct{})
	}
	p.transferStart = time.Now()
}

func (p *Prefetch) download(ctx context.Context) {
	if p.snapshots == nil {
		p.prepareDownloads()
	}
	next := 0
	defer func() {
		p.transferEnd = time.Now()
		if err := ctx.Err(); err != nil {
			p.skippedReason, p.transferErr = err.Error(), err
		}
		// A cancelled downloader must also release consumers waiting for a
		// snapshot that was never started.
		for ; next < len(p.snapshots); next++ {
			close(p.snapshots[next].ready)
		}
	}()
	for i, snap := range p.state.Snapshots {
		if err := ctx.Err(); err != nil {
			p.skippedReason = err.Error()
			p.transferErr = err
			break
		}
		result := &p.snapshots[i]
		result.attempted = true
		m := Metric{Kind: snap.Kind, Key: snap.Key}
		err := func() (transferErr error) {
			if err := ctx.Err(); err != nil {
				return err
			}
			t := time.Now()
			entry, err := p.backend.Lookup(ctx, snap.Key, snap.Version, []string{snap.Prefix, snap.CompatiblePrefix})
			m.LookupMS = ms(t)
			if err != nil || entry == nil {
				if entry == nil && err == nil {
					m.RestoreReason = "no-match"
				}
				return err
			}
			m.MatchedKey = entry.Key
			if strings.HasPrefix(entry.Key, snap.Prefix) {
				m.MatchType = "dependency-exact"
			} else {
				m.MatchType = "dependency-compatible"
			}
			f, err := os.CreateTemp("", "ready-setup-go-prefetch-*")
			if err != nil {
				return err
			}
			result.file = f.Name()
			ctx, finish := actionlog.Default.Transfer(ctx, "Download "+snap.Kind+" cache", 0, p.state.Config.Storage != "chunked")
			defer func() {
				transferErr = errors.Join(transferErr, f.Close())
				finish(transferErr)
			}()
			t = time.Now()
			if p.state.Config.Storage == "chunked" {
				err = downloadChunks(ctx, p.backend, snap, p.state.Config, *entry, f, &m, &result.hints)
			} else {
				m.Bytes, err = p.backend.Download(ctx, *entry, f)
			}
			m.TransferMS = ms(t)
			if err != nil {
				return err
			}
			return nil
		}()
		if errors.Is(err, errMissingChunk) {
			// Eviction is a normal miss; never merge the downloaded prefix.
			os.Remove(result.file)
			result.file = ""
			m.Skipped = true
			m.RestoreReason = "missing-chunk"
		} else if err != nil {
			os.Remove(result.file)
			result.file = ""
			m.Error = err.Error()
			m.RestoreReason = "restore-failed"
			if errors.Is(err, cachebackend.ErrPermission) {
				m.RestoreReason = "access-denied"
			}
		}
		result.metric = m
		if errors.Is(err, errMissingChunk) {
			err = nil
		}
		result.err = err
		close(result.ready)
		next = i + 1
	}
}

func (p *Prefetch) merge(ctx context.Context, s *State) (Report, error) {
	start := time.Now()
	r := Report{Phase: "restore"}
	var first error
	for i := range p.snapshots {
		result := &p.snapshots[i]
		wait := time.Now()
		select {
		case <-result.ready:
		case <-ctx.Done():
			if p.cancel != nil {
				p.cancel()
			}
			<-result.ready
		}
		r.PrefetchWaitMS += ms(wait)
		if !result.attempted {
			break
		}
		m := result.metric
		s.Snapshots[i].RestoredDigest = ""
		s.Snapshots[i].DependencyMatch = false
		s.Snapshots[i].RestoredChunks = nil
		err := func() error {
			if result.err != nil {
				return result.err
			}
			if result.file == "" {
				return nil
			}
			f, err := os.Open(result.file)
			if err != nil {
				return err
			}
			defer f.Close()
			compression := s.Config.Compression
			if s.Config.Storage == "chunked" {
				compression = "none"
			}
			t := time.Now()
			stats, err := cachearchive.Restore(ctx, f, s.Snapshots[i].Roots, cachearchive.Options{Compression: compression, RestoreConcurrency: s.Config.RestoreConcurrency, MaxBytes: s.Config.MaxBytes, ContentIdentity: m.Kind == "modules" && s.Config.SavePolicy != "on-miss"})
			m.ArchiveMS = ms(t)
			m.ArchiveStageMS, m.ArchivePreflightMS = stats.StageMS, stats.PreflightMS
			m.ArchiveMergeMS, m.ArchiveCleanupMS = stats.MergeMS, stats.CleanupMS
			m.PublishedFiles, m.LinkedFiles, m.CopiedBytes = stats.PublishedFiles, stats.LinkedFiles, stats.CopiedBytes
			m.CopiedFiles = stats.CopiedFiles
			if err != nil {
				return err
			}
			s.Snapshots[i].RestoredChunks = result.hints
			m.Hit = true
			s.Snapshots[i].DependencyMatch = m.MatchType == "dependency-exact"
			m.Files = stats.Files
			m.PayloadBytes = stats.Bytes
			if m.Kind == "modules" {
				s.Snapshots[i].RestoredDigest = stats.ContentDigest
			}
			return nil
		}()
		if err != nil {
			m.Error = err.Error()
			if m.RestoreReason == "" {
				m.RestoreReason = "restore-failed"
			}
			if first == nil {
				first = err
			}
			if !s.Config.Strict {
				warning(fmt.Errorf("restore %s: %w", m.Kind, err))
			}
		}
		r.Snapshots = append(r.Snapshots, m)
	}
	if p.done != nil {
		<-p.done
	}
	r.SkippedReason = p.skippedReason
	// Report the union of transfer and restore spans. They overlap in pipeline
	// mode; an intervening toolchain-only gap is not cache restore work.
	r.TotalMS = restoreElapsedMS(p.transferStart, p.transferEnd, start, time.Now())
	if first == nil {
		first = p.transferErr
	}
	if !s.Config.Strict {
		first = nil
	}
	return r, first
}

func restoreElapsedMS(transferStart, transferEnd, restoreStart, restoreEnd time.Time) float64 {
	elapsed := restoreEnd.Sub(restoreStart)
	if !transferStart.IsZero() {
		if transferEnd.After(restoreStart) {
			transferEnd = restoreStart
		}
		elapsed += transferEnd.Sub(transferStart)
	}
	return float64(elapsed) / float64(time.Millisecond)
}
