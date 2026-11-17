package installer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"ready-setup-go/internal/actionlog"
)

// Official Go archives need only directories and regular files. Reject links,
// duplicate files, Windows device/ADS paths and traversal before any write.
func archivePath(root, name string) (string, error) {
	name = strings.TrimSuffix(name, "/")
	if name == "go" {
		return root, nil
	}
	if !strings.HasPrefix(name, "go/") || strings.ContainsAny(name, "\\:\x00") || path.Clean(name) != name {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	rel := strings.TrimPrefix(name, "go/")
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", fmt.Errorf("unsafe archive path %q", name)
		}
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}

// These internal safety ceilings deliberately exceed normal official releases.
// Tar counts all expanded bytes, including headers, padding and extra members;
// ZIP counts expanded file payloads. Neither is a filesystem allocation quota.
type extractionLimits struct {
	bytes   int64
	entries int
}

var toolchainLimits = extractionLimits{bytes: 4 << 30, entries: 100000}

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

type expansionReader struct {
	r    io.Reader
	left int64
}

func (r *expansionReader) Read(p []byte) (int, error) {
	// Probe one byte past the ceiling so exact-limit EOF still succeeds.
	if int64(len(p)) > r.left+1 {
		p = p[:r.left+1]
	}
	n, err := r.r.Read(p)
	if int64(n) > r.left {
		return 0, errors.New("toolchain expansion exceeds size limit")
	}
	r.left -= int64(n)
	return n, err
}

func writeEntry(ctx context.Context, root, name string, mode os.FileMode, reader io.Reader) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	dest, err := archivePath(root, name)
	if err != nil {
		return 0, err
	}
	if mode.IsDir() {
		return 0, os.MkdirAll(dest, 0755)
	}
	if !mode.IsRegular() || dest == root {
		return 0, fmt.Errorf("unsupported archive entry %q", name)
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return 0, err
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm()&0777)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, &contextReader{ctx: ctx, r: reader})
	closeErr := f.Close()
	return n, errors.Join(err, closeErr)
}
func untar(ctx context.Context, root string, r io.Reader, limits extractionLimits) (int64, error) {
	gz, err := gzip.NewReader(&contextReader{ctx: ctx, r: r})
	if err != nil {
		return 0, err
	}
	defer gz.Close()
	expanded := &expansionReader{r: &contextReader{ctx: ctx, r: gz}, left: limits.bytes}
	tr := tar.NewReader(expanded)
	var bytes int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return bytes, err
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return bytes, err
		}
		entries++
		if entries > limits.entries {
			return bytes, errors.New("toolchain exceeds entry limit")
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return bytes, fmt.Errorf("unsupported tar entry %q", h.Name)
		}
		if h.Size < 0 || h.Size > expanded.left {
			return bytes, errors.New("toolchain expansion exceeds size limit")
		}
		n, err := writeEntry(ctx, root, h.Name, h.FileInfo().Mode(), tr)
		bytes += n
		if err != nil {
			return bytes, err
		}
	}
	// Consume the gzip trailer and any remaining members to validate transport CRC.
	_, err = io.Copy(io.Discard, expanded)
	return bytes, err
}
func unzip(ctx context.Context, root, file string, limits extractionLimits) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	z, err := zip.OpenReader(file)
	if err != nil {
		return 0, err
	}
	defer z.Close()
	if len(z.File) > limits.entries {
		return 0, errors.New("toolchain exceeds entry limit")
	}
	var bytes int64
	for _, f := range z.File {
		if err := ctx.Err(); err != nil {
			return bytes, err
		}
		if f.UncompressedSize64 > uint64(limits.bytes-bytes) {
			return bytes, errors.New("toolchain expansion exceeds size limit")
		}
		r, err := f.Open()
		if err != nil {
			return bytes, err
		}
		n, err := writeEntry(ctx, root, f.Name, f.Mode(), &expansionReader{r: r, left: limits.bytes - bytes})
		closeErr := r.Close()
		bytes += n
		if err = errors.Join(err, closeErr); err != nil {
			return bytes, err
		}
	}
	return bytes, nil
}

type measuredReader struct {
	r        io.Reader
	bytes    int64
	duration time.Duration
	progress *actionlog.Progress
}

