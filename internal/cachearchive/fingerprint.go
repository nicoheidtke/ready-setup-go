package cachearchive

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
)

type contentEntry struct {
	name   string
	mode   int64
	dir    bool
	digest [sha256.Size]byte
}

// contentIdentity deliberately excludes mtimes: reading an unchanged module
// cache can update bookkeeping timestamps without changing reusable contents.
// Entries are sorted independently of tar order, and length framing prevents
// ambiguous names or byte concatenations from sharing an identity.
type contentIdentity struct{ entries []contentEntry }

func newContentIdentity() *contentIdentity { return &contentIdentity{} }

func (c *contentIdentity) add(name string, mode int64, dir bool, digest []byte) {
	if dir && !strings.Contains(name, "/") {
		mode = 0755 // Pack normalizes the root mode.
	}
	e := contentEntry{name: name, mode: mode & 0777, dir: dir}
	copy(e.digest[:], digest)
	c.entries = append(c.entries, e)
}

func (c *contentIdentity) sum() string {
	sort.Slice(c.entries, func(i, j int) bool { return c.entries[i].name < c.entries[j].name })
	h := sha256.New()
	h.Write([]byte("setup-go-better-content-v1\x00"))
	var buf [8]byte
	for _, e := range c.entries {
		binary.LittleEndian.PutUint64(buf[:], uint64(len(e.name)))
		h.Write(buf[:])
		h.Write([]byte(e.name))
		binary.LittleEndian.PutUint64(buf[:], uint64(e.mode))
		h.Write(buf[:])
		if e.dir {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
			h.Write(e.digest[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Fingerprint hashes a tree's paths, entry types, modes and every regular-file
// byte, without creating or compressing an archive. It never trusts size/mtime
// alone, so same-size mutations with preserved timestamps are detected. The
// maximum bounds regular-file bytes read; Pack still enforces tar overhead.
func Fingerprint(ctx context.Context, roots []Root, maxBytes int64) (stats Stats, err error) {
	opts, err := options(Options{MaxBytes: maxBytes})
	if err != nil {
		return stats, err
	}
	ordered, _, err := rootMap(roots)
	if err != nil {
		return stats, err
	}
	identity := newContentIdentity()
	buf := make([]byte, 32*1024)
	for _, r := range ordered {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
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
			if info.IsDir() {
				identity.add(archiveName, int64(info.Mode().Perm()), true, nil)
				return nil
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported cache entry %q", archiveName)
			}
			if info.Size() > opts.MaxBytes-stats.Bytes {
				return errors.New("fingerprint payload exceeds size limit")
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
			h := sha256.New()
			remaining := opts.MaxBytes - stats.Bytes
			n, copyErr := io.CopyBuffer(h, io.LimitReader(&contextReader{ctx: ctx, r: f}, remaining+1), buf)
			after, statErr := f.Stat()
			closeErr := f.Close()
			if err := errors.Join(copyErr, statErr, closeErr); err != nil {
				return err
			}
			if n > remaining {
				return errors.New("fingerprint payload exceeds size limit")
			}
			if n != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) || after.Mode() != info.Mode() {
				return fmt.Errorf("cache file changed while hashing %q", name)
			}
			identity.add(archiveName, int64(info.Mode().Perm()), false, h.Sum(nil))
			stats.Files++
			stats.Bytes += n
			return nil
		})
		if err := errors.Join(err, root.Close()); err != nil {
			return stats, err
		}
	}
	stats.ContentDigest = identity.sum()
	return stats, nil
}
