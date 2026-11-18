package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ready-setup-go/internal/actionenv"
	"ready-setup-go/internal/actionlog"
	"ready-setup-go/internal/cachearchive"
)

// Start runs only after successful toolchain setup. Disabled mode incurs no Go
// environment query, fingerprint walk, backend construction, or state file.
func Start(ctx context.Context, c Config, version, toolchain string) error {
	return StartPrefetched(ctx, c, version, toolchain, nil)
}

func StartPrefetched(ctx context.Context, c Config, version, toolchain string, p *Prefetch) error {
	defer p.Close()
	if err := c.Validate(); err != nil {
		return err
	}
	debugConfig(c)
	if c.Mode == "off" {
		actionlog.Default.Printf("Cache SKIP · caching disabled\n")
		summaryReport(c, Report{Phase: "restore", SkippedReason: "caching disabled"}, "")
		return nil
	}
	endStage := actionlog.Default.Stage("Restore Go caches (join prefetch, validate and publish)")
	defer endStage()
	prep := time.Now()
	unavailable := func(err error) error {
		r := Report{Phase: "restore", PreparationMS: ms(prep), SkippedReason: err.Error()}
		summaryReport(c, r, "Post-job save unavailable because cache preparation failed.")
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
		r := Report{Phase: "restore", SkippedReason: "runner cache policy disables requested operations"}
		summaryReport(c, r, "Post-job save disabled by runner cache policy.")
		if err := writeReport(c.Report, r); err != nil {
			return failure(c, err)
		}
		return nil
	}
	var b Backend
	var s State
	var err error
	if p != nil {
		select {
		case <-p.ready:
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
		select {
		case <-p.ready:
		case <-ctx.Done():
			return unavailable(ctx.Err())
		}
		waitMS = ms(wait)
		if p.err != nil {
			return unavailable(p.err)
		}
		b, s = p.backend, p.state
		// The downloader still reads its immutable snapshot identities while
		// this invocation adds local roots and successful restore outcomes.
		s.Snapshots = append([]Snapshot(nil), p.state.Snapshots...)
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
	r.PrefetchWaitMS += waitMS
	savePolicy := "Post-job save requested; save results appear in the post-job summary."
	branchSkip := ""
	if c.Mode == "restore-save" && write {
		branchSkip = saveBranchSkip(c)
	}
	if c.Mode == "restore" {
		savePolicy = "Post-job save disabled: restore-only mode."
	} else if !write {
		savePolicy = "Post-job save disabled: runner cache policy denies writes."
	} else if restoreErr != nil {
		savePolicy = "Post-job save unavailable because strict restore failed."
	} else if branchSkip != "" {
		savePolicy = "Post-job save disabled: " + branchSkip + "."
	}
	summaryReport(c, r, savePolicy)
	hit := len(r.Snapshots) > 0 && len(r.Snapshots) == len(s.Snapshots)
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
	if c.Mode != "restore-save" || !write || branchSkip != "" {
		if c.Mode == "restore" {
			actionlog.Default.Printf("Cache save SKIP · restore-only mode\n")
		} else if branchSkip != "" {
			actionlog.Default.Printf("Cache save SKIP · %s\n", branchSkip)
		} else {
			actionlog.Default.Printf("Cache save SKIP · runner cache policy denies writes\n")
		}
		return nil
	}
	if os.Getenv("GITHUB_STATE") == "" {
		return failure(c, ErrPostStateUnavailable)
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
	endStage := actionlog.Default.Stage("Save Go caches (post-job)")
	defer endStage()
	policy := DefaultConfig()
	policy.Strict = os.Getenv("STATE_cache_strict") == "true"
	stateError := func(err error) error { return failure(policy, fmt.Errorf("cache post state: %w", err)) }
	if !filepath.IsAbs(path) || filepath.Base(path) != "state.json" || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "ready-setup-go-state-") {
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
	debugConfig(s.Config)
	if s.Config.Mode != "restore-save" {
		actionlog.Default.Printf("Cache save SKIP · restore-only or disabled mode\n")
		summaryReport(s.Config, Report{Phase: "save", SkippedReason: "restore-only or disabled mode"}, "")
		return nil
	}
	if _, write := permissions(); !write {
		r := Report{Phase: "save", SkippedReason: "runner cache policy denies writes"}
		summaryReport(s.Config, r, "")
		if err := writeReport(postReportPath(s.Config.Report), r); err != nil {
			return failure(s.Config, err)
		}
		return nil
	}
	if reason := saveBranchSkip(s.Config); reason != "" {
		r := Report{Phase: "save", SkippedReason: reason}
		summaryReport(s.Config, r, "")
		if err := writeReport(postReportPath(s.Config.Report), r); err != nil {
			return failure(s.Config, err)
		}
		return nil
	}
	var backend Backend
	for _, snap := range s.Snapshots {
		if snapshotSaveSkip(s.Config, snap) != "" {
			continue
		}
		backend, err = newBackend(s.Config)
		if err != nil {
			summaryReport(s.Config, Report{Phase: "save", SkippedReason: err.Error()}, "")
			return failure(s.Config, err)
		}
		break
	}
	r, saveErr := Save(ctx, backend, s)
	summaryReport(s.Config, r, "")
	report := postReportPath(s.Config.Report)
	if err = writeReport(report, r); err != nil {
		return failure(s.Config, errors.Join(saveErr, err))
	}
	return saveErr
}
