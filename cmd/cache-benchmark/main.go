// cache-benchmark measures paired cache-off and cache-enabled Go workloads.
// It uses explicit synchronous restore/save for complete native operation timing;
// the separate cache-validation workflow exercises the runner's real post hook.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
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

	"ready-setup-go/internal/cache"
	"ready-setup-go/internal/cachearchive"
	"ready-setup-go/internal/cachebackend"
	"ready-setup-go/internal/installer"
)

const exasolRevision = "54b7e1a7454f49d821102bcab5bfce2c7747f8c6"

type options struct {
	Workload, Scenario, Source, Go, SeedGo, Out string
	Samples                                     int
	Validation                                  bool
	Config                                      cache.Config
	DependencyModule, DependencyVersion         string
}

type commandResult struct {
	Name           string   `json:"name"`
	Args           []string `json:"args"`
	DurationMS     float64  `json:"duration_ms"`
	ExitCode       int      `json:"exit_code"`
	Error          string   `json:"error,omitempty"`
	AllowedFailure bool     `json:"allowed_failure,omitempty"`
}
type sample struct {
	Index         int             `json:"index"`
	Order         int             `json:"order"`
	Mode          string          `json:"mode"`
	Measured      bool            `json:"measured"`
	Success       bool            `json:"success"`
	PreparationMS float64         `json:"preparation_ms"`
	TotalMS       float64         `json:"total_ms"`
	WorkloadMS    float64         `json:"workload_ms"`
	Restore       cache.Report    `json:"restore"`
	Save          cache.Report    `json:"save"`
	Commands      []commandResult `json:"commands"`
	Log           string          `json:"log"`
	Error         string          `json:"error,omitempty"`
}
type summary struct {
	Count         int     `json:"successful_pairs"`
	Failed        int     `json:"failed_pairs"`
	OffMedianMS   float64 `json:"cache_off_median_ms"`
	OnMedianMS    float64 `json:"cache_on_median_ms"`
	DeltaMedianMS float64 `json:"paired_delta_median_ms"`
	DeltaP95MS    float64 `json:"paired_delta_p95_ms"`
	Status        string  `json:"status"`
}
type report struct {
	Schema            string       `json:"schema"`
	Workload          string       `json:"workload"`
	Scenario          string       `json:"scenario"`
	Platform          string       `json:"platform"`
	GoVersion         string       `json:"go_version"`
	SeedGoVersion     string       `json:"seed_go_version"`
	SourceRevision    string       `json:"source_revision"`
	ActionRevision    string       `json:"action_revision"`
	ImageOS           string       `json:"image_os"`
	ImageVersion      string       `json:"image_version"`
	Config            cache.Config `json:"cache_config"`
	DependencyModule  string       `json:"dependency_module,omitempty"`
	DependencyVersion string       `json:"dependency_version,omitempty"`
	Seeds             []sample     `json:"seeds"`
	Samples           []sample     `json:"samples"`
	Summary           summary      `json:"summary"`
	Notes             []string     `json:"notes"`
}

