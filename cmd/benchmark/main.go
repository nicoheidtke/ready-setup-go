// benchmark compares launch paths, not GitHub's action preparation overhead.
// The separate workflow records real action step and preparation durations.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"setup-go-faster/internal/installer"
	"sort"
	"strings"
	"time"
)

type sample struct {
	Index               int              `json:"index"`
	Launcher            string           `json:"launcher"`
	ElapsedMS           float64          `json:"elapsed_ms"`
	LauncherAndExportMS float64          `json:"launcher_and_export_ms"`
	Result              installer.Result `json:"result"`
}
type summary struct {
	MedianMS float64 `json:"median_ms"`
	P95MS    float64 `json:"p95_ms"`
}
type report struct {
	Platform     string             `json:"platform"`
	ImageOS      string             `json:"image_os"`
	ImageVersion string             `json:"image_version"`
	Revision     string             `json:"revision"`
	Scenario     string             `json:"scenario"`
	NodeVersion  string             `json:"node_version"`
	ShellVersion string             `json:"shell_version"`
	Samples      []sample           `json:"samples"`
	Summary      map[string]summary `json:"summary"`
	Notes        []string           `json:"notes"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func cleanEnv(extra map[string]string) []string {
	// Preserve runner context but prevent subprocesses from altering this job.
	for _, k := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE", "STATE_cache_state", "GOROOT", "INPUT_GO-VERSION", "INPUT_CHECK-LATEST"} {
		if _, ok := extra[k]; !ok {
			extra[k] = ""
		}
	}
	// Installation samples must remain independent from optional restore/post work.
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(strings.ToUpper(key), "INPUT_CACHE") {
			extra[key] = ""
		}
	}
	extra["INPUT_CACHE"] = "off"
	var env []string
	for _, s := range os.Environ() {
		k, _, _ := strings.Cut(s, "=")
		keep := true
		for x := range extra {
			if strings.EqualFold(k, x) {
				keep = false
				break
			}
		}
		if keep {
			env = append(env, s)
		}
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
func toolVersion(name string, args ...string) string {
	b, e := exec.Command(name, args...).Output()
	if e != nil {
		return e.Error()
	}
	return strings.SplitN(strings.TrimSpace(string(b)), "\n", 2)[0]
}
func run() error {
	dist := flag.String("dist", "dist", "packaged action directory")
	version := flag.String("version", "", "exact version to seed")
	scenario := flag.String("scenario", "installed-exact", "installed-exact, installed-minor, latest-minor, cold")
	count := flag.Int("samples", 20, "samples per launcher (at least 20)")
	out := flag.String("out", "benchmark.json", "JSON output")
	node := flag.String("node", "node", "Node executable (use runner Node 24 for representative measurements)")
	shell := flag.String("shell", "bash", "Bash executable")
	flag.Parse()
	if *count < 20 {
		return fmt.Errorf("at least 20 samples are required")
	}
	v, e := installer.ParseSelector(*version)
	if e != nil || !v.Exact {
		return fmt.Errorf("version must be exact and >=1.26.0")
	}
	if *scenario != "installed-exact" && *scenario != "installed-minor" && *scenario != "latest-minor" && *scenario != "cold" {
		return fmt.Errorf("unknown scenario")
	}
	root, e := filepath.Abs(*dist)
	if e != nil {
		return e
	}
	tmp, e := os.MkdirTemp("", "setup-go-faster-benchmark-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	platform := runtime.GOOS
	suffix := ""
	runnerOS := map[string]string{"linux": "Linux", "darwin": "macOS", "windows": "Windows"}[platform]
	arch := map[string]string{"amd64": "X64", "arm64": "ARM64"}[runtime.GOARCH]
	if platform == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(root, "bin", platform+"-"+runtime.GOARCH, "setup-go-faster"+suffix)
	warm := filepath.Join(tmp, "warm")
	if *scenario != "cold" {
		// Populate once outside measured samples. All subsequent warm exact/minor
		// executions have an invalid HTTP proxy, so accidental network use fails.
		cmd := exec.Command(binary, "setup", "--go-version", *version, "--cache=off")
		cmd.Env = cleanEnv(map[string]string{"RUNNER_TEMP": warm, "RUNNER_TOOL_CACHE": ""})
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if e = cmd.Run(); e != nil {
			return fmt.Errorf("seed: %w", e)
		}
	}
	r := report{Platform: platform + "/" + runtime.GOARCH, ImageOS: os.Getenv("ImageOS"), ImageVersion: os.Getenv("ImageVersion"), Revision: os.Getenv("GITHUB_SHA"), Scenario: *scenario, NodeVersion: toolVersion(*node, "--version"), ShellVersion: toolVersion(*shell, "--version"), Summary: map[string]summary{}, Notes: []string{"Launcher/export duration is process wall time minus installer-reported time; it includes environment export and JSON writing.", "Peak temporary bytes are peak logical file payload, excluding filesystem metadata and allocation rounding.", "Streaming extraction and body-read times are partitioned, not independent pipeline latencies.", "Action acquisition is not included in these local process samples; see workflow timing and acquisition experiments.", "No winner is inferred automatically. Compare paired differences and p95; investigate regressions and overlap."}}
	for i := 0; i < *count; i++ {
		order := []string{"composite", "node"}
		if i%2 == 1 {
			order[0], order[1] = order[1], order[0]
		}
		for _, launcher := range order {
			runTemp := warm
			if *scenario == "cold" {
				runTemp = filepath.Join(tmp, fmt.Sprintf("cold-%d-%s", i, launcher))
			}
			requested := *version
			latest := "false"
			if strings.Contains(*scenario, "minor") {
				requested = fmt.Sprintf("1.%d.x", v.Minor)
			}
			if *scenario == "latest-minor" {
				latest = "true"
			}
			jsonFile := filepath.Join(tmp, "sample.json")
			var cmd *exec.Cmd
			if launcher == "composite" {
				cmd = exec.Command(*shell, "--noprofile", "--norc", filepath.Join(root, "launch.sh"), "--cache=off", "--json", jsonFile)
			} else {
				cmd = exec.Command(*node, filepath.Join(root, "experimental", "node", "launch.cjs"), "--cache=off", "--json", jsonFile)
			}
			extra := map[string]string{"RUNNER_OS": runnerOS, "RUNNER_ARCH": arch, "RUNNER_TEMP": runTemp, "RUNNER_TOOL_CACHE": "", "SETUP_GO_FASTER_ROOT": root, "SETUP_GO_FASTER_VERSION": requested, "SETUP_GO_FASTER_LATEST": latest}
			if *scenario == "installed-exact" || *scenario == "installed-minor" {
				extra["HTTP_PROXY"] = "http://127.0.0.1:1"
				extra["HTTPS_PROXY"] = "http://127.0.0.1:1"
				extra["NO_PROXY"] = ""
			}
			cmd.Env = cleanEnv(extra)
			cmd.Stderr = os.Stderr
			start := time.Now()
			if e = cmd.Run(); e != nil {
				return fmt.Errorf("sample %d %s: %w", i, launcher, e)
			}
			wall := float64(time.Since(start)) / float64(time.Millisecond)
			b, e := os.ReadFile(jsonFile)
			if e != nil {
				return e
			}
			var result installer.Result
			if e = json.Unmarshal(b, &result); e != nil {
				return e
			}
			if (*scenario == "installed-exact" || *scenario == "installed-minor") && result.Source != "installed" {
				return fmt.Errorf("warm sample unexpectedly used %s", result.Source)
			}
			r.Samples = append(r.Samples, sample{i, launcher, wall, wall - result.Metrics.TotalMS, result})
			if *scenario == "cold" {
				if e = os.RemoveAll(runTemp); e != nil {
					return e
				}
			}
		}
	}
	for _, name := range []string{"composite", "node"} {
		var values []float64
		for _, s := range r.Samples {
			if s.Launcher == name {
				values = append(values, s.ElapsedMS)
			}
		}
		sort.Float64s(values)
		n := len(values)
		median := values[n/2]
		if n%2 == 0 {
			median = (values[n/2-1] + median) / 2
		}
		r.Summary[name] = summary{median, values[(95*n+99)/100-1]}
	}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(*out, append(b, '\n'), 0644)
}
