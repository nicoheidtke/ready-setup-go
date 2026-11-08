package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"

	"setup-go-better/internal/cachearchive"
)

// Prefetch holds private downloads, never credentials in persisted state. It
// may run before Go exists: destination paths are deliberately absent until
// StartPrefetched queries the selected installed executable.
type Prefetch struct {
	cancel        context.CancelFunc
	done          chan struct{}
	state         State
	backend       Backend
	report        Report
	files         []string
	err           error
	errors        []error
	transferErr   error
	preparationMS float64
}

func BeginPrefetch(ctx context.Context, c Config, version string) *Prefetch {
	if c.Mode == "off" || !c.Overlap {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &Prefetch{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(p.done)
		start := time.Now()
		if p.err = c.Validate(); p.err != nil {
			return
		}
		read, write := permissions()
		if !read && (!write || c.Mode == "restore") {
			return
		}
		p.backend, p.err = newBackend(c)
		if p.err != nil {
			return
		}
		workspace := os.Getenv("GITHUB_WORKSPACE")
		if workspace == "" {
			workspace, p.err = os.Getwd()
			if p.err != nil {
				return
			}
		}
		var deps string
		deps, p.err = Dependencies(ctx, workspace, c.DependencyPaths)
		if p.err != nil {
			return
		}
		p.state, p.err = NewState(c, version, deps, []cachearchive.Root{{Name: "modules"}, {Name: "build"}})
		p.preparationMS = ms(start)
		if p.err == nil && read {
			p.download(ctx)
		}
	}()
	return p
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
	for _, file := range p.files {
		if file != "" {
			os.Remove(file)
		}
	}
}

func (p *Prefetch) download(ctx context.Context) {
	start := time.Now()
	p.report = Report{Phase: "restore"}
	p.files = make([]string, len(p.state.Snapshots))
	for i, snap := range p.state.Snapshots {
		if err := ctx.Err(); err != nil {
			p.report.SkippedReason = err.Error()
			p.transferErr = err
			break
		}
		m := Metric{Kind: snap.Kind, Key: snap.Key}
		err := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			t := time.Now()
			entry, err := p.backend.Lookup(ctx, snap.Key, snap.Version, []string{snap.Prefix, snap.CompatiblePrefix})
			m.LookupMS = ms(t)
			if err != nil || entry == nil {
				return err
			}
			m.MatchedKey = entry.Key
			f, err := os.CreateTemp("", "setup-go-better-prefetch-*")
			if err != nil {
				return err
			}
			defer f.Close()
			p.files[i] = f.Name()
			t = time.Now()
			if p.state.Config.Storage == "chunked" {
				err = downloadChunks(ctx, p.backend, snap, p.state.Config, *entry, f, &m)
			} else {
				m.Bytes, err = p.backend.Download(ctx, *entry, f)
			}
			m.TransferMS = ms(t)
			if err != nil {
				os.Remove(f.Name()) // Close first on Windows below; Close also retries.
				return err
			}
			return nil
		}()
		if errors.Is(err, errMissingChunk) {
			// Eviction is a normal miss; never merge the downloaded prefix.
			os.Remove(p.files[i])
			p.files[i] = ""
			m.Skipped = true
		} else if err != nil {
			os.Remove(p.files[i])
			p.files[i] = ""
			m.Error = err.Error()
		}
		p.report.Snapshots = append(p.report.Snapshots, m)
		if errors.Is(err, errMissingChunk) {
			err = nil
		}
		p.errors = append(p.errors, err)
	}
	p.report.TotalMS = ms(start)
}

func (p *Prefetch) merge(ctx context.Context, s *State) (Report, error) {
	start := time.Now()
	r := p.report
	var first error
	for i := range r.Snapshots {
		m := &r.Snapshots[i]
		s.Snapshots[i].RestoredDigest = ""
		err := func() error {
			if p.errors[i] != nil {
				return p.errors[i]
			}
			if p.files[i] == "" {
				return nil
			}
			f, err := os.Open(p.files[i])
			if err != nil {
				return err
			}
			defer f.Close()
			h := sha256.New()
			compression := s.Config.Compression
			if s.Config.Storage == "chunked" {
				compression = "none"
			}
			t := time.Now()
			stats, err := cachearchive.Restore(ctx, io.TeeReader(f, h), s.Snapshots[i].Roots, cachearchive.Options{Compression: compression, MaxBytes: s.Config.MaxBytes})
			m.ArchiveMS = ms(t)
			if err != nil {
				return err
			}
			m.Hit = true
			m.Files = stats.Files
			m.PayloadBytes = stats.Bytes
			if m.Kind == "modules" {
				s.Snapshots[i].RestoredDigest = hex.EncodeToString(h.Sum(nil))
			}
			return nil
		}()
		if err != nil {
			m.Error = err.Error()
			if first == nil {
				first = err
			}
			if !s.Config.Strict {
				warning(err)
			}
		}
	}
	r.TotalMS += ms(start)
	if first == nil {
		first = p.transferErr
	}
	if !s.Config.Strict {
		first = nil
	}
	return r, first
}