type workloadRunner func(context.Context, string, string, string, string, io.Writer) ([]commandResult, error)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	o := options{Config: cache.DefaultConfig()}
	// This synchronous maintainer harness explicitly writes its isolated samples;
	// branch eligibility is covered by main/post fixtures, not these direct APIs.
	o.Config.SaveBranches = "all"
	flag.StringVar(&o.Workload, "workload", "small", "small, monorepo, exasol-build, exasol-test, exasol-lint")
	flag.StringVar(&o.Scenario, "scenario", "unchanged", "cold, unchanged, source, dependency, go-upgrade")
	flag.StringVar(&o.Source, "source", "testdata/packages", "pinned package fixture or Exasol checkout")
	flag.StringVar(&o.Go, "go", "go", "selected workload Go executable")
	flag.StringVar(&o.SeedGo, "seed-go", "", "seed Go executable; required and different for go-upgrade")
	flag.StringVar(&o.Out, "out", "cache-benchmark.json", "raw JSON report; command logs are stored alongside it")
	flag.IntVar(&o.Samples, "samples", 20, "paired observations, minimum 20 outside correctness mode")
	flag.BoolVar(&o.Validation, "validation", false, "correctness-only smoke; permits fewer samples and makes no performance summary")
	flag.StringVar(&o.Config.Mode, "mode", "restore-save", "candidate mode: restore-save or restore; baseline is always off")
	flag.StringVar(&o.Config.SavePolicy, "save-policy", o.Config.SavePolicy, "on-miss or always; candidate snapshot refresh policy")
	flag.StringVar(&o.Config.Layout, "layout", "split", "split or combined")
	flag.IntVar(&o.Config.ChunkConcurrency, "chunk-concurrency", o.Config.ChunkConcurrency, "parallel chunk compression/transfer workers, 1..16")
	flag.IntVar(&o.Config.CompressionConcurrency, "compression-concurrency", o.Config.CompressionConcurrency, "whole-archive compression workers, 1..16")
	flag.IntVar(&o.Config.RestoreConcurrency, "restore-concurrency", o.Config.RestoreConcurrency, "restored-file publication workers, 1..16")
	flag.StringVar(&o.Config.Storage, "storage", "archive", "archive or chunked")
	flag.StringVar(&o.Config.Compression, "compression", "gzip", "gzip, zstd, or none")
	flag.Int64Var(&o.Config.MaxBytes, "max-bytes", o.Config.MaxBytes, "uncompressed archive and transferred archive byte limit")
	flag.IntVar(&o.Config.UploadConcurrency, "upload-concurrency", o.Config.UploadConcurrency, "parallel blob upload workers")
	flag.IntVar(&o.Config.DownloadConcurrency, "download-concurrency", o.Config.DownloadConcurrency, "parallel archive download workers, 1..32")
	flag.Int64Var(&o.Config.DownloadChunkBytes, "download-chunk-bytes", o.Config.DownloadChunkBytes, "archive download range size bytes")
	flag.Int64Var(&o.Config.UploadChunkBytes, "upload-chunk-bytes", o.Config.UploadChunkBytes, "blob upload block bytes")
	flag.DurationVar(&o.Config.BuildMaxAge, "build-max-age", 0, "optional archive-only build-cache age filter")
	flag.StringVar(&o.DependencyModule, "dependency-module", "github.com/google/uuid", "module changed outside timing in dependency scenario")
	flag.StringVar(&o.DependencyVersion, "dependency-version", "v1.5.0", "exact module version for dependency scenario")
	dependency := flag.String("dependency", "", "optional module@version shorthand for dependency mutation")
	flag.Parse()
	if *dependency != "" {
		var ok bool
		o.DependencyModule, o.DependencyVersion, ok = strings.Cut(*dependency, "@")
		if !ok {
			return errors.New("dependency must be module@version")
		}
	}
	if err := validateOptions(o); err != nil {
		return err
	}
	if os.Getenv("ACTIONS_RUNTIME_TOKEN") == "" || os.Getenv("ACTIONS_RESULTS_URL") == "" {
		return errors.New("cache benchmark requires runner credentials; invoke the Node benchmark action")
	}
	b := &cachebackend.Client{BaseURL: os.Getenv("ACTIONS_RESULTS_URL"), Token: os.Getenv("ACTIONS_RUNTIME_TOKEN"), MaxBytes: o.Config.MaxBytes, BlockSize: o.Config.UploadChunkBytes, Concurrency: o.Config.UploadConcurrency, DownloadConcurrency: o.Config.DownloadConcurrency, DownloadBlockSize: o.Config.DownloadChunkBytes}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Hour)
	defer cancel()
	r, err := benchmark(ctx, o, b, runWorkload)
	if e := writeJSON(o.Out, r); e != nil {
		return e
	}
	return err
}

