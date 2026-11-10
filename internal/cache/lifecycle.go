package cache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"setup-go-better/internal/actionenv"
	"setup-go-better/internal/cachearchive"
	"setup-go-better/internal/cachebackend"
)

type Backend interface {
	Lookup(context.Context, string, string, []string) (*cachebackend.Entry, error)
	Download(context.Context, cachebackend.Entry, io.Writer) (int64, error)
	Upload(context.Context, string, string, io.ReadSeeker, int64) error
}

type Snapshot struct {
	Kind             string              `json:"kind"`
	Key              string              `json:"key"`
	Prefix           string              `json:"prefix"`
	CompatiblePrefix string              `json:"compatible_prefix"`
	Version          string              `json:"version"`
	Roots            []cachearchive.Root `json:"roots"`
	// RestoredDigest identifies validated archive contents, excluding mtimes.
	RestoredDigest string `json:"restored_digest,omitempty"`
}

type State struct {
	Format    string     `json:"format"`
	Config    Config     `json:"config"`
	Snapshots []Snapshot `json:"snapshots"`
}

type Metric struct {
	Kind               string  `json:"kind"`
	Key                string  `json:"key"`
	MatchedKey         string  `json:"matched_key,omitempty"`
	Hit                bool    `json:"hit"`
	Skipped            bool    `json:"skipped"`
	Bytes              int64   `json:"transferred_bytes"`
	Files              int64   `json:"archive_files"`
	PayloadBytes       int64   `json:"archive_payload_bytes"`
	Chunks             int     `json:"chunks,omitempty"`
	ReusedChunks       int     `json:"reused_chunks,omitempty"`
	ReusedBytes        int64   `json:"reused_bytes,omitempty"`
	LookupMS           float64 `json:"lookup_ms"`
	TransferMS         float64 `json:"transfer_ms"`
	ArchiveMS          float64 `json:"archive_ms"`
	ArchiveStageMS     float64 `json:"archive_stage_ms,omitempty"`
	ArchivePreflightMS float64 `json:"archive_preflight_ms,omitempty"`
	ArchiveMergeMS     float64 `json:"archive_merge_ms,omitempty"`
	ArchiveCleanupMS   float64 `json:"archive_cleanup_ms,omitempty"`
	PublishedFiles     int64   `json:"published_files,omitempty"`
	LinkedFiles        int64   `json:"linked_files,omitempty"`
	CopiedFiles        int64   `json:"copied_files,omitempty"`
	CopiedBytes        int64   `json:"copied_bytes,omitempty"`
	ModuleCheckMS      float64 `json:"module_check_ms,omitempty"`
	SaveReason         string  `json:"save_reason,omitempty"`
	Error              string  `json:"error,omitempty"`
}
type Report struct {
	Phase          string   `json:"phase"`
	PreparationMS  float64  `json:"preparation_ms"`
	PrefetchWaitMS float64  `json:"prefetch_wait_ms,omitempty"`
	SkippedReason  string   `json:"skipped_reason,omitempty"`
	TotalMS        float64  `json:"total_ms"`
	Snapshots      []Metric `json:"snapshots"`
}

func ms(start time.Time) float64 { return float64(time.Since(start)) / float64(time.Millisecond) }
func warning(err error) {
	msg := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(err.Error())
	fmt.Fprintln(os.Stdout, "::warning::setup-go-better cache: "+msg)
}
func failure(c Config, err error) error {
	if c.Strict {
		return err
	}
	warning(err)
	return nil
}

func newBackend(c Config) (Backend, error) {
	url, token := os.Getenv("ACTIONS_RESULTS_URL"), os.Getenv("ACTIONS_RUNTIME_TOKEN")
	if url == "" || token == "" {
		return nil, fmt.Errorf("runtime cache credentials unavailable; use the Node action on a cache-v2-capable GitHub runner")
	}
	return &cachebackend.Client{BaseURL: url, Token: token, MaxBytes: c.MaxBytes, Concurrency: c.UploadConcurrency, BlockSize: c.UploadChunkBytes, DownloadConcurrency: c.DownloadConcurrency, DownloadBlockSize: c.DownloadChunkBytes}, nil
}

// The runner may grant only a subset of cache operations. Unknown/unset modes
// preserve compatibility with runners predating this optional capability.
func permissions() (read, write bool) {
	switch os.Getenv("ACTIONS_CACHE_MODE") {
	case "none":
		return false, false
	case "read":
		return true, false
	case "write-only":
		return false, true
	default:
		return true, true
	}
}

