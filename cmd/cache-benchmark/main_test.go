package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"setup-go-better/internal/cache"
	"setup-go-better/internal/cachebackend"
)

func testDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T) string {
	t.Helper()
	dir := testDir(t)
	writeFile(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(dir, "packages_test.go"), "package packages_test\n\nimport \"testing\"\nfunc TestValue(t *testing.T) {}\n")
	return dir
}
func testOptions(t *testing.T) options {
	t.Helper()
	c := cache.DefaultConfig()
	c.Mode = "restore-save"
	c.MaxBytes = 1 << 20
	return options{Workload: "small", Scenario: "unchanged", Source: fixture(t), Go: "go", Out: filepath.Join(testDir(t), "observations.json"), Samples: 2, Validation: true, Config: c, DependencyModule: "github.com/google/uuid", DependencyVersion: "v1.5.0"}
}

// This server models only cache-v2 control and direct signed-blob transfer. The
// real lifecycle, archive validation, and native HTTP client are used end to end.
func cacheServer(t *testing.T) *httptest.Server {
	t.Helper()
	type entry struct{ key, version, id string }
	var mu sync.Mutex
	var entries []entry
	pending := map[string]entry{}
	blobs := map[string][]byte{}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/blob/") {
			if r.Header.Get("Authorization") != "" || r.URL.Query().Get("sig") != "signed-only" {
				t.Error("invalid blob authentication")
			}
			if r.Method == "PUT" {
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				blobs[r.URL.Path] = b
				w.WriteHeader(201)
				return
			}
			_, _ = w.Write(blobs[r.URL.Path])
			return
		}
		if r.Header.Get("Authorization") != "Bearer runtime-only" {
			t.Error("missing runtime control token")
		}
		var req struct {
			Key         string   `json:"key"`
			Version     string   `json:"version"`
			RestoreKeys []string `json:"restore_keys"`
			Size        string   `json:"size_bytes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "CreateCacheEntry"):
			e := entry{req.Key, req.Version, fmt.Sprintf("/blob/%d", len(pending))}
			pending[req.Key] = e
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "signed_upload_url": server.URL + e.id + "?sig=signed-only"})
		case strings.HasSuffix(r.URL.Path, "FinalizeCacheEntryUpload"):
			e := pending[req.Key]
			if _, ok := blobs[e.id]; !ok {
				t.Error("finalized missing blob")
			}
			entries = append(entries, e)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case strings.HasSuffix(r.URL.Path, "GetCacheEntryDownloadURL"):
			for _, prefix := range append([]string{req.Key}, req.RestoreKeys...) {
				for i := len(entries) - 1; i >= 0; i-- {
					e := entries[i]
					if e.version == req.Version && strings.HasPrefix(e.key, prefix) {
						_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "matched_key": e.key, "signed_download_url": server.URL + e.id + "?sig=signed-only"})
						return
					}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false})
		default:
			t.Errorf("unknown v2 request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	return server
}

func populateCaches(ctx context.Context, goPath, work, caches, workload string, log io.Writer) ([]commandResult, error) {
	for _, kind := range []string{"modules", "build"} {
		if err := os.MkdirAll(filepath.Join(caches, kind), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(caches, kind, "payload"), []byte(kind+"-payload"), 0644); err != nil {
			return nil, err
		}
	}
	_, _ = io.WriteString(log, "simulated workload, real archives and HTTP cache service\n")
	return []commandResult{{Name: "simulated", ExitCode: 0}}, nil
}

func TestPairedBenchmarkThroughLocalCacheV2(t *testing.T) {
	o := testOptions(t)
	server := cacheServer(t)
	defer server.Close()
	b := &cachebackend.Client{BaseURL: server.URL, Token: "runtime-only", MaxBytes: 1 << 20}
	var calls int
	var restored []bool
	var cachePaths []string
	var workPaths []string
	runner := func(ctx context.Context, goPath, work, caches, workload string, log io.Writer) ([]commandResult, error) {
		_, err := os.Stat(filepath.Join(caches, "build", "payload"))
		restored = append(restored, err == nil)
		cachePaths = append(cachePaths, caches)
		workPaths = append(workPaths, work)
		calls++
		return populateCaches(ctx, goPath, work, caches, workload, log)
	}
	r, err := benchmark(context.Background(), o, b, runner)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 6 || len(r.Seeds) != 2 || len(r.Samples) != 4 {
		t.Fatalf("calls %d seeds %d samples %d", calls, len(r.Seeds), len(r.Samples))
	}
	if got := fmt.Sprint(restored); got != "[false false true false true false]" {
		t.Fatalf("seed/off/on order and restores = %s", got)
	}
	for start := 0; start < 6; start += 3 {
		if cachePaths[start] != cachePaths[start+1] || cachePaths[start] != cachePaths[start+2] || workPaths[start] != workPaths[start+1] || workPaths[start] != workPaths[start+2] {
			t.Fatal("seed and measured absolute paths differ")
		}
	}
	if r.Samples[0].Mode != "off" || r.Samples[1].Mode != "restore-save" || r.Samples[2].Mode != "restore-save" || r.Samples[3].Mode != "off" {
		t.Fatal("paired order did not reverse")
	}
	if r.Seeds[0].Save.Snapshots[0].Key == r.Seeds[1].Save.Snapshots[0].Key {
		t.Fatal("remote namespace reused across pairs")
	}
	firstPrefix := r.Seeds[0].Save.Snapshots[0].Key
	secondMatch := r.Samples[2].Restore.Snapshots[0].MatchedKey
	if firstPrefix == secondMatch {
		t.Fatal("second pair restored first pair's seed")
	}
	for _, s := range r.Samples {
		if !s.Success || !s.Measured || s.TotalMS < 0 {
			t.Fatalf("bad observation %+v", s)
		}
		if s.Mode != "off" {
			if !s.Restore.Snapshots[0].Hit || len(s.Save.Snapshots) == 0 {
				t.Fatal("restore/save not included")
			}
		}
	}
	if !strings.Contains(r.Summary.Status, "correctness only") || r.Summary.Count != 2 || r.Summary.DeltaMedianMS != 0 {
		t.Fatalf("incorrect correctness-mode summary %+v", r.Summary)
	}
}

type fakeBackend struct {
	lookupError, uploadError bool
	uploads                  int
}

func (b *fakeBackend) Lookup(context.Context, string, string, []string) (*cachebackend.Entry, error) {
	if b.lookupError {
		return nil, errors.New("lookup unavailable")
	}
	return nil, nil
}
func (b *fakeBackend) Download(context.Context, cachebackend.Entry, io.Writer) (int64, error) {
	return 0, errors.New("unexpected download")
}
func (b *fakeBackend) Upload(context.Context, string, string, io.ReadSeeker, int64) error {
	b.uploads++
	if b.uploadError {
		return errors.New("upload unavailable")
	}
	return nil
}

func TestSampleRetainsRestoreWorkloadAndSaveFailures(t *testing.T) {
	for _, phase := range []string{"restore", "workload", "save", "missing-warm-hit"} {
		t.Run(phase, func(t *testing.T) {
			o := testOptions(t)
			o.Config.Strict = true
			b := &fakeBackend{lookupError: phase == "restore", uploadError: phase == "save"}
			runner := populateCaches
			if phase == "workload" {
				runner = func(context.Context, string, string, string, string, io.Writer) ([]commandResult, error) {
					return []commandResult{{Name: "failed-build", ExitCode: 9, Error: "compile failed"}}, errors.New("workload failed")
				}
			}
			expect := restoreExpectation{}
			if phase == "missing-warm-hit" {
				expect.Kinds = []string{"build"}
			}
			s := executeSample(context.Background(), 0, 0, "restore-save", "unused", "1.26.0", o.Source, filepath.Join(testDir(t), "caches"), o.Config, b, runner, "small", filepath.Join(testDir(t), "command.log"), false, expect)
			if s.Success || s.Error == "" || s.TotalMS < 0 {
				t.Fatalf("failure missing from sample: %+v", s)
			}
			if phase == "restore" && (len(s.Restore.Snapshots) == 0 || s.Restore.Snapshots[0].Error == "") {
				t.Fatal("restore failure metrics lost")
			}
			if phase == "workload" && (len(s.Commands) != 1 || s.Commands[0].ExitCode != 9) {
				t.Fatal("workload exit code lost")
			}
			if phase == "save" && (len(s.Save.Snapshots) == 0 || s.Save.Snapshots[0].Error == "") {
				t.Fatal("save failure metrics lost")
			}
			if phase != "save" && b.uploads != 0 {
				t.Fatal("uploaded after failed restore/workload")
			}
		})
	}
}

func TestScenarioExpectations(t *testing.T) {
	for _, tc := range []struct {
		expect restoreExpectation
		report cache.Report
		fail   bool
	}{
		{restoreExpectation{Miss: true}, cache.Report{}, false},
		{restoreExpectation{Miss: true}, cache.Report{Snapshots: []cache.Metric{{Kind: "build", Hit: true}}}, true},
		{restoreExpectation{Kinds: []string{"build"}}, cache.Report{Snapshots: []cache.Metric{{Kind: "build", Hit: true}}}, false},
		{restoreExpectation{Kinds: []string{"modules", "build"}}, cache.Report{Snapshots: []cache.Metric{{Kind: "build", Hit: true}}}, true},
	} {
		if err := tc.expect.Check(tc.report); (err != nil) != tc.fail {
			t.Fatalf("expect %+v report %+v err %v", tc.expect, tc.report, err)
		}
	}
}

func TestSummaryRetainsFailedPairsAndPairedDeltas(t *testing.T) {
	var samples []sample
	for i := 0; i < 21; i++ {
		samples = append(samples, sample{Index: i, Mode: "off", Measured: true, Success: true, TotalMS: 100}, sample{Index: i, Mode: "restore-save", Measured: true, Success: i < 20, TotalMS: 110 + float64(i)})
	}
	s := summarize(samples, false)
	if s.Count != 20 || s.Failed != 1 || s.DeltaMedianMS != 19.5 || s.DeltaP95MS != 28 {
		t.Fatalf("summary %+v", s)
	}
	if !strings.Contains(summarize(samples[:38], false).Status, "incomplete") {
		t.Fatal("fewer than 20 pairs treated as complete")
	}
}

func TestBenchmarkEnvironmentDropsCredentialsAndRunnerFiles(t *testing.T) {
	for _, k := range []string{"ACTIONS_RUNTIME_TOKEN", "ACTIONS_RESULTS_URL", "GH_TOKEN", "GITHUB_TOKEN", "GITHUB_ENV", "GITHUB_STATE", "STATE_cache_state", "INPUT_CACHE", "INPUT_CACHE-SCOPE"} {
		t.Setenv(k, "private")
	}
	env := commandEnv("", filepath.Join(testDir(t), "cache"), nil)
	for _, item := range env {
		if strings.HasSuffix(item, "=private") {
			t.Fatalf("secret/runner environment inherited %s", item)
		}
	}
}

func TestGeneratedMonorepoUsesTwoModulesAndLocalDependency(t *testing.T) {
	o := testOptions(t)
	o.Workload = "monorepo"
	dst := filepath.Join(testDir(t), "monorepo")
	if err := prepareFixture(o, dst); err != nil {
		t.Fatal(err)
	}
	goPath, _, err := toolchain(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	commands, err := runWorkload(context.Background(), goPath, dst, filepath.Join(testDir(t), "caches"), "monorepo", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatal(commands)
	}
	if err = mutateSource(dst, "monorepo"); err != nil {
		t.Fatal(err)
	}
	commands, err = runWorkload(context.Background(), goPath, dst, filepath.Join(testDir(t), "mutated-caches"), "monorepo", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatal(commands)
	}
}

func TestNoPerformanceSummaryBelowTwentyPairs(t *testing.T) {
	o := testOptions(t)
	o.Validation = false
	if validateOptions(o) == nil {
		t.Fatal("accepted two performance samples")
	}
	o.Samples = 20
	if err := validateOptions(o); err != nil {
		t.Fatal(err)
	}
	o.Scenario = "go-upgrade"
	if validateOptions(o) == nil {
		t.Fatal("upgrade without seed toolchain accepted")
	}
}
