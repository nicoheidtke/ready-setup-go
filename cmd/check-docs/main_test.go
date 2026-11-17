package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, name, text string) string {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestLinks(t *testing.T) {
	root := t.TempDir()
	put(t, root, "docs/target.md", "# Target\n## Build `Go` & test\n## Target\n")
	text := "# Read me\n[good](docs/target.md#build-go--test)\n[duplicate](docs/target.md#target-1)\n[ref][target]\n[target]: docs/target.md#target\n[external](https://example.invalid/absent#nope)\n`[code](missing.md)`\n```md\n[example](missing.md)\n```\n~~~text\n[example](missing.md)\n~~~\n"
	file := put(t, root, "README.md", text)
	if got := checkLinks(root, file, text); len(got) != 0 {
		t.Fatalf("valid links: %v", got)
	}
	for _, tc := range []struct{ name, link, want string }{
		{"missing", "[missing](docs/no.md)", "missing link target"},
		{"heading", "[bad](docs/target.md#nope)", "missing heading"},
		{"reference", "[bad][nope]", "undefined link reference"},
		{"outside", "[bad](../outside.md)", "escapes repository"},
		{"ref-target", "[ref][bad]\n[bad]: missing.md", "missing link target"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := checkLinks(root, file, tc.link)
			if len(got) != 1 || !strings.Contains(got[0], tc.want) {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
		})
	}
}

func TestSourceMarkdownExcludesGeneratedAndFixtureTrees(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"README.md", "docs/design.md", ".github/pull_request_template.md", "internal/cachebackend/PROTOCOL.md", ".cache/report.md", "dist/README.md", "testdata/README.md", "cmd/check-docs/testdata/README.md", "internal/example/testdata/fixture.md"} {
		put(t, root, name, "")
	}
	got, err := sourceMarkdown(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %v", got)
	}
}

const actionFixture = `name: Example
inputs:
  version:
    description: Selected version.
  enabled:
    default: 'false'
  size:
    default: "4"
outputs:
  path:
    description: Result path.
runs:
  using: node24
`
const usageFixture = "## Inputs\n| Input | Default | Behavior |\n| --- | --- | --- |\n| `version` | Empty | Version. |\n| `enabled` | `false` | Optional. |\n| `size` | `4` | Size. |\n\n## Outputs and environment\n| Output | Value |\n| --- | --- |\n| `path` | Path. |\n"

const distributionIntro = "Examples use the `v1` major version tag."
const distributionRecord = "Release provenance is recorded in GitHub Releases."

func TestDistributionExamples(t *testing.T) {
	for _, ref := range []string{"v1", "v1.2.3", strings.Repeat("a", 40)} {
		docs := map[string]string{"docs/USAGE.md": distributionIntro, "README.md": "- uses: nicoheidtke/ready-setup-go@" + ref}
		if got := checkDistributionExamples(docs); len(got) != 0 {
			t.Fatal(got)
		}
	}
	for _, ref := range []string{"main", "dist", "v2", "v1.02.3", "DISTRIBUTION_COMMIT_SHA", "v0.0.0-preview.20261115"} {
		docs := map[string]string{"docs/USAGE.md": distributionIntro, "README.md": "- uses: nicoheidtke/ready-setup-go@" + ref}
		if got := checkDistributionExamples(docs); len(got) == 0 {
			t.Fatal("invalid reference accepted", ref)
		}
	}
	if got := checkDistributionExamples(map[string]string{}); len(got) == 0 {
		t.Fatal("missing major version accepted")
	}
}

func TestReferenceParity(t *testing.T) {
	if got := checkReference(actionFixture, usageFixture); len(got) != 0 {
		t.Fatal(got)
	}
	for _, tc := range []struct{ name, action, usage, want string }{
		{"default", actionFixture, strings.Replace(usageFixture, "`false`", "`true`", 1), "default for enabled"},
		{"missing-input", actionFixture, strings.Replace(usageFixture, "| `enabled` | `false` | Optional. |", "", 1), "undocumented inputs name enabled"},
		{"unknown-output", actionFixture, strings.Replace(usageFixture, "`path`", "`unknown`", 1), "unknown outputs name unknown"},
		{"duplicate-input", strings.Replace(actionFixture, "outputs:", "  enabled:\n    default: 'true'\noutputs:", 1), usageFixture, "duplicate action inputs"},
		{"duplicate-doc", actionFixture, usageFixture + "| `path` | duplicate |\n", "duplicate documented"},
		{"unsupported-default", strings.Replace(actionFixture, "'false'", "|", 1), usageFixture, "unsupported action default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(checkReference(tc.action, tc.usage), "\n")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRepositoryChecksImportAndIntegration(t *testing.T) {
	root := t.TempDir()
	put(t, root, "README.md", "[usage](docs/USAGE.md#inputs)\n")
	put(t, root, "AGENTS.md", "# Guidance\n")
	put(t, root, "CLAUDE.md", "@AGENTS.md\n")
	put(t, root, "action.yml", actionFixture)
	put(t, root, "docs/USAGE.md", distributionIntro+"\n"+usageFixture)
	put(t, root, "docs/DEVELOPMENT.md", distributionRecord)
	if got := checkRepository(root); len(got) != 0 {
		t.Fatal(got)
	}
	put(t, root, "CLAUDE.md", "@agents.md\n")
	if got := strings.Join(checkRepository(root), "\n"); !strings.Contains(got, "only @AGENTS.md") {
		t.Fatal(got)
	}
}
