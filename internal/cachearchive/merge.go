package cachearchive

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Each existing component is inspected once. A missing ancestor makes every
// descendant absent; symlink/file ancestors still fail before any publication.
func preflightPath(name string, checked map[string]fs.FileInfo) (fs.FileInfo, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	if info, ok := checked[abs]; ok {
		return info, nil
	}
	parent := filepath.Dir(abs)
	if parent != abs {
		info, err := preflightPath(parent, checked)
		if err != nil {
			return nil, err
		}
		if info == nil {
			checked[abs] = nil
			return nil, nil
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("unsafe cache destination component %q", parent)
		}
	}
	info, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		checked[abs] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("unsafe cache destination component %q", abs)
	}
	checked[abs] = info
	return info, nil
}

type directory struct {
	root  *os.Root
	name  string
	mode  fs.FileMode
	mtime time.Time
}

func merge(ctx context.Context, stages map[string]string, entries []entry, roots map[string]Root, workers int, stats *Stats) (err error) {
	opened := make(map[string]*os.Root)
	private := make(map[string]string)
	ready := make(map[*os.Root]map[string]bool)
	var modes []directory
	var jobs []publicationJob
	// Temporarily writable existing module directories always regain their modes,
	// including after cancellation or failure.
	defer func() {
		for rootName, name := range private {
			err = errors.Join(err, opened[rootName].RemoveAll(name))
		}
		for i := len(modes) - 1; i >= 0; i-- {
			d := modes[i]
			if !d.mtime.IsZero() {
				err = errors.Join(err, d.root.Chtimes(d.name, d.mtime, d.mtime))
			}
			err = errors.Join(err, d.root.Chmod(d.name, d.mode))
		}
		for _, r := range opened {
			err = errors.Join(err, r.Close())
		}
	}()
	for i := range entries {
		e := &entries[i]
		if err := ctx.Err(); err != nil {
			return err
		}
		parts := strings.SplitN(e.name, "/", 2)
		r := opened[parts[0]]
		if r == nil {
			if err := os.MkdirAll(roots[parts[0]].Path, 0755); err != nil {
				return err
			}
			r, err = os.OpenRoot(roots[parts[0]].Path)
			if err != nil {
				return err
			}
			opened[parts[0]] = r
			ready[r] = make(map[string]bool)
			if err := writableDirs(r, ".", &modes, ready[r]); err != nil {
				return err
			}
			// Rename cannot replace local files: only a new random private
			// directory receives staging. Final publication uses Link, never Rename.
			if dir, e := os.MkdirTemp(roots[parts[0]].Path, ".ready-setup-go-publish-*"); e == nil {
				if e := os.Rename(filepath.Join(stages[parts[0]], "payload"), filepath.Join(dir, "payload")); e == nil {
					private[parts[0]] = filepath.Base(dir)
				} else {
					os.Remove(dir)
				}
			}
		}
		name := "."
		if len(parts) == 2 {
			name = filepath.FromSlash(parts[1])
		}
		if e.hdr.Typeflag == tar.TypeDir {
			if _, err := r.Lstat(name); errors.Is(err, os.ErrNotExist) {
				if err := writableDirs(r, name, &modes, ready[r]); err != nil {
					return err
				}
				modes = append(modes, directory{r, name, fs.FileMode(e.hdr.Mode), e.hdr.ModTime})
			} else if err != nil {
				return err
			}
			continue
		}
		if info, err := r.Lstat(name); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsafe destination %q", name)
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := writableDirs(r, filepath.Dir(name), &modes, ready[r]); err != nil {
			return err
		}
		// All directory creation and temporary mode changes finish before any
		// workers start. Jobs contain metadata only, never file payload buffers.
		job := publicationJob{root: r, name: name, hdr: &e.hdr}
		if stage := private[parts[0]]; stage != "" {
			job.source, job.private = filepath.Join(stage, "payload", name), true
		} else {
			job.source = filepath.Join(stages[parts[0]], "payload", name)
		}
		jobs = append(jobs, job)
	}
	publication, publishErr := publishFiles(ctx, jobs, workers, publishFile)
	stats.PublishedFiles += publication.PublishedFiles
	stats.LinkedFiles += publication.LinkedFiles
	stats.CopiedFiles += publication.CopiedFiles
	stats.CopiedBytes += publication.CopiedBytes
	return publishErr
}

// The fallback preserves atomic no-replace creation on filesystems where staged
// moves or hard links are unavailable. Publication errors remove only our file.
func publishCopy(ctx context.Context, r *os.Root, name string, hdr tar.Header, openSource func() (*os.File, error), copyBuffer []byte, stats *Stats) error {
	out, err := r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		info, statErr := r.Lstat(name)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe destination %q", name)
		}
		return nil
	}
	if err != nil {
		return err
	}
	in, err := openSource()
	if err == nil {
		var n int64
		n, err = io.CopyBuffer(out, &contextReader{ctx: ctx, r: in}, copyBuffer)
		stats.CopiedBytes += n
		err = errors.Join(err, in.Close())
	}
	if err == nil {
		err = out.Chmod(fs.FileMode(hdr.Mode))
	}
	err = errors.Join(err, out.Close())
	if err == nil {
		err = r.Chtimes(name, hdr.ModTime, hdr.ModTime)
	}
	if err != nil {
		r.Chmod(name, 0600)
		r.Remove(name)
		return err
	}
	stats.PublishedFiles++
	stats.CopiedFiles++
	return nil
}
func writableDirs(r *os.Root, name string, modes *[]directory, ready map[string]bool) error {
	names := []string{"."}
	if name != "." {
		current := ""
		for _, part := range strings.Split(name, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			names = append(names, current)
		}
	}
	for _, name := range names {
		if ready[name] {
			continue
		}
		info, err := r.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			if err := r.Mkdir(name, 0755); err != nil {
				return err
			}
			ready[name] = true
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe cache directory %q", name)
		}
		if info.Mode().Perm()&0200 == 0 {
			if err := r.Chmod(name, info.Mode().Perm()|0700); err != nil {
				return err
			}
			*modes = append(*modes, directory{root: r, name: name, mode: info.Mode().Perm()})
		}
		ready[name] = true
	}
	return nil
}
