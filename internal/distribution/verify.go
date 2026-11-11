package distribution

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

func Binary(platform string) string {
	name := "bin/" + strings.ReplaceAll(platform, "/", "-") + "/setup-go-better"
	if strings.HasPrefix(platform, "windows/") {
		name += ".exe"
	}
	return name
}

func Asset(platform string) string {
	name := "setup-go-better-" + strings.ReplaceAll(platform, "/", "-")
	if strings.HasPrefix(platform, "windows/") {
		name += ".exe"
	}
	return name
}

func checksums(root string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(root, "SHA256SUMS"))
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		sum, name, ok := strings.Cut(line, "  ")
		decoded, err := hex.DecodeString(sum)
		if !ok || err != nil || len(decoded) != 32 || sum != strings.ToLower(sum) || name == "SHA256SUMS" || !filepath.IsLocal(filepath.FromSlash(name)) || path.Clean(name) != name || strings.ContainsAny(name, "\\\r\n\x00") {
			return nil, fmt.Errorf("invalid checksum entry")
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate checksum entry %s", name)
		}
		result[name] = sum
	}
	files, err := regularFiles(root)
	if err != nil {
		return nil, err
	}
	if len(files) != len(result)+1 {
		return nil, fmt.Errorf("checksum manifest does not cover every file")
	}
	for _, name := range files {
		if name == "SHA256SUMS" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		if result[name] != hex.EncodeToString(sum[:]) {
			return nil, fmt.Errorf("checksum mismatch: %s", name)
		}
	}
	return result, nil
}

// Verify checks local integrity and packaging consistency. Without AllowDirty,
// a release candidate must identify a clean source checkout. This cannot prove
// origin: consumers must authenticate published checksums/attestations separately.
func Verify(root, archives string, allowDirty bool, platform string) error {
	manifest, err := checksums(root)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, "SOURCE_PROVENANCE.json"))
	if err != nil {
		return err
	}
	var p Provenance
	if err = json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("invalid source provenance")
	}
	rev, err := hex.DecodeString(p.Revision)
	if err != nil || len(rev) != 20 || len(p.Revision) != 40 || strings.ToLower(p.Revision) != p.Revision {
		return fmt.Errorf("invalid source revision")
	}
	digest, err := hex.DecodeString(p.SourceSHA256)
	if err != nil || len(digest) != 32 || !strings.HasPrefix(p.Compiler, "go version go1.") {
		return fmt.Errorf("invalid source metadata")
	}
	if p.Dirty && !allowDirty {
		return fmt.Errorf("dirty source build is not a release candidate (use --allow-dirty for local diagnostics)")
	}
	marker, err := os.ReadFile(filepath.Join(root, "SOURCE_REVISION"))
	if err != nil || string(marker) != p.Marker() {
		return fmt.Errorf("source marker does not match provenance")
	}
	platforms := Platforms
	if platform != "" {
		found := false
		for _, candidate := range Platforms {
			found = found || platform == candidate
		}
		if !found {
			return fmt.Errorf("unsupported platform %q", platform)
		}
		platforms = []string{platform}
	}
	if strings.Join(p.Platforms, ",") != strings.Join(platforms, ",") {
		return fmt.Errorf("provenance platform set does not match requested distribution")
	}
	for _, required := range []string{"action.yml", "launch.cjs", "post.cjs", "launcher.cjs", "launch.sh", "experimental/node/action.yml", "experimental/node/launch.cjs", "experimental/node/post.cjs", "experimental/composite/action.yml", "README.md", "SOURCE_REVISION", "SOURCE_PROVENANCE.json"} {
		if _, exists := manifest[required]; !exists {
			return fmt.Errorf("missing required distribution file %s", required)
		}
	}
	for _, target := range platforms {
		name := Binary(target)
		if _, exists := manifest[name]; !exists {
			return fmt.Errorf("missing binary %s", name)
		}
		if runtime.GOOS != "windows" && !strings.HasPrefix(target, "windows/") {
			st, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
			if err != nil || st.Mode().Perm()&0111 != 0111 {
				return fmt.Errorf("missing executable mode on %s", name)
			}
		}
	}
	for name := range manifest {
		if strings.HasPrefix(name, "bin/") {
			found := false
			for _, target := range platforms {
				found = found || name == Binary(target)
			}
			if !found {
				return fmt.Errorf("unexpected binary %s", name)
			}
		}
	}
	if archives == "" {
		return nil
	}
	assets, err := checksums(archives)
	if err != nil {
		return err
	}
	if len(assets) != len(platforms)+2 {
		return fmt.Errorf("unexpected release asset set")
	}
	for _, target := range platforms {
		name := Asset(target)
		if assets[name] == "" || assets[name] != manifest[Binary(target)] {
			return fmt.Errorf("standalone asset differs from bundled binary: %s", name)
		}
	}
	for _, variant := range []string{"bundled", "launcher"} {
		if assets[variant+".zip"] == "" {
			return fmt.Errorf("missing %s ZIP", variant)
		}
		if err := verifyZIP(root, filepath.Join(archives, variant+".zip"), variant == "launcher"); err != nil {
			return err
		}
	}
	return nil
}

func verifyZIP(root, name string, launcher bool) error {
	z, err := zip.OpenReader(name)
	if err != nil {
		return err
	}
	defer z.Close()
	files, err := regularFiles(root)
	if err != nil {
		return err
	}
	expected := make(map[string]bool)
	for _, file := range files {
		if !launcher || !strings.HasPrefix(file, "bin/") {
			expected[file] = true
		}
	}
	seen := make(map[string]bool)
	for _, file := range z.File {
		if !expected[file.Name] || seen[file.Name] || !file.Mode().IsRegular() {
			return fmt.Errorf("unexpected/duplicate ZIP entry %s", file.Name)
		}
		seen[file.Name] = true
		mode := os.FileMode(0644)
		if strings.HasPrefix(file.Name, "bin/") && !strings.HasSuffix(file.Name, ".exe") {
			mode = 0755
		}
		if file.Mode().Perm() != mode {
			return fmt.Errorf("invalid ZIP executable mode: %s", file.Name)
		}
		original, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.Name)))
		if err != nil {
			return err
		}
		if file.UncompressedSize64 != uint64(len(original)) {
			return fmt.Errorf("ZIP content size differs: %s", file.Name)
		}
		r, err := file.Open()
		if err != nil {
			return err
		}
		b, readErr := io.ReadAll(io.LimitReader(r, int64(len(original))+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(original, b) {
			return fmt.Errorf("ZIP bytes differ: %s", file.Name)
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("ZIP is missing distribution entries")
	}
	return nil
}
