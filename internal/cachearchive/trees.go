package cachearchive

import (
	"archive/tar"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// publishMissingTrees runs only after complete stream validation and destination
// preflight. The mover must atomically refuse every existing destination,
// including empty directories. Ordinary os.Rename does not provide that contract.
func publishMissingTrees(ctx context.Context, stages map[string]string, entries []entry, roots map[string]Root, checked map[string]fs.FileInfo, move func(string, string) error, stats *Stats) ([]entry, error) {
	type tree struct {
		name, source, target string
		hdr                  *tar.Header
		stagedMetadata       bool
		children             []*tree
		files                int64
	}
	nodes := make(map[string]*tree)
	var forest []*tree
	for i := range entries {
		e := &entries[i]
		parts := strings.Split(e.name, "/")
		var parent *tree
		for j := range parts {
			name := strings.Join(parts[:j+1], "/")
			n := nodes[name]
			if n == nil {
				n = &tree{name: name, source: filepath.Join(stages[parts[0]], "payload"), target: roots[parts[0]].Path}
				if parent == nil {
					forest = append(forest, n)
				} else {
					n.source = filepath.Join(parent.source, parts[j])
					n.target = filepath.Join(parent.target, parts[j])
					parent.children = append(parent.children, n)
				}
				nodes[name] = n
			}
			if e.hdr.Typeflag == tar.TypeReg {
				n.files++
			}
			parent = n
		}
		parent.hdr = &e.hdr
		parent.stagedMetadata = e.stagedMetadata
	}
	// Finalize directories after children so read-only modes and archive mtimes
	// survive publication, including archives listing parents after their files.
	var metadata func(*tree, bool) error
	metadata = func(n *tree, reset bool) error {
		if err := ctx.Err(); err != nil && !reset {
			return err
		}
		if reset {
			err := os.Chmod(n.source, 0700)
			for _, child := range n.children {
				err = errors.Join(err, metadata(child, true))
			}
			return err
		}
		if n.stagedMetadata {
			return nil
		}
		for _, child := range n.children {
			if err := metadata(child, false); err != nil {
				return err
			}
		}
		mode := fs.FileMode(0755)
		if n.hdr != nil {
			mode = fs.FileMode(n.hdr.Mode)
			if err := os.Chtimes(n.source, n.hdr.ModTime, n.hdr.ModTime); err != nil {
				return err
			}
		}
		return os.Chmod(n.source, mode)
	}
	moved := make(map[string]bool)
	var mark func(*tree)
	mark = func(n *tree) {
		moved[n.name] = true
		for _, child := range n.children {
			mark(child)
		}
	}
	var visit func(*tree) error
	visit = func(n *tree) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if n.hdr != nil && n.hdr.Typeflag == tar.TypeReg {
			return nil
		}
		info, err := preflightPath(n.target, checked)
		if err != nil {
			return err
		}
		if info == nil {
			// Parents were checked before any publication. Recheck after creation,
			// because this path-based Windows primitive cannot use os.Root handles.
			// Hostile concurrent filesystem writers remain outside our trust model.
			parent := filepath.Dir(n.target)
			if err := os.MkdirAll(parent, 0755); err != nil {
				return err
			}
			if err := noSymlinkPath(parent); err != nil {
				return err
			}
			if err := metadata(n, false); err != nil {
				return errors.Join(err, metadata(n, true))
			}
			if err := ctx.Err(); err != nil {
				return errors.Join(err, metadata(n, true))
			}
			if err := move(n.source, n.target); err == nil {
				mark(n)
				stats.PublishedFiles += n.files
				return nil
			}
			// A late destination, cross-volume staging, or unsupported filesystem
			// retains the original exclusive-create/hard-link merge. Restore writable
			// staging first: unlinking read-only files fails on Windows.
			return metadata(n, true)
		}
		for _, child := range n.children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, n := range forest {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	if len(moved) == 0 {
		return entries, nil
	}
	remaining := make([]entry, 0, len(entries))
	for _, e := range entries {
		if !moved[e.name] {
			remaining = append(remaining, e)
		}
	}
	return remaining, nil
}