func validateOptions(o options) error {
	if o.Samples < 1 || (!o.Validation && o.Samples < 20) {
		return errors.New("at least 20 paired samples are required; --validation is correctness-only")
	}
	if o.Samples > 100 {
		return errors.New("at most 100 paired samples per job")
	}
	if o.Config.Mode != "restore-save" && o.Config.Mode != "restore" {
		return errors.New("candidate mode must be restore or restore-save")
	}
	if err := o.Config.Validate(); err != nil {
		return err
	}
	if !contains([]string{"small", "monorepo", "exasol-build", "exasol-test", "exasol-lint"}, o.Workload) {
		return errors.New("unknown workload")
	}
	if !contains([]string{"cold", "unchanged", "source", "dependency", "go-upgrade"}, o.Scenario) {
		return errors.New("unknown scenario")
	}
	if o.Scenario == "go-upgrade" && o.SeedGo == "" {
		return errors.New("go-upgrade requires --seed-go")
	}
	if strings.ContainsAny(o.DependencyModule+o.DependencyVersion, "\r\n\x00") || strings.HasPrefix(o.DependencyModule, "-") || !strings.HasPrefix(o.DependencyVersion, "v") || strings.ContainsAny(o.DependencyVersion, " @") {
		return errors.New("dependency mutation requires a module and exact v-prefixed version")
	}
	return nil
}

