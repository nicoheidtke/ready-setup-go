package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setup-go-faster/internal/actionlog"
)

func TestDebugReportKeepsKeysAndFileDetails(t *testing.T) {
	t.Setenv("RUNNER_DEBUG", "1")
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	old := actionlog.Default
	actionlog.Default = actionlog.New(&out, true)
	t.Cleanup(func() { actionlog.Default = old })
	path := filepath.Join(t.TempDir(), "report.json")
	r := Report{Phase: "restore", Snapshots: []Metric{{Kind: "modules", Key: "full-snapshot-key", Error: "fixture failure"}}}
	if err := writeReport(path, r); err != nil {
		t.Fatal(err)
	}
	var debug string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "::debug::") {
			debug += line
		}
	}
	if !strings.Contains(debug, "full-snapshot-key") || !strings.Contains(debug, "fixture failure") {
		t.Fatalf("wrong debug detail: %q", debug)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "full-snapshot-key") || !strings.Contains(string(b), "fixture failure") {
		t.Fatalf("debug altered report file: %q, %v", b, err)
	}
}

func TestDebugConfigurationExcludesPrivateInputs(t *testing.T) {
	t.Setenv("RUNNER_DEBUG", "1")
	var out bytes.Buffer
	old := actionlog.Default
	actionlog.Default = actionlog.New(&out, true)
	t.Cleanup(func() { actionlog.Default = old })
	c := DefaultConfig()
	c.Scope, c.DependencyPaths, c.Report = "private-scope", "private-manifest-path", "private-report-path"
	debugConfig(c)
	got := out.String()
	if !strings.Contains(got, "cache configuration") || !strings.Contains(got, "gzip") || strings.Contains(got, "private-") {
		t.Fatalf("unexpected debug configuration: %q", got)
	}
}
