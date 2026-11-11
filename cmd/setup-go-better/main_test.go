package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"setup-go-better/internal/actionlog"
	"strings"
	"testing"
)

func TestStageBalanceOnVersionError(t *testing.T) {
	var out bytes.Buffer
	old := actionlog.Default
	actionlog.Default = actionlog.New(&out, true)
	defer func() { actionlog.Default = old }()
	summary := filepath.Join(t.TempDir(), "summary")
	if err := os.WriteFile(summary, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	if err := runArgs(t, "setup", "--go-version=invalid", "--cache=off"); err == nil {
		t.Fatal("invalid selector accepted")
	}
	if got := out.String(); got != "::group::Resolve Go version\n::endgroup::\n" {
		t.Fatalf("failure left a group open: %q", got)
	}
	if b, err := os.ReadFile(summary); err != nil || !strings.Contains(string(b), "ERROR") || !strings.Contains(string(b), "invalid Go version") {
		t.Fatalf("setup failure missing from summary: %s, %v", b, err)
	}
}

func TestInstalledToolchainStagesStaySequential(t *testing.T) {
	var out bytes.Buffer
	old := actionlog.Default
	actionlog.Default = actionlog.New(&out, true)
	defer func() { actionlog.Default = old }()
	root := t.TempDir()
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	tool := filepath.Join(root, "go", "1.26.4", arch)
	if err := os.MkdirAll(filepath.Join(tool, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	exe := "go"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	for path, data := range map[string]string{filepath.Join(tool, "VERSION"): "go1.26.4", filepath.Join(tool, "bin", exe): "fixture"} {
		if err := os.WriteFile(path, []byte(data), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("RUNNER_TOOL_CACHE", root)
	summary := filepath.Join(t.TempDir(), "summary")
	if err := os.WriteFile(summary, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	for _, name := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "INPUT_GO-VERSION-FILE"} {
		t.Setenv(name, "")
	}
	if err := runArgs(t, "setup", "--go-version=1.26.4", "--cache=off"); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "::group::Resolve Go version\n::endgroup::\n::group::Prepare Go toolchain\n") || !strings.HasSuffix(got, "::endgroup::\nCache SKIP · caching disabled\n") || strings.Count(got, "::endgroup::") != 2 {
		t.Fatalf("unexpected group sequence: %q", got)
	}
	b, err := os.ReadFile(summary)
	if err != nil || !strings.Contains(string(b), "Go toolchain") || !strings.Contains(string(b), "1.26.4") || !strings.Contains(string(b), "caching disabled") {
		t.Fatalf("missing main summary: %s, %v", b, err)
	}
	if err = runArgs(t, "setup", "--go-version=1.26.4", "--cache=off", "--summary=false"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(summary); string(after) != string(b) {
		t.Fatal("summary opt-out appended content")
	}
}

func runArgs(t *testing.T, args ...string) error {
	t.Helper()
	old := os.Args
	os.Args = append([]string{"setup-go-better"}, args...)
	defer func() { os.Args = old }()
	return run()
}

func TestCacheInputErrorsPrecedeInstallation(t *testing.T) {
	for _, tc := range []struct{ name, args, want string }{
		{"mode", "--cache=invalid", "cache must be"},
		{"storage", "--cache-storage=invalid", "cache-storage"},
		{"overlap", "--cache-overlap=invalid", "cache-overlap"},
		{"layout", "--cache-layout=invalid", "cache-layout"},
		{"compression", "--cache-compression=invalid", "cache-compression"},
		{"strict", "--cache-strict=typo", "cache-strict"},
		{"summary", "--summary=typo", "summary"},
		{"failure", "--cache-save-on-failure=typo", "cache-save-on-failure"},
		{"age", "--cache-build-max-age=-1h", "nonnegative"},
		{"bytes", "--cache-max-bytes=0", "max bytes"},
		{"workers", "--cache-upload-concurrency=33", "concurrency"},
		{"download workers", "--cache-download-concurrency=33", "concurrency"},
		{"download worker parse", "--cache-download-concurrency=typo", "cache-download-concurrency"},
		{"download block", "--cache-download-chunk-bytes=0", "chunk bytes"},
		{"download block parse", "--cache-download-chunk-bytes=typo", "cache-download-chunk-bytes"},
		{"chunk workers", "--cache-chunk-concurrency=17", "cache-chunk-concurrency"},
		{"compression workers", "--cache-compression-concurrency=17", "cache-compression-concurrency"},
		{"compression worker parse", "--cache-compression-concurrency=typo", "cache-compression-concurrency"},
		{"restore workers", "--cache-restore-concurrency=0", "cache-restore-concurrency"},
		{"restore worker parse", "--cache-restore-concurrency=typo", "cache-restore-concurrency"},
		{"chunk worker parse", "--cache-chunk-concurrency=typo", "cache-chunk-concurrency"},
		{"chunk", "--cache-upload-chunk-bytes=0", "chunk bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runArgs(t, "setup", "--go-version=1.26.8", tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: %v", tc.args, err)
			}
		})
	}
}

func TestPostNoStateAndArguments(t *testing.T) {
	t.Setenv("STATE_cache_state", "")
	if err := runArgs(t, "post"); err != nil {
		t.Fatal(err)
	}
	if err := runArgs(t, "post", "extra"); err == nil {
		t.Fatal("post silently ignored arguments")
	}
}

func TestDownloadActionInputsValidateBeforeInstallation(t *testing.T) {
	for _, input := range []string{"INPUT_CACHE-DOWNLOAD-CONCURRENCY", "INPUT_CACHE-DOWNLOAD-CHUNK-BYTES"} {
		t.Run(input, func(t *testing.T) {
			t.Setenv(input, "invalid")
			if err := runArgs(t, "setup", "--go-version=1.26.8"); err == nil || !strings.Contains(err.Error(), "cache-download-") {
				t.Fatalf("download input was ignored: %v", err)
			}
		})
	}
}

func TestVersionFileInputErrorsPrecedeInstallation(t *testing.T) {
	t.Setenv("INPUT_GO-VERSION", "")
	t.Setenv("INPUT_GO-VERSION-FILE", "missing-version-file/go.mod")
	err := runArgs(t, "setup", "--cache=off")
	if err == nil || !strings.Contains(err.Error(), "go-version-file") {
		t.Fatalf("file input was ignored: %v", err)
	}
	// An explicit version takes precedence even when the file does not exist.
	// Invalid selector validation fails without a network call.
	err = runArgs(t, "setup", "--go-version=invalid", "--cache=off")
	if err == nil || !strings.Contains(err.Error(), "invalid Go version") {
		t.Fatalf("explicit selector did not take precedence: %v", err)
	}
}

func TestZstdCLIAndActionInputAcceptedBeforeVersionResolution(t *testing.T) {
	for _, source := range []string{"flag", "action input"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("INPUT_CACHE-COMPRESSION", "")
			args := []string{"setup", "--go-version=invalid", "--cache=off"}
			if source == "flag" {
				args = append(args, "--cache-compression=zstd")
			} else {
				t.Setenv("INPUT_CACHE-COMPRESSION", "zstd")
			}
			if err := runArgs(t, args...); err == nil || !strings.Contains(err.Error(), "invalid Go version") {
				t.Fatalf("zstd input rejected before version resolution: %v", err)
			}
		})
	}
}