func benchmark(ctx context.Context, o options, backend cache.Backend, runner workloadRunner) (report, error) {
	r := report{Schema: "ready-setup-go-cache-benchmark-v1", Workload: o.Workload, Scenario: o.Scenario, Platform: runtime.GOOS + "/" + runtime.GOARCH, ActionRevision: os.Getenv("GITHUB_SHA"), ImageOS: os.Getenv("ImageOS"), ImageVersion: os.Getenv("ImageVersion"), Config: o.Config, Notes: []string{
		"Native timing includes dependency fingerprint/state preparation, restore, the complete workload, archive/pruning and synchronous save. Installation, action acquisition, checkout, fixture mutation, seed and local reset are outside samples.",
		"Every pair gets a fresh remote namespace and independent unmeasured seed. Local module/build directories are emptied before each variant; seed and measured work use identical absolute source/cache paths within the pair.",
		"Order reverses on alternating pairs. OS page caches, upstream module proxies and CPU scheduling are uncontrolled; retain failures and repeat on all target platforms.",
		"Synchronous native Save measures upload fully; actual runner post-hook scheduling is validated separately. Jobs API collector records complete job/post durations with coarse timestamps.",
		"The baseline disables this action's build/module cache; GitHub-hosted installed toolchains and upstream module CDN caching are separate concerns.",
		"Fresh namespaces isolate sample results and consume remote storage. Use manual runs sparingly and repository cache retention controls; this helper does not delete repository caches.",
		"Exasol timing preserves go mod download and its three optional tool warmups before generation/workload. Lint results and tflint plugins use isolated local paths and are not part of this action's module/build snapshots. Task setup, dedicated tool-cache restore/save, Python/integration/deployment steps, and distribution installation are outside scope; these are not whole-upstream-job equivalence claims.",
	}}
	if err := validateOptions(o); err != nil {
		return r, err
	}
	goPath, version, err := toolchain(ctx, o.Go)
	if err != nil {
		return r, err
	}
	r.GoVersion = version
	seedPath, seedVersion := goPath, version
	if o.SeedGo != "" {
		seedPath, seedVersion, err = toolchain(ctx, o.SeedGo)
		if err != nil {
			return r, err
		}
	}
	r.SeedGoVersion = seedVersion
	if o.Scenario == "go-upgrade" && version == seedVersion {
		return r, errors.New("go-upgrade requires different exact seed and target Go versions")
	}
	if strings.HasPrefix(o.Workload, "exasol-") {
		cmd := exec.CommandContext(ctx, "git", "-C", o.Source, "rev-parse", "HEAD")
		b, e := cmd.Output()
		if e != nil || strings.TrimSpace(string(b)) != exasolRevision {
			return r, errors.New("Exasol checkout must match the recorded acceptance commit")
		}
		clean := exec.CommandContext(ctx, "git", "-C", o.Source, "diff", "--quiet", "HEAD", "--")
		if clean.Run() != nil {
			return r, errors.New("Exasol checkout has tracked modifications; benchmark requires the clean recorded commit")
		}
		r.SourceRevision = exasolRevision
	} else {
		r.SourceRevision = "repository testdata/packages plus generated two-module workspace"
	}
	root, err := os.MkdirTemp("", "ready-setup-go-cache-benchmark-*")
	if err != nil {
		return r, err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		os.Remove(root)
		return r, err
	}
	root = canonical
	defer removeTree(root)
	out, err := filepath.Abs(o.Out)
	if err != nil {
		return r, err
	}
	if err = os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return r, err
	}
	base := filepath.Join(root, "base")
	if err = prepareFixture(o, base); err != nil {
		return r, err
	}
	changed := filepath.Join(root, "changed")
	if err = copyTree(base, changed); err != nil {
		return r, err
	}
	if o.Scenario == "source" {
		if err = mutateSource(changed, o.Workload); err != nil {
			return r, err
		}
	}
	if o.Scenario == "dependency" {
		r.DependencyModule = o.DependencyModule
		r.DependencyVersion = o.DependencyVersion
		before, e := cache.Dependencies(ctx, changed, "")
		if e != nil {
			return r, e
		}
		if err = mutateDependency(ctx, goPath, changed, filepath.Join(root, "preparation-caches"), o); err != nil {
			return r, err
		}
		after, e := cache.Dependencies(ctx, changed, "")
		if e != nil {
			return r, e
		}
		if before == after {
			return r, errors.New("dependency mutation did not change manifests; select a different exact dependency version")
		}
		r.Notes = append(r.Notes, "Dependency scenario uses an explicit manifest/module-version change prepared with separate caches before timing; choose versions appropriate to the target project. Default uuid v1.6.0 to v1.5.0 is a controlled downgrade, not a latest-version claim.")
	}
	failed := false
	for i := 0; i < o.Samples; i++ {
		if err = ctx.Err(); err != nil {
			return r, err
		}
		pair := filepath.Join(root, fmt.Sprintf("pair-%03d", i))
		work := filepath.Join(pair, "workspace")
		caches := filepath.Join(pair, "caches")
		cfg := o.Config
		cfg.Strict = true
		cfg.Scope = filepath.Base(root) + fmt.Sprintf("/pair-%03d/%s/%s", i, o.Workload, o.Scenario)
		if err = copyTree(base, work); err != nil {
			return r, err
		}
		seedOK := true
		expected := restoreExpectation{Miss: o.Scenario == "cold" || o.Scenario == "go-upgrade"}
		if o.Scenario != "cold" {
			seed := executeSample(ctx, i, -1, "seed", seedPath, seedVersion, work, caches, cfg, backend, runner, o.Workload, filepath.Join(filepath.Dir(out), fmt.Sprintf("seed-%03d.log", i)), true, restoreExpectation{})
			seed.Measured = false
			r.Seeds = append(r.Seeds, seed)
			seedOK = seed.Success
			if !expected.Miss {
				for _, m := range seed.Save.Snapshots {
					if m.Files > 0 && !m.Skipped {
						expected.Kinds = append(expected.Kinds, m.Kind)
					}
				}
			}
		}
		modes := []string{"off", cfg.Mode}
		if i%2 == 1 {
			modes[0], modes[1] = modes[1], modes[0]
		}
		for order, mode := range modes {
			logPath := filepath.Join(filepath.Dir(out), fmt.Sprintf("sample-%03d-%s.log", i, mode))
			if !seedOK {
				r.Samples = append(r.Samples, sample{Index: i, Order: order, Mode: mode, Error: "seed failed; pair not measured", Log: logPath})
				failed = true
				continue
			}
			if err = resetPair(work, caches, changed); err != nil {
				return r, err
			}
			s := executeSample(ctx, i, order, mode, goPath, version, work, caches, cfg, backend, runner, o.Workload, logPath, false, expected)
			r.Samples = append(r.Samples, s)
			if !s.Success {
				failed = true
			}
			if err = writeJSON(out, r); err != nil {
				return r, err
			}
		}
	}
	r.Summary = summarize(r.Samples, o.Validation)
	if failed {
		return r, errors.New("one or more cache benchmark samples failed; raw observations were retained")
	}
	return r, nil
}

type restoreExpectation struct {
	Miss  bool
	Kinds []string
}

func (e restoreExpectation) Check(r cache.Report) error {
	for _, m := range r.Snapshots {
		if e.Miss && m.Hit {
			return errors.New("unexpected cache hit in isolated cold/Go-upgrade scenario")
		}
	}
	for _, kind := range e.Kinds {
		hit := false
		for _, m := range r.Snapshots {
			if m.Kind == kind && m.Hit {
				hit = true
			}
		}
		if !hit {
			return fmt.Errorf("seeded %s archive did not restore; sample is not warm", kind)
		}
	}
	return nil
}

