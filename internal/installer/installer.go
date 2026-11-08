package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Metrics are wall times. Download includes blocked body reads; extraction excludes
// those reads, so the streaming tar pipeline is not counted twice.
type Metrics struct {
	ResolutionMS       float64 `json:"resolution_ms"`
	DownloadMS         float64 `json:"download_ms"`
	ExtractionMS       float64 `json:"extraction_ms"`
	TotalMS            float64 `json:"total_ms"`
	MetadataBytes      int64   `json:"metadata_bytes"`
	ArchiveBytes       int64   `json:"archive_bytes"`
	PeakTemporaryBytes int64   `json:"peak_temporary_bytes"`
}
type Result struct {
	Version string  `json:"go_version"`
	Root    string  `json:"goroot"`
	Source  string  `json:"source"`
	Metrics Metrics `json:"metrics"`
}
type Config struct {
	OS, Arch, ToolCache, InstallDir string
	Client                          *http.Client
	// URLs are injectable for local protocol tests; not action inputs.
	MetadataURL, DownloadURL string
}

func DefaultConfig() Config {
	tmp := os.Getenv("RUNNER_TEMP")
	if tmp == "" {
		tmp = os.TempDir()
	}
	return Config{OS: runtime.GOOS, Arch: runtime.GOARCH, ToolCache: os.Getenv("RUNNER_TOOL_CACHE"), InstallDir: filepath.Join(tmp, "setup-go-better", "toolchains"), Client: &http.Client{Timeout: 5 * time.Minute}, MetadataURL: "https://go.dev/dl/?mode=json", DownloadURL: "https://go.dev/dl/"}
}
func elapsed(t time.Time) float64 { return float64(time.Since(t)) / float64(time.Millisecond) }
func supported(osName, arch string) bool {
	return (osName == "linux" || osName == "darwin" || osName == "windows") && (arch == "amd64" || arch == "arm64")
}
func executable(osName string) string {
	if osName == "windows" {
		return "go.exe"
	}
	return "go"
}
func complete(root string, v Version, osName string) bool {
	b, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return false
	}
	if strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)[0] != "go"+v.String() {
		return false
	}
	s, err := os.Stat(filepath.Join(root, "bin", executable(osName)))
	return err == nil && s.Mode().IsRegular()
}

type candidate struct {
	Version
	root, source string
}

func (c Config) installed(s Selector) []candidate {
	var found []candidate
	arch := c.Arch
	if arch == "amd64" {
		arch = "x64"
	}
	for _, spec := range []struct{ base, source string }{{c.InstallDir, "installed"}, {filepath.Join(c.ToolCache, "go"), "runner-tool-cache"}} {
		if spec.source == "runner-tool-cache" && c.ToolCache == "" {
			continue
		}
		entries, _ := os.ReadDir(spec.base)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			v, err := ParseSelector(e.Name())
			if err != nil || !v.Exact || !s.Matches(v.Version) {
				continue
			}
			root := filepath.Join(spec.base, e.Name(), c.OS+"-"+c.Arch)
			marker := filepath.Join(root, ".complete")
			if spec.source == "runner-tool-cache" {
				root = filepath.Join(spec.base, e.Name(), arch)
				marker = root + ".complete"
			}
			if st, err := os.Stat(marker); err == nil && st.Mode().IsRegular() && complete(root, v.Version, c.OS) {
				found = append(found, candidate{v.Version, root, spec.source})
			}
		}
	}
	return found
}
func best(cs []candidate) (candidate, bool) {
	var b candidate
	ok := false
	for _, c := range cs {
		if !ok || b.Version.Less(c.Version) {
			b, ok = c, true
		}
	}
	return b, ok
}

type archive struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
}
type release struct {
	Version string    `json:"version"`
	Stable  bool      `json:"stable"`
	Files   []archive `json:"files"`
}

func (c Config) releases(ctx context.Context, historical bool, m *Metrics) ([]release, error) {
	u := c.MetadataURL
	if historical {
		u += "&include=all"
	}
	resp, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	m.MetadataBytes += int64(len(b))
	if err != nil {
		return nil, err
	}
	var rs []release
	err = json.Unmarshal(b, &rs)
	return rs, err
}
func (c Config) resolve(ctx context.Context, s Selector, m *Metrics) (Version, archive, error) {
	for _, all := range []bool{false, true} {
		rs, err := c.releases(ctx, all, m)
		if err != nil {
			return Version{}, archive{}, fmt.Errorf("release metadata: %w", err)
		}
		var v Version
		var a archive
		found := false
		for _, r := range rs {
			x, err := ParseSelector(strings.TrimPrefix(r.Version, "go"))
			if err != nil || !x.Exact || !r.Stable || !s.Matches(x.Version) {
				continue
			}
			for _, f := range r.Files {
				if f.Kind == "archive" && f.OS == c.OS && f.Arch == c.Arch && (!found || v.Less(x.Version)) {
					v, a, found = x.Version, f, true
				}
			}
		}
		if found {
			return v, a, nil
		}
	}
	return Version{}, archive{}, fmt.Errorf("no official archive for requested Go version on %s/%s", c.OS, c.Arch)
}
func (c Config) get(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "setup-go-better")
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: HTTP %d", u, resp.StatusCode)
	}
	return resp, nil
}
func (c Config) Setup(ctx context.Context, version string, latest bool) (r Result, err error) {
	return c.SetupResolved(ctx, version, latest, nil)
}

// SetupResolved announces the exact selected version before downloading it.
// The callback must return promptly; the toolchain is not executable yet.
func (c Config) SetupResolved(ctx context.Context, version string, latest bool, resolved func(string)) (r Result, err error) {
	start := time.Now()
	defer func() { r.Metrics.TotalMS = elapsed(start) }()
	if !supported(c.OS, c.Arch) {
		return r, fmt.Errorf("unsupported platform %s/%s", c.OS, c.Arch)
	}
	s, err := ParseSelector(version)
	if err != nil {
		return r, err
	}
	c.InstallDir, err = filepath.Abs(c.InstallDir)
	if err != nil {
		return r, err
	}
	if c.ToolCache != "" {
		c.ToolCache, err = filepath.Abs(c.ToolCache)
		if err != nil {
			return r, err
		}
	}
	resolution := time.Now()
	cs := c.installed(s)
	if b, ok := best(cs); ok && (s.Exact || !latest) {
		r.Version, r.Root, r.Source = b.Version.String(), b.root, b.source
		r.Metrics.ResolutionMS = elapsed(resolution)
		if resolved != nil {
			resolved(r.Version)
		}
		return r, nil
	}
	v, a, err := c.resolve(ctx, s, &r.Metrics)
	r.Metrics.ResolutionMS = elapsed(resolution)
	if err != nil {
		return r, err
	}
	for _, b := range cs {
		if b.Version == v {
			r.Version, r.Root, r.Source = v.String(), b.root, b.source
			if resolved != nil {
				resolved(r.Version)
			}
			return r, nil
		}
	}
	r.Version = v.String()
	r.Source = "download"
	if resolved != nil {
		resolved(r.Version)
	}
	r.Root, err = c.install(ctx, v, a, &r.Metrics)
	return r, err
}
