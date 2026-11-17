package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"ready-setup-go/internal/actionlog"
)

func TestGzipLevelInputsValidateBeforeInstallation(t *testing.T) {
	for _, source := range []string{"flag", "action input"} {
		for _, level := range []string{"0", "-1", "10", "typo"} {
			t.Run(source+"/"+level, func(t *testing.T) {
				t.Setenv("INPUT_CACHE-GZIP-LEVEL", "")
				args := []string{"setup", "--go-version=invalid", "--cache=off", "--summary=false"}
				if source == "flag" {
					args = append(args, "--cache-gzip-level="+level)
				} else {
					t.Setenv("INPUT_CACHE-GZIP-LEVEL", level)
				}
				if err := runArgs(t, args...); err == nil || !strings.Contains(err.Error(), "cache-gzip-level must be default or 1..9") {
					t.Fatalf("invalid level was accepted or reached installation: %v", err)
				}
			})
		}
	}
}

func TestGzipLevelInputsUseInstalledToolchain(t *testing.T) {
	old := actionlog.Default
	actionlog.Default = actionlog.New(io.Discard, false)
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
	for _, name := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "INPUT_GO-VERSION-FILE", "INPUT_CACHE-GZIP-LEVEL"} {
		t.Setenv(name, "")
	}
	for _, source := range []string{"flag", "action input"} {
		for _, level := range []string{"default", "1", "9"} {
			t.Run(source+"/"+level, func(t *testing.T) {
				t.Setenv("INPUT_CACHE-GZIP-LEVEL", "")
				args := []string{"setup", "--go-version=1.26.4", "--cache=off", "--summary=false"}
				if source == "flag" {
					args = append(args, "--cache-gzip-level="+level)
				} else {
					t.Setenv("INPUT_CACHE-GZIP-LEVEL", level)
				}
				if err := runArgs(t, args...); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