func executeSample(ctx context.Context, index, order int, mode, goPath, version, work, caches string, cfg cache.Config, backend cache.Backend, runner workloadRunner, workload, logPath string, seed bool, expected restoreExpectation) (s sample) {
	s = sample{Index: index, Order: order, Mode: mode, Measured: !seed, Log: logPath}
	log, err := os.Create(logPath)
	if err != nil {
		s.Error = err.Error()
		return
	}
	defer log.Close()
	if err = os.MkdirAll(caches, 0755); err != nil {
		s.Error = err.Error()
		return
	}
	start := time.Now()
	defer func() { s.TotalMS = milliseconds(start) }()
	var state cache.State
	if mode != "off" {
		prep := time.Now()
		deps, e := cache.Dependencies(ctx, work, "")
		if e != nil {
			s.Error = e.Error()
			return
		}
		roots := []cachearchive.Root{{Name: "modules", Path: filepath.Join(caches, "modules")}, {Name: "build", Path: filepath.Join(caches, "build")}}
		state, e = cache.NewState(cfg, version, deps, roots)
		s.PreparationMS = milliseconds(prep)
		if e != nil {
			s.Error = e.Error()
			return
		}
		if !seed {
			s.Restore, err = cache.Restore(ctx, backend, &state)
			if err != nil {
				s.Error = err.Error()
				return
			}
			if err = expected.Check(s.Restore); err != nil {
				s.Error = err.Error()
				return
			}
		}
	}
	workStart := time.Now()
	s.Commands, err = runner(ctx, goPath, work, caches, workload, log)
	s.WorkloadMS = milliseconds(workStart)
	if err != nil {
		s.Error = err.Error()
		return
	}
	if seed || mode == "restore-save" {
		s.Save, err = cache.Save(ctx, backend, state)
		if err != nil {
			s.Error = err.Error()
			return
		}
	}
	s.Success = true
	return
}

func summarize(samples []sample, validation bool) summary {
	s := summary{Status: "incomplete: fewer than 20 successful pairs"}
	byPair := map[int]map[string]sample{}
	for _, observation := range samples {
		if byPair[observation.Index] == nil {
			byPair[observation.Index] = map[string]sample{}
		}
		byPair[observation.Index][observation.Mode] = observation
	}
	var off, on, deltas []float64
	for _, pair := range byPair {
		a, ok := pair["off"]
		var b sample
		for mode, x := range pair {
			if mode != "off" {
				b = x
			}
		}
		if !ok || !a.Success || !b.Success || !a.Measured || !b.Measured {
			s.Failed++
			continue
		}
		s.Count++
		off = append(off, a.TotalMS)
		on = append(on, b.TotalMS)
		deltas = append(deltas, b.TotalMS-a.TotalMS)
	}
	if validation {
		s.Status = "correctness only; no performance summary"
		return s
	}
	if len(deltas) > 0 {
		s.OffMedianMS = percentile(off, 50)
		s.OnMedianMS = percentile(on, 50)
		s.DeltaMedianMS = percentile(deltas, 50)
		s.DeltaP95MS = percentile(deltas, 95)
	}
	if s.Count >= 20 {
		s.Status = "descriptive paired measurements; inspect raw failures, p95 and repeat across platforms before drawing conclusions"
	}
	return s
}

func percentile(xs []float64, p int) float64 {
	sort.Float64s(xs)
	n := len(xs)
	if p == 50 && n%2 == 0 {
		return (xs[n/2-1] + xs[n/2]) / 2
	}
	return xs[(p*n+99)/100-1]
}
func milliseconds(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}
func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func toolchain(ctx context.Context, name string) (string, string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	cmd := exec.CommandContext(ctx, path, "env", "GOVERSION")
	cmd.Env = commandEnv("", "", nil)
	b, err := cmd.Output()
	if err != nil {
		return "", "", errors.New("query benchmark Go version failed")
	}
	version := strings.TrimPrefix(strings.TrimSpace(string(b)), "go")
	selector, e := installer.ParseSelector(version)
	if e != nil || !selector.Exact {
		return "", "", errors.New("benchmark needs a released Go toolchain")
	}
	return path, version, nil
}

