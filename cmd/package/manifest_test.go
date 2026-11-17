package main

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPackagedDocumentationLinks(t *testing.T) {
	included := make(map[string]bool)
	for _, name := range packagedFiles {
		if included[name] {
			t.Fatalf("duplicate packaged file %s", name)
		}
		included[name] = true
	}
	for _, name := range sourceDocuments {
		if included[name] {
			t.Errorf("development-only file packaged: %s", name)
		}
	}
	links := regexp.MustCompile(`\]\(([^)]+)\)`)
	for _, name := range packagedFiles {
		data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		for _, match := range links.FindAllSubmatch(sourceDocumentLinks(data, name, "reviewed-source"), -1) {
			target := strings.SplitN(string(match[1]), "#", 2)[0]
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if !included[path.Clean(path.Join(path.Dir(name), target))] {
				t.Errorf("packaged %s links to absent %s", name, target)
			}
		}
	}
}

func TestSourceDocumentLinks(t *testing.T) {
	text := "[dev](DEVELOPMENT.md#validation) [usage](USAGE.md) [contributing](../CONTRIBUTING.md) [other](DEVELOPMENT.md.backup)"
	got := string(sourceDocumentLinks([]byte(text), "docs/USAGE.md", "abc123"))
	want := "[dev](https://github.com/nicoheidtke/ready-setup-go/blob/abc123/docs/DEVELOPMENT.md#validation) [usage](USAGE.md) [contributing](https://github.com/nicoheidtke/ready-setup-go/blob/abc123/CONTRIBUTING.md) [other](DEVELOPMENT.md.backup)"
	if got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
	if got := string(sourceDocumentLinks([]byte(text), "action.yml", "abc123")); got != text {
		t.Fatal("rewrote a non-Markdown file")
	}
}

func TestOutputDirectoriesRejectStaleFilesWithoutRemovingThem(t *testing.T) {
	for _, staleInAssets := range []bool{false, true} {
		t.Run(map[bool]string{false: "distribution", true: "assets"}[staleInAssets], func(t *testing.T) {
			root := t.TempDir()
			out, assets := filepath.Join(root, "dist"), filepath.Join(root, "assets")
			stale := out
			if staleInAssets {
				stale = assets
			}
			if err := os.MkdirAll(stale, 0755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(stale, "old-report.md")
			if err := os.WriteFile(file, []byte("retain"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := validateOutputDirectories(out, assets); err == nil || !strings.Contains(err.Error(), "not empty") {
				t.Fatalf("expected stale-output error, got %v", err)
			}
			if data, err := os.ReadFile(file); err != nil || string(data) != "retain" {
				t.Fatalf("existing file changed: %q, %v", data, err)
			}
		})
	}
}

func TestOutputDirectoriesMustBeSeparate(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "dist")
	for _, assets := range []string{out, filepath.Join(out, "assets"), root} {
		if err := validateOutputDirectories(out, assets); err == nil {
			t.Errorf("accepted overlapping outputs %q and %q", out, assets)
		}
	}
	if err := validateOutputDirectories(out, filepath.Join(root, "assets")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(out, 0755); err != nil {
		t.Fatal(err)
	}
	if err := validateOutputDirectories(out, ""); err != nil {
		t.Fatalf("rejected existing empty output: %v", err)
	}
}
