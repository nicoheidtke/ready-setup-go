// Package cache manages optional immutable Go cache snapshots.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"setup-go-better/internal/cachearchive"
)

const Format = "sgb-cache-v1"
const DefaultMaxBytes int64 = 10 << 30

type Config struct {
	Mode                   string        `json:"mode"`
	Layout                 string        `json:"layout"`
	Compression            string        `json:"compression"`
	CompressionConcurrency int           `json:"compression_concurrency"`
	RestoreConcurrency     int           `json:"restore_concurrency"`
	ChunkConcurrency       int           `json:"chunk_concurrency"`
	Storage                string        `json:"storage"`
	Overlap                bool          `json:"overlap"`
	Scope                  string        `json:"scope"`
	DependencyPaths        string        `json:"dependency_paths"`
	BuildMaxAge            time.Duration `json:"build_max_age"`
	MaxBytes               int64         `json:"max_bytes"`
	UploadConcurrency      int           `json:"upload_concurrency"`
	UploadChunkBytes       int64         `json:"upload_chunk_bytes"`
	DownloadConcurrency    int           `json:"download_concurrency"`
	DownloadChunkBytes     int64         `json:"download_chunk_bytes"`
	Strict                 bool          `json:"strict"`
	SaveOnFailure          bool          `json:"save_on_failure"`
	Report                 string        `json:"report,omitempty"`
	DisableSummary         bool          `json:"disable_summary,omitempty"`
}

func DefaultConfig() Config {
	return Config{Mode: "off", Layout: "split", Compression: "gzip", CompressionConcurrency: 4, RestoreConcurrency: 4, Storage: "archive", ChunkConcurrency: 4, Overlap: true, MaxBytes: DefaultMaxBytes, UploadConcurrency: 4, UploadChunkBytes: 32 << 20, DownloadConcurrency: 4, DownloadChunkBytes: 32 << 20}
}

func (c Config) Validate() error {
	if c.CompressionConcurrency < 1 || c.CompressionConcurrency > 16 {
		return fmt.Errorf("cache-compression-concurrency must be 1..16")
	}
	if c.RestoreConcurrency < 1 || c.RestoreConcurrency > 16 {
		return fmt.Errorf("cache-restore-concurrency must be 1..16")
	}
	if c.DownloadConcurrency < 1 || c.DownloadConcurrency > 32 || c.DownloadChunkBytes < 1 || c.DownloadChunkBytes > 4000<<20 {
		return fmt.Errorf("cache download concurrency must be 1..32 and chunk bytes 1..4194304000")
	}
	if c.ChunkConcurrency < 1 || c.ChunkConcurrency > 16 {
		return fmt.Errorf("cache-chunk-concurrency must be 1..16")
	}
	if c.Storage != "" && c.Storage != "archive" && c.Storage != "chunked" {
		return fmt.Errorf("cache-storage must be archive or chunked")
	}
	if c.Mode != "off" && c.Mode != "restore" && c.Mode != "restore-save" {
		return fmt.Errorf("cache must be off, restore, or restore-save")
	}
	if c.Layout != "split" && c.Layout != "combined" {
		return fmt.Errorf("cache-layout must be split or combined")
	}
	if c.Compression != "gzip" && c.Compression != "none" {
		return fmt.Errorf("cache-compression must be gzip or none")
	}
	if c.BuildMaxAge < 0 || c.MaxBytes <= 0 || c.MaxBytes > DefaultMaxBytes {
		return fmt.Errorf("cache build age must be nonnegative and max bytes must be between 1 and %d", DefaultMaxBytes)
	}
	if c.UploadConcurrency < 1 || c.UploadConcurrency > 32 || c.UploadChunkBytes < 1 || c.UploadChunkBytes > 4000<<20 {
		return fmt.Errorf("cache upload concurrency must be 1..32 and chunk bytes 1..4194304000")
	}
	return nil
}