func NewState(c Config, version, dependencies string, roots []cachearchive.Root) (State, error) {
	if err := c.Validate(); err != nil {
		return State{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return State{}, err
	}
	id := digest(os.Getenv("GITHUB_RUN_ID")+"\x00"+os.Getenv("GITHUB_RUN_ATTEMPT")) + "-" + hex.EncodeToString(nonce[:])
	storage := ""
	if c.Storage == "chunked" {
		storage = "chunks-"
	}
	base := storage + fmt.Sprintf("%s-%s-%s-go%s-%s-%s-%s-", Format, runtime.GOOS, runtime.GOARCH, version, namespace(c.Scope), c.Layout, c.Compression)
	s := State{Format: Format, Config: c}
	groups := [][]cachearchive.Root{roots}
	if c.Layout == "split" {
		groups = nil
		for _, r := range roots {
			groups = append(groups, []cachearchive.Root{r})
		}
	}
	for _, rs := range groups {
		kind := "combined"
		if len(rs) == 1 {
			kind = rs[0].Name
		}
		compatible := base + kind + "-"
		prefix := compatible + dependencies + "-"
		s.Snapshots = append(s.Snapshots, Snapshot{Kind: kind, Roots: rs, CompatiblePrefix: compatible, Prefix: prefix, Key: prefix + id, Version: versionDigest(Format + "\x00" + c.Compression + "\x00" + kind)})
	}
	return s, nil
}

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
		err := func() error {
			if snap.Kind == "modules" && snap.RestoredDigest != "" {
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
			f, err := os.CreateTemp("", "setup-go-better-save-*")
			if err != nil {
				return err
			}
			defer os.Remove(f.Name())
			defer f.Close()
			opts := cachearchive.Options{Compression: s.Config.Compression, MaxBytes: s.Config.MaxBytes}
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

func writeReport(path string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "cache %s: %.1f ms, %s\n", r.Phase, r.TotalMS, strings.TrimSpace(string(b)))
	if path == "" {
		return nil
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func persist(s State) (string, error) {
	dir, err := os.MkdirTemp(os.Getenv("RUNNER_TEMP"), "setup-go-better-state-*")
	if err != nil {
		return "", err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		os.Remove(dir)
		return "", err
	}
	b, err := json.Marshal(s)
	if err != nil {
		os.Remove(dir)
		return "", err
	}
	path := filepath.Join(dir, "state.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		os.Remove(dir)
		return "", err
	}
	return path, nil
}

// Start runs only after successful toolchain setup. Disabled mode incurs no Go
// environment query, fingerprint walk, backend construction, or state file.
func Start(ctx context.Context, c Config, version, toolchain string) error {
	return StartPrefetched(ctx, c, version, toolchain, nil)
}

func StartPrefetched(ctx context.Context, c Config, version, toolchain string, p *Prefetch) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Mode == "off" {
		return nil
	}
	prep := time.Now()
	unavailable := func(err error) error {
		r := Report{Phase: "restore", PreparationMS: ms(prep), SkippedReason: err.Error()}
		if e := writeReport(c.Report, r); e != nil {
			return failure(c, e)
		}
		return failure(c, err)
	}
	for _, o := range [][2]string{{"cache-hit", "false"}, {"cache-key", ""}, {"cache-matched-key", ""}, {"cache-restored-bytes", "0"}, {"cache-restore-ms", "0"}} {
		if err := actionenv.Output(o[0], o[1]); err != nil {
			return err
		}
	}
	read, write := permissions()
	if !read && (!write || c.Mode == "restore") {
		if err := writeReport(c.Report, Report{Phase: "restore", SkippedReason: "runner cache policy disables requested operations"}); err != nil {
			return failure(c, err)
		}
		return nil
	}
	var b Backend
	var s State
	var err error
	if p != nil {
		select {
		case <-p.done:
			if p.err != nil {
				return unavailable(p.err)
			}
		default:
		}
	}
	if p == nil {
		b, err = newBackend(c)
		if err != nil {
			return unavailable(err)
		}
	}
	roots, err := GoRoots(ctx, toolchain)
	if err != nil {
		return unavailable(err)
	}
	preparationMS, waitMS := ms(prep), 0.0
	if p != nil {
		wait := time.Now()
		<-p.done
		waitMS = ms(wait)
		if p.err != nil {
			return unavailable(p.err)
		}
		b, s = p.backend, p.state
	}
	if p != nil {
		for i := range s.Snapshots {
			if s.Snapshots[i].Kind == "combined" {
				s.Snapshots[i].Roots = roots
			} else {
				for _, root := range roots {
					if root.Name == s.Snapshots[i].Kind {
						s.Snapshots[i].Roots = []cachearchive.Root{root}
					}
				}
			}
		}
	} else {
		workspace := os.Getenv("GITHUB_WORKSPACE")
		if workspace == "" {
			workspace, err = os.Getwd()
			if err != nil {
				return unavailable(err)
			}
		}
		deps, err := Dependencies(ctx, workspace, c.DependencyPaths)
		if err != nil {
			return unavailable(err)
		}
		s, err = NewState(c, version, deps, roots)
		if err != nil {
			return err
		}
	}
	if p == nil {
		preparationMS = ms(prep)
	}
	r := Report{Phase: "restore", SkippedReason: "runner cache policy disables restore"}
	var restoreErr error
	if read {
		if p == nil {
			r, restoreErr = Restore(ctx, b, &s)
		} else {
			r, restoreErr = p.merge(ctx, &s)
		}
	}
	if p != nil {
		preparationMS += p.preparationMS
	}
	r.PreparationMS = preparationMS
	r.PrefetchWaitMS = waitMS
	hit := len(r.Snapshots) > 0
	var keys, matched []string
	var bytes int64
	for _, m := range r.Snapshots {
		hit = hit && m.Hit
		keys = append(keys, m.Key)
		if m.MatchedKey != "" {
			matched = append(matched, m.MatchedKey)
		}
		bytes += m.Bytes
	}
	for _, o := range [][2]string{{"cache-hit", fmt.Sprint(hit)}, {"cache-key", strings.Join(keys, ",")}, {"cache-matched-key", strings.Join(matched, ",")}, {"cache-restored-bytes", fmt.Sprint(bytes)}, {"cache-restore-ms", fmt.Sprintf("%.3f", r.TotalMS)}} {
		if err = actionenv.Output(o[0], o[1]); err != nil {
			return err
		}
	}
	if err = writeReport(c.Report, r); err != nil {
		return failure(c, errors.Join(restoreErr, err))
	}
	if restoreErr != nil {
		return restoreErr
	}
	if c.Mode != "restore-save" || !write {
		return nil
	}
	if os.Getenv("GITHUB_STATE") == "" {
		return failure(c, fmt.Errorf("GITHUB_STATE unavailable; post-job save requires the Node action"))
	}
	path, err := persist(s)
	if err != nil {
		return failure(c, err)
	}
	if err = actionenv.State("cache_state", path); err != nil {
		os.Remove(path)
		os.Remove(filepath.Dir(path))
		return failure(c, err)
	}
	if err = actionenv.State("cache_strict", fmt.Sprint(c.Strict)); err != nil {
		os.Remove(path)
		os.Remove(filepath.Dir(path))
		return failure(c, err)
	}
	return nil
}

// Post uses freshly injected runtime credentials; neither token nor signed
// download URLs are persisted in the private action state.
func Post(ctx context.Context) error {
	path := os.Getenv("STATE_cache_state")
	if path == "" {
		return nil
	}
	policy := DefaultConfig()
	policy.Strict = os.Getenv("STATE_cache_strict") == "true"
	stateError := func(err error) error { return failure(policy, fmt.Errorf("cache post state: %w", err)) }
	if !filepath.IsAbs(path) || filepath.Base(path) != "state.json" || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "setup-go-better-state-") {
		return stateError(fmt.Errorf("invalid cache post state path"))
	}
	st, err := os.Lstat(path)
	if err != nil {
		return stateError(err)
	}
	if !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return stateError(fmt.Errorf("invalid cache post state"))
	}
	defer os.Remove(filepath.Dir(path))
	defer os.Remove(path)
	b, err := os.ReadFile(path)
	if err != nil {
		return stateError(err)
	}
	var s State
	if err = json.Unmarshal(b, &s); err != nil {
		return stateError(err)
	}
	if s.Format != Format {
		return stateError(fmt.Errorf("incompatible cache post state"))
	}
	if err = s.Config.Validate(); err != nil {
		return stateError(err)
	}
	if s.Config.Mode != "restore-save" {
		return nil
	}
	if _, write := permissions(); !write {
		return nil
	}
	backend, err := newBackend(s.Config)
	if err != nil {
		return failure(s.Config, err)
	}
	r, saveErr := Save(ctx, backend, s)
	report := s.Config.Report
	if report != "" {
		report += ".post.json"
	}
	if err = writeReport(report, r); err != nil {
		return failure(s.Config, errors.Join(saveErr, err))
	}
	return saveErr
}
