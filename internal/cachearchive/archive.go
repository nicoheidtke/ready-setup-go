// Package cachearchive creates portable snapshots and validates complete downloads before merging.
package cachearchive

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
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
	if o.Compression != "none" && o.Compression != "gzip" {
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

// Pack is deterministic for unchanged contents, modes, and mtimes. Missing roots
// are skipped; symbolic links and special files are rejected.
func Pack(ctx context.Context, w io.Writer, roots []Root, opts Options) (stats Stats, err error) {
	opts, err = options(opts)
	if err != nil {
		return
	}
	ordered, _, err := rootMap(roots)
	if err != nil {
		return stats, err
	}
	var gz *parallelGzipWriter
	var raw *bufio.Writer
	if opts.Compression == "gzip" {
		gz = newParallelGzipWriter(ctx, w, opts.CompressionConcurrency)
		w = gz
	} else {
		// Coalesce tar headers, padding and small file writes in raw archives.
		// Gzip already buffers its output in fixed members.
		raw = bufio.NewWriterSize(w, 256*1024)
		w = raw
	}
	tw := tar.NewWriter(&boundedWriter{ctx: ctx, w: w, left: opts.MaxBytes})
	defer func() {
		err = errors.Join(err, tw.Close())
		if gz != nil {
			err = errors.Join(err, gz.Close())
		}
		if raw != nil {
			if cancelErr := ctx.Err(); cancelErr != nil {
				err = errors.Join(err, cancelErr)
			} else {
				err = errors.Join(err, raw.Flush())
			}
		}
	}()
	for _, r := range ordered {
		if err := noSymlinkPath(r.Path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return stats, err
		}
		root, err := os.OpenRoot(r.Path)
		if err != nil {
			return stats, err
		}
		seen := make(map[string]bool)
		err = fs.WalkDir(root.FS(), ".", func(name string, e fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			archiveName := r.Name
			if name != "." {
				if err := portable(name); err != nil {
					return err
				}
				archiveName += "/" + name
			}
			key := caseKey(archiveName)
			if seen[key] {
				return fmt.Errorf("case-colliding path %q", archiveName)
			}
			seen[key] = true
			info, err := e.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported cache entry %q", archiveName)
			}
			if info.Mode().IsRegular() && r.Name == "build" && !opts.TrimBefore.IsZero() && buildObject(name) && info.ModTime().Before(opts.TrimBefore) {
				return nil
			}
			h, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			h.Name, h.Uid, h.Gid, h.Uname, h.Gname = archiveName, 0, 0, "", ""
			h.AccessTime, h.ChangeTime = time.Time{}, time.Time{}
			h.Mode &= 0777
			h.Format = tar.FormatPAX
			if info.IsDir() {
				h.ModTime = time.Unix(0, 0)
				if name == "." {
					h.Mode = 0755
				}
			}
			if err := tw.WriteHeader(h); err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			f, err := root.Open(name)
			if err != nil {
				return err
			}
			opened, statErr := f.Stat()
			if statErr != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
				f.Close()
				return fmt.Errorf("cache file changed while opening %q", name)
			}
			n, copyErr := io.Copy(tw, &contextReader{ctx: ctx, r: f})
			closeErr := f.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return err
			}
			stats.Files++
			stats.Bytes += n
			return nil
		})
		closeErr := root.Close()
		if err := errors.Join(err, closeErr); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

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
		entries = append(entries, entry{name, *h})
	}
	// Drain for gzip checksum verification; only tar zero padding is permitted.
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
	err = merge(ctx, stages, entries, byName, opts.RestoreConcurrency, &stats)
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
			if stage, err := os.MkdirTemp(parent, ".setup-go-better-cache-*"); err == nil {
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
	return os.MkdirTemp("", "setup-go-better-cache-*")
}

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
			if dir, e := os.MkdirTemp(roots[parts[0]].Path, ".setup-go-better-publish-*"); e == nil {
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
func portable(name string) error {
	if name == "" || name == "." || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:\x00") {
		return fmt.Errorf("unsafe cache path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, "<>\"|?*") {
			return fmt.Errorf("nonportable cache path %q", name)
		}
		for _, c := range part {
			if c < 32 {
				return fmt.Errorf("control character in cache path %q", name)
			}
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		reserved := base == "CON" || base == "PRN" || base == "AUX" || base == "NUL"
		for _, prefix := range []string{"COM", "LPT"} {
			if strings.HasPrefix(base, prefix) {
				suffix := strings.TrimPrefix(base, prefix)
				if len([]rune(suffix)) == 1 && strings.Contains("123456789¹²³", suffix) {
					reserved = true
				}
			}
		}
		if reserved {
			return fmt.Errorf("reserved cache path %q", name)
		}
	}
	return nil
}
func noSymlinkPath(name string) error {
	abs, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs[len(volume):], string(filepath.Separator))
	current := volume + string(filepath.Separator)
	parts := strings.Split(rest, string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return fmt.Errorf("unsafe cache destination component %q", current)
		}
	}
	return nil
}
func buildObject(name string) bool {
	p := strings.Split(name, "/")
	if len(p) != 2 || len(p[0]) != 2 || len(p[1]) != 66 || p[1][:2] != p[0] || (p[1][64:] != "-a" && p[1][64:] != "-d") {
		return false
	}
	for _, c := range p[1][:64] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type boundedWriter struct {
	ctx  context.Context
	w    io.Writer
	left int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.left {
		return 0, errors.New("archive exceeds uncompressed size limit")
	}
	n, err := w.w.Write(p)
	w.left -= int64(n)
	return n, err
}

type boundedReader struct {
	r    io.Reader
	left int64
	read int64
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if r.left < 0 {
		return 0, errors.New("archive exceeds uncompressed size limit")
	}
	if int64(len(p)) > r.left {
		p = p[:r.left+1]
	}
	n, err := r.r.Read(p)
	r.left -= int64(n)
	r.read += int64(n)
	if r.left < 0 {
		return n, errors.New("archive exceeds uncompressed size limit")
	}
	return n, err
}

func caseKey(s string) string {
	return strings.Map(func(r rune) rune {
		lowest := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < lowest {
				lowest = next
			}
		}
		return lowest
	}, s)
}