func (r *measuredReader) Read(p []byte) (int, error) {
	t := time.Now()
	n, e := r.r.Read(p)
	r.duration += time.Since(t)
	r.bytes += int64(n)
	r.progress.Add(int64(n))
	return n, e
}

func (c Config) install(ctx context.Context, v Version, a archive, m *Metrics) (result string, err error) {
	return c.installWithLimits(ctx, v, a, m, toolchainLimits)
}

func (c Config) installWithLimits(ctx context.Context, v Version, a archive, m *Metrics, limits extractionLimits) (result string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	want, err := hex.DecodeString(a.SHA256)
	if err != nil || len(want) != sha256.Size {
		return "", fmt.Errorf("invalid official SHA-256")
	}
	if path.Base(a.Filename) != a.Filename || strings.ContainsAny(a.Filename, "\\:\x00") || a.Size <= 0 {
		return "", fmt.Errorf("invalid archive metadata")
	}
	parent := filepath.Join(c.InstallDir, v.String())
	if err = os.MkdirAll(parent, 0755); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, ".staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stage)
	root := filepath.Join(stage, "go")
	if err = os.Mkdir(root, 0755); err != nil {
		return "", err
	}
	start := time.Now()
	resp, err := c.get(ctx, c.DownloadURL+a.Filename)
	m.DownloadMS += elapsed(start)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	progress := actionlog.Default.Progress("Download/extract Go toolchain", "bytes", a.Size)
	defer func() { progress.Finish(err) }()
	mr := &measuredReader{r: &contextReader{ctx: ctx, r: io.LimitReader(resp.Body, a.Size+1)}, progress: progress}
	hash := sha256.New()
	body := io.TeeReader(mr, hash)
	extractStart := time.Now()
	var extracted, spool int64
	if strings.HasSuffix(a.Filename, ".tar.gz") {
		extracted, err = untar(ctx, root, body, limits)
		if err == nil {
			_, err = io.Copy(io.Discard, body)
		}
	} else if strings.HasSuffix(a.Filename, ".zip") {
		file := filepath.Join(stage, "archive.zip")
		var f *os.File
		f, err = os.Create(file)
		if err == nil {
			spool, err = io.Copy(f, body)
			err = errors.Join(err, f.Close())
		}
		if err == nil && (mr.bytes != a.Size || hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(a.SHA256)) {
			err = fmt.Errorf("archive checksum or size mismatch")
		}
		if err == nil {
			extracted, err = unzip(ctx, root, file, limits)
		}
	} else {
		err = fmt.Errorf("unsupported archive format %q", a.Filename)
	}
	m.ArchiveBytes = mr.bytes
	m.PeakTemporaryBytes = extracted + spool
	m.DownloadMS += float64(mr.duration) / float64(time.Millisecond)
	m.ExtractionMS += float64(time.Since(extractStart)-mr.duration) / float64(time.Millisecond)
	if err != nil {
		return "", fmt.Errorf("download/extract: %w", err)
	}
	if mr.bytes != a.Size || hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(a.SHA256) {
		return "", fmt.Errorf("archive checksum or size mismatch")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !complete(root, v, c.OS) {
		return "", fmt.Errorf("archive lacks expected Go installation")
	}
	if err = os.WriteFile(filepath.Join(root, ".complete"), []byte(v.String()+"\n"), 0644); err != nil {
		return "", err
	}
	m.PeakTemporaryBytes += int64(len(v.String()) + 1)
	dest := filepath.Join(parent, c.OS+"-"+c.Arch)
	// Never replace an existing installation. A concurrent successful install wins.
	if _, err = os.Lstat(dest); err == nil {
		if marker, e := os.Stat(filepath.Join(dest, ".complete")); e == nil && marker.Mode().IsRegular() && complete(dest, v, c.OS) {
			return dest, nil
		}
		return "", fmt.Errorf("incomplete installation at %s; remove it before retrying", dest)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(root, dest); err != nil {
		if _, e := os.Stat(filepath.Join(dest, ".complete")); e == nil && complete(dest, v, c.OS) {
			return dest, nil
		}
		return "", err
	}
	return dest, nil
}