// GoRoots queries the selected executable, preserving user cache path settings.
func GoRoots(ctx context.Context, toolchain string) ([]cachearchive.Root, error) {
	name := "go"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cmd := exec.CommandContext(ctx, filepath.Join(toolchain, "bin", name), "env", "-json", "GOCACHE", "GOMODCACHE")
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if !strings.EqualFold(k, "GOROOT") && !strings.EqualFold(k, "GOTOOLCHAIN") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "GOROOT="+toolchain, "GOTOOLCHAIN=local")
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("query selected Go cache paths: %w", err)
	}
	var paths struct{ GOCACHE, GOMODCACHE string }
	if err = json.Unmarshal(b, &paths); err != nil {
		return nil, err
	}
	var roots []cachearchive.Root
	for _, r := range []cachearchive.Root{{Name: "modules", Path: paths.GOMODCACHE}, {Name: "build", Path: paths.GOCACHE}} {
		if !filepath.IsAbs(r.Path) || strings.ContainsAny(r.Path, "\x00\r\n") {
			return nil, fmt.Errorf("Go %s path must be absolute (GOCACHE=off is incompatible with caching)", r.Name)
		}
		roots = append(roots, r)
	}
	if roots[0].Path == roots[1].Path {
		return nil, fmt.Errorf("module and build cache paths must differ")
	}
	for i := range roots {
		for j := range roots {
			if i != j {
				rel, e := filepath.Rel(roots[i].Path, roots[j].Path)
				if e == nil && filepath.IsLocal(rel) {
					return nil, fmt.Errorf("module and build cache paths must not overlap")
				}
			}
		}
	}
	return roots, nil
}

var dependencyNames = map[string]bool{"go.mod": true, "go.sum": true, "go.work": true, "go.work.sum": true}
var skipDirs = map[string]bool{".git": true, ".cache": true, "vendor": true, "node_modules": true, "dist": true, "bin": true, ".setup-go-better": true}

// Dependencies includes manifests in all modules, with stable relative names.
// Explicit files may have any name; explicit directories use recursive discovery.
func Dependencies(ctx context.Context, workspace, paths string) (string, error) {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	var inputs []string
	if strings.TrimSpace(paths) == "" {
		inputs = []string{"."}
	} else {
		for _, p := range strings.Split(strings.ReplaceAll(paths, "\r\n", "\n"), "\n") {
			if p = strings.TrimSpace(p); p != "" {
				inputs = append(inputs, p)
			}
		}
	}
	files := map[string]bool{}
	for _, p := range inputs {
		if filepath.IsAbs(p) {
			return "", fmt.Errorf("cache-dependency-path must be workspace-relative: %s", p)
		}
		full := filepath.Join(workspace, p)
		rel, e := filepath.Rel(workspace, full)
		if e != nil || (rel != "." && !filepath.IsLocal(rel)) {
			return "", fmt.Errorf("cache dependency path escapes workspace")
		}
		st, e := os.Lstat(full)
		if e != nil {
			return "", e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("cache dependency path is a symlink: %s", p)
		}
		if st.Mode().IsRegular() {
			files[full] = true
			continue
		}
		if !st.IsDir() {
			return "", fmt.Errorf("cache dependency is not a file/directory: %s", p)
		}
		e = filepath.WalkDir(full, func(name string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if name != full && skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if dependencyNames[d.Name()] {
				info, err := d.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					return fmt.Errorf("dependency manifest is not a regular file: %s", name)
				}
				files[name] = true
			}
			return nil
		})
		if e != nil {
			return "", e
		}
	}
	var names []string
	for p := range files {
		names = append(names, p)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, p := range names {
		rel, _ := filepath.Rel(workspace, p)
		fmt.Fprintf(h, "%s\x00", filepath.ToSlash(rel))
		f, e := os.Open(p)
		if e != nil {
			return "", e
		}
		_, e = io.Copy(h, f)
		ce := f.Close()
		if e != nil {
			return "", e
		}
		if ce != nil {
			return "", ce
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:24], nil
}

// Service versions retain the complete SHA-256 digest, matching the toolkit.
func versionDigest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func digest(s string) string        { return versionDigest(s)[:24] }
func namespace(scope string) string {
	workflow, _, _ := strings.Cut(os.Getenv("GITHUB_WORKFLOW_REF"), "@")
	if workflow == "" {
		workflow = os.Getenv("GITHUB_WORKFLOW")
	}
	return digest(os.Getenv("GITHUB_REPOSITORY") + "\x00" + workflow + "\x00" + os.Getenv("GITHUB_JOB") + "\x00" + scope)
}