func commandEnv(goPath, caches string, extra map[string]string) []string {
	replace := map[string]string{"GOTOOLCHAIN": "local", "GOTELEMETRY": "off", "GOROOT": "", "GOWORK": "", "GITHUB_ENV": "", "GITHUB_OUTPUT": "", "GITHUB_PATH": "", "GITHUB_STATE": "", "STATE_cache_state": "", "ACTIONS_RUNTIME_TOKEN": "", "ACTIONS_RESULTS_URL": "", "GH_TOKEN": "", "GITHUB_TOKEN": ""}
	if caches != "" {
		replace["GOCACHE"] = filepath.Join(caches, "build")
		replace["GOMODCACHE"] = filepath.Join(caches, "modules")
		replace["GOPATH"] = filepath.Join(caches, "gopath")
		replace["GOLANGCI_LINT_CACHE"] = filepath.Join(caches, "golangci-lint")
		replace["TFLINT_PLUGIN_DIR"] = filepath.Join(caches, "tflint-plugins")
	}
	if goPath != "" {
		replace["PATH"] = filepath.Dir(goPath) + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	for k, v := range extra {
		replace[k] = v
	}
	var env []string
	for _, item := range os.Environ() {
		k, _, _ := strings.Cut(item, "=")
		skip := strings.HasPrefix(strings.ToUpper(k), "INPUT_CACHE")
		for key := range replace {
			if strings.EqualFold(k, key) {
				skip = true
				break
			}
		}
		if !skip {
			env = append(env, item)
		}
	}
	for k, v := range replace {
		env = append(env, k+"="+v)
	}
	return env
}

func runWorkload(ctx context.Context, goPath, work, caches, workload string, log io.Writer) ([]commandResult, error) {
	type command struct {
		name, dir string
		args      []string
		env       map[string]string
		optional  bool
	}
	commands := []command{{"test", work, []string{"test", "-mod=readonly", "-count=1", "./..."}, nil, false}}
	if workload == "monorepo" {
		commands = nil
		for _, module := range []string{"alpha", "beta"} {
			commands = append(commands, command{"test/" + module, filepath.Join(work, module), []string{"test", "-mod=readonly", "-count=1", "./..."}, nil, false})
		}
	}
	if strings.HasPrefix(workload, "exasol-") {
		for _, name := range []string{"golangci-lint", "tflint-plugins"} {
			if err := os.MkdirAll(filepath.Join(caches, name), 0755); err != nil {
				return nil, err
			}
		}
		commands = []command{
			{"warmup/modules", work, []string{"mod", "download"}, nil, false},
			{"warmup/golangci-lint", work, []string{"tool", "golangci-lint", "version"}, nil, true},
			{"warmup/tflint-version", work, []string{"tool", "tflint", "--version"}, nil, true},
			{"warmup/tflint-init", work, []string{"tool", "tflint", "--init"}, nil, true},
			{"generate", work, []string{"generate", "./..."}, map[string]string{"GOFLAGS": "-tags=containers_image_openpgp", "SKIP_EMBED": "true"}, false},
		}
		switch workload {
		case "exasol-build":
			commands = append(commands, command{"build", work, []string{"build", "-mod=readonly", "-tags=containers_image_openpgp", "-trimpath", "-gcflags=all=-l", "-o", filepath.Join(caches, "exasol-validation"), "./cmd/exasol"}, map[string]string{"CGO_ENABLED": "0"}, false})
		case "exasol-test":
			commands = append(commands, command{"test", work, []string{"test", "-mod=readonly", "-tags=containers_image_openpgp", "-race", "-count=1", "-timeout=20m", "./..."}, nil, false})
		case "exasol-lint":
			commands = append(commands, command{"lint", work, []string{"tool", "golangci-lint", "run", "./..."}, nil, false})
		}
	}
	var results []commandResult
	for _, c := range commands {
		cmd := exec.CommandContext(ctx, goPath, c.args...)
		cmd.Dir = c.dir
		cmd.Env = commandEnv(goPath, caches, c.env)
		cmd.Stdout = log
		cmd.Stderr = log
		fmt.Fprintln(log, "command:", c.name, strings.Join(c.args, " "))
		start := time.Now()
		err := cmd.Run()
		r := commandResult{Name: c.name, Args: c.args, DurationMS: milliseconds(start)}
		if err != nil {
			r.ExitCode = -1
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				r.ExitCode = exit.ExitCode()
			}
			r.Error = "workload command failed"
			r.AllowedFailure = c.optional
			results = append(results, r)
			if c.optional {
				continue
			}
			return results, fmt.Errorf("%s failed (exit %d); see command log", c.name, r.ExitCode)
		}
		results = append(results, r)
	}
	return results, nil
}

