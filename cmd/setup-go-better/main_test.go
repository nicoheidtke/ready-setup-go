package main

import (
	"os"
	"strings"
	"testing"
)

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
		{"failure", "--cache-save-on-failure=typo", "cache-save-on-failure"},
		{"age", "--cache-build-max-age=-1h", "nonnegative"},
		{"bytes", "--cache-max-bytes=0", "max bytes"},
		{"workers", "--cache-upload-concurrency=33", "concurrency"},
		{"download workers", "--cache-download-concurrency=33", "concurrency"},
		{"download worker parse", "--cache-download-concurrency=typo", "cache-download-concurrency"},
		{"download block", "--cache-download-chunk-bytes=0", "chunk bytes"},
		{"download block parse", "--cache-download-chunk-bytes=typo", "cache-download-chunk-bytes"},
		{"chunk workers", "--cache-chunk-concurrency=17", "cache-chunk-concurrency"},
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
