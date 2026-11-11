// Package distribution records and verifies release preparation artifacts.
// Local integrity checks are not signatures or hosted attestations.
package distribution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var Platforms = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"}

type Provenance struct {
	Revision     string   `json:"revision"`
	Dirty        bool     `json:"dirty"`
	SourceSHA256 string   `json:"source_tree_sha256"`
	Compiler     string   `json:"compiler"`
	Platforms    []string `json:"platforms"`
}

func git(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read Git source metadata: %w", err)
	}
	return b, nil
}

// Source hashes sorted nonignored working-tree files (including local changes
// and untracked files), not a claimed Git tree object. Call from the repo root.
// The digest excludes ignored build outputs and includes paths/deletion markers.
func Source(root string, platforms []string, requireClean bool) (Provenance, error) {
	var p Provenance
	rev, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return p, err
	}
	p.Revision = strings.TrimSpace(string(rev))
	status, err := git(root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return p, err
	}
	p.Dirty = len(status) != 0
	if requireClean && p.Dirty {
		return p, fmt.Errorf("release preparation requires a clean source checkout")
	}
	files, err := git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return p, err
	}
	names := strings.Split(strings.TrimSuffix(string(files), "\x00"), "\x00")
	sort.Strings(names)
	h := sha256.New()
	previous := ""
	for _, name := range names {
		if name == "" || name == previous {
			continue
		}
		previous = name
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return p, fmt.Errorf("invalid source path")
		}
		st, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if os.IsNotExist(err) {
			fmt.Fprintf(h, "deleted\x00%s\x00", name)
			continue
		}
		if err != nil {
			return p, err
		}
		if !st.Mode().IsRegular() {
			return p, fmt.Errorf("nonregular source input %s", name)
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return p, err
		}
		sum := sha256.Sum256(b)
		fmt.Fprintf(h, "file\x00%s\x00%x\x00", name, sum)
	}
	p.SourceSHA256 = hex.EncodeToString(h.Sum(nil))
	cmd := exec.Command("go", "version")
	compiler, err := cmd.Output()
	if err != nil {
		return p, fmt.Errorf("read build compiler: %w", err)
	}
	p.Compiler = strings.TrimSpace(string(compiler))
	p.Platforms = append([]string(nil), platforms...)
	return p, nil
}

func (p Provenance) Marker() string {
	if p.Dirty {
		return p.Revision + "-dirty\n"
	}
	return p.Revision + "\n"
}

func WriteProvenance(root string, p Provenance) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(root, "SOURCE_PROVENANCE.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "SOURCE_REVISION"), []byte(p.Marker()), 0644)
}

// WriteChecksums covers every regular file except the checksum file itself.
func WriteChecksums(root string) error {
	files, err := regularFiles(root)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	for _, name := range files {
		if name == "SHA256SUMS" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%x  %s\n", sum, name)
	}
	return os.WriteFile(filepath.Join(root, "SHA256SUMS"), b.Bytes(), 0644)
}

func regularFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(name string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("nonregular distribution entry %s", name)
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}