func prepareFixture(o options, dst string) error {
	if o.Workload != "monorepo" {
		return copyTree(o.Source, dst)
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	for _, name := range []string{"alpha", "beta"} {
		module := filepath.Join(dst, name)
		if err := copyTree(o.Source, module); err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(module, "go.mod"))
		if err != nil {
			return err
		}
		lines := strings.Split(string(b), "\n")
		lines[0] = "module example.com/cache-benchmark/" + name
		if name == "beta" {
			lines = append(lines, "require example.com/cache-benchmark/alpha v0.0.0", "")
		}
		if err = os.WriteFile(filepath.Join(module, "go.mod"), []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return err
		}
		b, err = os.ReadFile(filepath.Join(module, "packages_test.go"))
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(module, "packages_test.go"), []byte(strings.Replace(string(b), "package packages_test", "package "+name+"_test", 1)), 0644); err != nil {
			return err
		}
		if name == "alpha" {
			err = os.WriteFile(filepath.Join(module, "value.go"), []byte("package alpha\n\nfunc Value() int { return 42 }\n"), 0644)
		} else {
			err = os.WriteFile(filepath.Join(module, "workspace_test.go"), []byte("package beta_test\n\nimport (\"testing\"; \"example.com/cache-benchmark/alpha\")\nfunc TestWorkspaceDependency(t *testing.T) { if alpha.Value() != 42 { t.Fatal(\"bad value\") } }\n"), 0644)
		}
		if err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dst, "go.work"), []byte("go 1.26.0\n\nuse (\n ./alpha\n ./beta\n)\n"), 0644)
}

func mutateSource(work, workload string) error {
	path := filepath.Join(work, "cache_benchmark_change_test.go")
	pkg := "packages_test"
	if workload == "monorepo" {
		path = filepath.Join(work, "alpha", "cache_benchmark_change.go")
		pkg = "alpha"
	}
	if strings.HasPrefix(workload, "exasol-") {
		path = filepath.Join(work, "cmd", "exasol", "cache_benchmark_change.go")
		pkg = "main"
	}
	return os.WriteFile(path, []byte("package "+pkg+"\n\n// CacheBenchmarkChange changes one package's compiler input.\nconst CacheBenchmarkChange = \"source-change\"\n"), 0644)
}

func mutateDependency(ctx context.Context, goPath, work, caches string, o options) error {
	dirs := []string{work}
	if o.Workload == "monorepo" {
		dirs = []string{filepath.Join(work, "alpha"), filepath.Join(work, "beta")}
	}
	for _, dir := range dirs {
		cmd := exec.CommandContext(ctx, goPath, "get", o.DependencyModule+"@"+o.DependencyVersion)
		cmd.Dir = dir
		cmd.Env = commandEnv(goPath, caches, nil)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return errors.New("prepare dependency mutation failed")
		}
	}
	return nil
}

func resetPair(work, caches, source string) error {
	if err := removeTree(caches); err != nil {
		return err
	}
	if err := removeTree(work); err != nil {
		return err
	}
	return copyTree(source, work)
}

// Go's module cache deliberately contains read-only directories/files. These
// paths are private temporary directories created by this helper, never input
// checkout paths; make only those owned copies writable before removing them.
func removeTree(path string) error {
	err := filepath.WalkDir(path, func(name string, d fs.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return os.Chmod(name, 0700)
		}
		return os.Chmod(name, 0600)
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != src && contains([]string{".git", ".cache", "node_modules", "vendor", "dist"}, d.Name()) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("benchmark source must contain regular files: %s", rel)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(filepath.Join(dst, rel), os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()|0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}
