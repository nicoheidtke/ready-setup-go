package cachearchive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type entry struct {
	name string
	hdr  tar.Header
}

// Restore validates the complete archive and compression checksum in staging.
// Existing regular files win; unrelated entries survive. Corruption causes no
// destination writes. Merge I/O errors may leave newly added files, but never
// overwrite or delete existing files. Concurrent hostile filesystem changes are
// outside the shared-runner trust model; os.Root also confines publication.
func Restore(ctx context.Context, r io.Reader, roots []Root, opts Options) (stats Stats, err error) {
	return restore(ctx, r, roots, opts, defaultMetadataLimits)
}

func restore(ctx context.Context, r io.Reader, roots []Root, opts Options, limits metadataLimits) (stats Stats, err error) {
	opts, err = options(opts)
	if err != nil {
		return
	}
	_, byName, err := rootMap(roots)
	if err != nil {
		return stats, err
	}
	r = &contextReader{ctx: ctx, r: r}
	if opts.Compression == "gzip" {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return stats, err
		}
		defer gz.Close()
		r = gz
	} else if opts.Compression == "zstd" {
		decoder, err := NewZstdReader(r)
		if err != nil {
			return stats, err
		}
		defer decoder.Close()
		r = decoder
	}
	limited := &boundedReader{r: &contextReader{ctx: ctx, r: r}, left: opts.MaxBytes}
	r = limited
	tr := tar.NewReader(r)
	started := time.Now()
	stageFinished := false
	stages := make(map[string]string)
	// Staging is private and directories remain writable until validation ends.
	// Remember successful preparations rather than checking the same parent for
	// every file in a module. Failed preparations must never enter this set.
	stageDirs := make(map[string]bool)
	prepareStageDir := func(name string) error {
		if stageDirs[name] {
			return nil
		}
		if err := os.MkdirAll(name, 0700); err != nil {
			return err
		}
		stageDirs[name] = true
		return nil
	}
	defer func() {
		cleanup := time.Now()
		for _, stage := range stages {
			err = errors.Join(err, os.RemoveAll(stage))
		}
		stats.CleanupMS = elapsedMS(cleanup)
		if !stageFinished {
			stats.StageMS = elapsedMS(started) - stats.CleanupMS
		}
	}()
	var identity *contentIdentity
	if opts.ContentIdentity {
		identity = newContentIdentity()
	}
	copyBuffer := make([]byte, 32*1024)
	var entries []entry
	budget := metadataBudget{limits: limits}
	seen, spellings, parents := make(map[string]byte), make(map[string]string), make(map[string]bool)
	for {
		before := limited.read
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			if limited.read-before < 1024 {
				return stats, errors.New("missing tar end marker")
			}
			break
		}
		if err != nil {
			return stats, err
		}
		name := strings.TrimSuffix(h.Name, "/")
		if err := budget.add(name); err != nil {
			return stats, err
		}
		if err := budget.header(h); err != nil {
			return stats, err
		}
		if err := portable(name); err != nil {
			return stats, err
		}
		if h.Size > opts.MaxBytes-stats.Bytes {
			return stats, errors.New("archive payload exceeds size limit")
		}
		for key := range h.PAXRecords {
			if strings.HasPrefix(key, "GNU.sparse.") {
				return stats, errors.New("sparse cache archives are unsupported")
			}
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return stats, fmt.Errorf("unsupported archive entry %q", name)
		}
		if h.Mode & ^int64(0777) != 0 || h.Size < 0 || (h.Typeflag == tar.TypeDir && h.Size != 0) {
			return stats, fmt.Errorf("invalid entry metadata %q", name)
		}
		parts := strings.Split(name, "/")
		if _, ok := byName[parts[0]]; !ok || (len(parts) == 1 && h.Typeflag != tar.TypeDir) {
			return stats, fmt.Errorf("unknown archive root %q", parts[0])
		}
		key := caseKey(name)
		if _, ok := seen[key]; ok {
			return stats, fmt.Errorf("duplicate archive entry %q", name)
		}
		if h.Typeflag == tar.TypeReg && parents[key] {
			return stats, fmt.Errorf("file replaces directory %q", name)
		}
		for i := range parts {
			prefix := strings.Join(parts[:i+1], "/")
			folded := caseKey(prefix)
			if previous, ok := spellings[folded]; ok && previous != prefix {
				return stats, fmt.Errorf("case-colliding path %q", name)
			}
			spellings[folded] = prefix
			if i < len(parts)-1 {
				if seen[folded] == tar.TypeReg {
					return stats, fmt.Errorf("parent is a file %q", name)
				}
				parents[folded] = true
			}
		}
		seen[key] = h.Typeflag
		stage := stages[parts[0]]
		if stage == "" {
			stage, err = stagingDirectory(byName[parts[0]].Path)
			if err != nil {
				return stats, err
			}
			stages[parts[0]] = stage
		}
		target := filepath.Join(stage, "payload")
		if len(parts) > 1 {
			target = filepath.Join(target, filepath.FromSlash(strings.Join(parts[1:], "/")))
		}
		var contentDigest []byte
		if h.Typeflag == tar.TypeDir {
			err = prepareStageDir(target)
		} else {
			err = prepareStageDir(filepath.Dir(target))
			if err == nil {
				var f *os.File
				f, err = os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err == nil {
					var n int64
					if identity != nil {
						hash := sha256.New()
						n, err = io.CopyBuffer(io.MultiWriter(f, hash), tr, copyBuffer)
						contentDigest = hash.Sum(nil)
					} else {
						n, err = io.CopyBuffer(f, tr, copyBuffer)
					}
					err = errors.Join(err, f.Close())
					stats.Files++
					stats.Bytes += n
				}
			}
		}
		if err != nil {
			return stats, err
		}
		if identity != nil {
			identity.add(name, h.Mode, h.Typeflag == tar.TypeDir, contentDigest)
		}
		entries = append(entries, entry{name, retainedHeader(h)})
	}
	// Drain for compression checksum verification; only tar zero padding is permitted.
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		for _, b := range buf[:n] {
			if b != 0 {
				return stats, errors.New("nonzero data after tar end")
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return stats, err
		}
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	if identity != nil {
		stats.ContentDigest = identity.sum()
	}
	stats.StageMS = elapsedMS(started)
	stageFinished = true
	preflight := time.Now()
	checked := make(map[string]fs.FileInfo)
	// Preflight every explicit destination and implicit ancestor before publication.
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		parts := strings.SplitN(e.name, "/", 2)
		target := byName[parts[0]].Path
		if len(parts) == 2 {
			target = filepath.Join(target, filepath.FromSlash(parts[1]))
		}
		info, err := preflightPath(target, checked)
		if err != nil {
			return stats, err
		}
		if info != nil && (info.IsDir() != (e.hdr.Typeflag == tar.TypeDir) || (!info.IsDir() && !info.Mode().IsRegular())) {
			return stats, fmt.Errorf("incompatible destination %q", target)
		}
	}
	stats.PreflightMS = elapsedMS(preflight)
	publication := time.Now()
	if treeMovesSupported {
		entries, err = publishMissingTrees(ctx, stages, entries, byName, checked, moveTreeNoReplace, &stats)
	}
	if err == nil {
		err = merge(ctx, stages, entries, byName, opts.RestoreConcurrency, &stats)
	}
	stats.MergeMS = elapsedMS(publication)
	return stats, err
}

func elapsedMS(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}

// Keep staging outside destination roots until complete validation, but on the
// destination filesystem where possible so publication need not copy payloads.
func stagingDirectory(destination string) (string, error) {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(abs)
	for {
		info, err := os.Lstat(parent)
		if err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("unsafe staging ancestor %q", parent)
			}
			if err := noSymlinkPath(parent); err != nil {
				return "", err
			}
			if stage, err := os.MkdirTemp(parent, ".ready-setup-go-cache-*"); err == nil {
				return stage, nil
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return os.MkdirTemp("", "ready-setup-go-cache-*")
}
