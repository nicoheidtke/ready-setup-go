package cachearchive

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Pack is deterministic for unchanged contents, modes, and mtimes. Missing roots
// are skipped; symbolic links and special files are rejected.
func Pack(ctx context.Context, w io.Writer, roots []Root, opts Options) (stats Stats, err error) {
	return pack(ctx, w, roots, opts, defaultMetadataLimits)
}

func pack(ctx context.Context, w io.Writer, roots []Root, opts Options, limits metadataLimits) (stats Stats, err error) {
	opts, err = options(opts)
	if err != nil {
		return
	}
	ordered, _, err := rootMap(roots)
	if err != nil {
		return stats, err
	}
	budget := metadataBudget{limits: limits}
	var identity *contentIdentity
	if opts.ContentIdentity {
		identity = newContentIdentity()
	}
	var gz *parallelGzipWriter
	var raw *bufio.Writer
	var compressed io.WriteCloser
	if opts.Compression == "gzip" {
		gz = newParallelGzipWriter(ctx, w, opts.CompressionConcurrency, opts.GzipLevel)
		w = gz
	} else if opts.Compression == "zstd" {
		compressed, err = NewZstdWriter(&contextWriter{ctx: ctx, w: w}, opts.CompressionConcurrency)
		if err != nil {
			return stats, err
		}
		w = compressed
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
		if compressed != nil {
			err = errors.Join(err, compressed.Close(), ctx.Err())
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
				archiveName += "/" + name
			}
			if err := budget.add(archiveName); err != nil {
				return err
			}
			if name != "." {
				if err := portable(name); err != nil {
					return err
				}
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
				if identity != nil {
					identity.add(archiveName, h.Mode, true, nil)
				}
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
			var target io.Writer = tw
			var contentHash hash.Hash
			if identity != nil {
				contentHash = sha256.New()
				target = io.MultiWriter(tw, contentHash)
			}
			n, copyErr := io.Copy(target, &contextReader{ctx: ctx, r: f})
			var after os.FileInfo
			if identity != nil && copyErr == nil {
				after, copyErr = f.Stat()
				if copyErr == nil && (n != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || after.Mode() != info.Mode()) {
					copyErr = fmt.Errorf("cache file changed while hashing %q", name)
				}
			}
			closeErr := f.Close()
			if err := errors.Join(copyErr, closeErr); err != nil {
				return err
			}
			stats.Files++
			stats.Bytes += n
			if identity != nil {
				identity.add(archiveName, h.Mode, false, contentHash.Sum(nil))
			}
			return nil
		})
		closeErr := root.Close()
		if err := errors.Join(err, closeErr); err != nil {
			return stats, err
		}
	}
	if identity != nil {
		stats.ContentDigest = identity.sum()
	}
	return stats, nil
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
