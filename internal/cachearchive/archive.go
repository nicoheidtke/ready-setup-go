// Package cachearchive creates portable snapshots and validates complete downloads before merging.
package cachearchive

import (
	"archive/tar"
	"compress/gzip"
	"context"
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
	// TrimBefore filters old canonical Go build objects only in the "build" root.
	// It never changes local files or trims modules. Zero disables trimming.
	TrimBefore time.Time
	// MaxBytes bounds the uncompressed tar, including headers. Zero means 20 GiB.
	MaxBytes int64
}

// Stats counts regular-file payload in the archive, including existing files skipped during merge.
type Stats struct{ Files, Bytes int64 }

func options(o Options) (Options, error) {
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
	var gz *gzip.Writer
	if opts.Compression == "gzip" {
		gz = gzip.NewWriter(w)
		w = gz
	}
	tw := tar.NewWriter(&boundedWriter{ctx: ctx, w: w, left: opts.MaxBytes})
	defer func() {
		err = errors.Join(err, tw.Close())
		if gz != nil {
			err = errors.Join(err, gz.Close())
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
	stage, err := os.MkdirTemp("", "setup-go-better-cache-*")
	if err != nil {
		return stats, err
	}
	defer os.RemoveAll(stage)
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
		target := filepath.Join(stage, filepath.FromSlash(name))
		if h.Typeflag == tar.TypeDir {
			err = os.MkdirAll(target, 0700)
		} else {
			err = os.MkdirAll(filepath.Dir(target), 0700)
			if err == nil {
				var f *os.File
				f, err = os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err == nil {
					var n int64
					n, err = io.Copy(f, tr)
					err = errors.Join(err, f.Close())
					stats.Files++
					stats.Bytes += n
				}
			}
		}
		if err != nil {
			return stats, err
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
	// Preflight every explicit destination and implicit ancestor before publication.
	for _, e := range entries {
		parts := strings.SplitN(e.name, "/", 2)
		target := byName[parts[0]].Path
		if len(parts) == 2 {
			target = filepath.Join(target, filepath.FromSlash(parts[1]))
		}
		if err := noSymlinkPath(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return stats, err
		}
		info, err := os.Lstat(target)
		if err == nil && (info.IsDir() != (e.hdr.Typeflag == tar.TypeDir) || (!info.IsDir() && !info.Mode().IsRegular())) {
			return stats, fmt.Errorf("incompatible destination %q", target)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return stats, err
		}
	}
	return stats, merge(ctx, stage, entries, byName)
}

type directory struct {
	root  *os.Root
	name  string
	mode  fs.FileMode
	mtime time.Time
}

func merge(ctx context.Context, stage string, entries []entry, roots map[string]Root) (err error) {
	opened := make(map[string]*os.Root)
	var modes []directory
	// Temporarily writable existing module directories always regain their modes,
	// including after cancellation or failure.
	defer func() {
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
	for _, e := range entries {
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
		}
		name := "."
		if len(parts) == 2 {
			name = filepath.FromSlash(parts[1])
		}
		if e.hdr.Typeflag == tar.TypeDir {
			if _, err := r.Lstat(name); errors.Is(err, os.ErrNotExist) {
				if err := writableDirs(r, name, &modes); err != nil {
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
		if err := writableDirs(r, filepath.Dir(name), &modes); err != nil {
			return err
		}
		out, err := r.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		in, err := os.Open(filepath.Join(stage, filepath.FromSlash(e.name)))
		if err == nil {
			_, err = io.Copy(out, &contextReader{ctx: ctx, r: in})
			err = errors.Join(err, in.Close())
		}
		if err == nil {
			err = out.Chmod(fs.FileMode(e.hdr.Mode))
		}
		err = errors.Join(err, out.Close())
		if err == nil {
			err = r.Chtimes(name, e.hdr.ModTime, e.hdr.ModTime)
		}
		if err != nil {
			r.Chmod(name, 0600)
			r.Remove(name)
			return err
		}
	}
	return nil
}
func writableDirs(r *os.Root, name string, modes *[]directory) error {
	names := []string{"."}
	if name != "." {
		current := ""
		for _, part := range strings.Split(name, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			names = append(names, current)
		}
	}
	for _, name := range names {
		info, err := r.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			if err := r.Mkdir(name, 0755); err != nil {
				return err
			}
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
