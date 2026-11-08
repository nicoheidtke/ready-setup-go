package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFileDirectives(t *testing.T) {
	for _, tc := range []struct{ name, contents, want, failure string }{
		{"minor", "module example.com/m\ngo 1.26\n", "1.26", ""},
		{"patch", "go 1.26.4", "1.26.4", ""},
		{"preferred", "go 1.26.4\ntoolchain go1.27.1\n", "1.27.1", ""},
		{"older language minimum", "go 1.25.0\ntoolchain go1.26.4\n", "1.26.4", ""},
		{"same preferred", "go 1.26.4\ntoolchain go1.26.4\n", "1.26.4", ""},
		{"default", "go 1.26.4\ntoolchain default\n", "1.26.4", ""},
		{"work", "go 1.26.4\nuse (\n ./one\n ./two\n)\n", "1.26.4", ""},
		{"comments and CRLF", "// go 1.99.0\r\nmodule \"toolchain/go1.99.0\"\r\ngo\t1.26.4 // toolchain go1.99.0\r\n", "1.26.4", ""},
		{"quoted version", "go \"1.26.4\"\n", "1.26.4", ""},
		{"ignored block", "require (\n go 1.99.0\n)\ngo 1.26.4\n", "1.26.4", ""},
		{"comment after version", "go 1.26.4/* ignored */\n", "1.26.4", ""},
		{"missing", "module example.com/m\n", "", "missing go directive"},
		{"only preferred", "toolchain go1.26.4\n", "", "missing go directive"},
		{"old go", "go 1.25.0\n", "", "unsupported go directive"},
		{"wildcard", "go 1.26.x\n", "", "unsupported go directive"},
		{"prerelease", "go 1.26rc1\n", "", "unsupported go directive"},
		{"duplicate go", "go 1.26.0\ngo 1.27.0\n", "", "duplicate go"},
		{"duplicate preferred", "go 1.26.0\ntoolchain default\ntoolchain go1.27.1\n", "", "duplicate toolchain"},
		{"extra tokens", "go 1.26.0 extra\n", "", "invalid or duplicate go"},
		{"empty version", "go\n", "", "invalid or duplicate go"},
		{"lower preferred", "go 1.26.4\ntoolchain go1.26.3\n", "", "older than go"},
		{"minor preferred", "go 1.26\ntoolchain go1.26\n", "", "unsupported toolchain"},
		{"custom preferred", "go 1.26\ntoolchain custom\n", "", "unsupported toolchain"},
		{"bad prefix", "go 1.26\ntoolchain 1.26.4\n", "", "unsupported toolchain"},
		{"unterminated string", "module \"bad\ngo 1.26.0\n", "", "invalid file"},
		{"unbalanced block", "go 1.26.0\nrequire (\n", "", "unbalanced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVersionFile(tc.contents)
			if tc.failure != "" {
				if err == nil || !strings.Contains(err.Error(), tc.failure) {
					t.Fatalf("got %q, %v; want error containing %q", got, err, tc.failure)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
			selector, err := ParseSelector(got)
			if err != nil || selector.Exact != (strings.Count(got, ".") == 2) {
				t.Fatalf("unexpected resolved selector: %+v, %v", selector, err)
			}
		})
	}
}

func TestResolveVersionInput(t *testing.T) {
	workspace := t.TempDir()
	file := filepath.Join(workspace, "nested module", "go.mod")
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("module example.com/m\ngo 1.26.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, fileInput := range []string{file, filepath.Join("nested module", "go.mod")} {
		got, err := ResolveVersionInput("", fileInput, workspace)
		if err != nil || got != "1.26.4" {
			t.Fatalf("file %q: %q, %v", fileInput, got, err)
		}
	}
	// An explicit input must not stat/read a nonexistent file.
	if got, err := ResolveVersionInput(" 1.27.1 ", "missing", workspace); err != nil || got != "1.27.1" {
		t.Fatalf("explicit input: %q, %v", got, err)
	}
	if _, err := ResolveVersionInput("", "missing", workspace); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := ResolveVersionInput("", "", workspace); err == nil {
		t.Fatal("missing inputs accepted")
	}
	t.Chdir(workspace)
	if got, err := ResolveVersionInput("", filepath.Join("nested module", "go.mod"), ""); err != nil || got != "1.26.4" {
		t.Fatalf("standalone cwd: %q, %v", got, err)
	}
	if err := os.WriteFile(file, []byte(strings.Repeat(" ", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveVersionInput("", file, ""); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize file: %v", err)
	}
}
