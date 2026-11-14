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

const distributionPin = "1111111111111111111111111111111111111111"
const distributionRelease = "v0.0.0-preview.20261111.3"
const distributionIntro = "Examples pin bundled preview `" + distributionPin + "` (`" + distributionRelease + "`)"
const distributionRecord = "| Release | [" + distributionRelease + "](https://github.com/nicoheidtke/setup-go-faster/releases/tag/" + distributionRelease + ") |\n| Distribution commit | `" + distributionPin + "` |\n| Source revision | `2222222222222222222222222222222222222222` |\n"

func TestDistributionExamples(t *testing.T) {
	valid := map[string]string{
		"docs/DEVELOPMENT.md": distributionRecord,
		"docs/USAGE.md":       distributionIntro,
		"README.md":           "```yaml\n- uses: nicoheidtke/setup-go-faster@" + distributionPin + "\n```\n",
	}
	if got := checkDistributionExamples(valid); len(got) != 0 {
		t.Fatal(got)
	}
	for _, tc := range []struct{ name, file, text, want string }{
		{"old-example", "README.md", "- uses: nicoheidtke/setup-go-faster@3333333333333333333333333333333333333333", "README.md:1: action example must pin"},
		{"mutable-example", "docs/SECURITY_MODEL.md", "- uses: 'nicoheidtke/setup-go-faster@main' # example", "action example must pin"},
		{"missing-record", "docs/DEVELOPMENT.md", "", "require recommended"},
		{"source-not-sha", "docs/DEVELOPMENT.md", strings.ReplaceAll(distributionRecord, "2222222222222222222222222222222222222222", "main"), "require recommended"},
		{"tag-link-drift", "docs/DEVELOPMENT.md", strings.Replace(distributionRecord, "/tag/"+distributionRelease, "/tag/v0.0.0-preview.old", 1), "require recommended"},
		{"duplicate-record", "docs/DEVELOPMENT.md", distributionRecord + distributionRecord, "duplicate recommended"},
		{"usage-tag-drift", "docs/USAGE.md", strings.Replace(distributionIntro, distributionRelease, "v0.0.0-preview.old", 1), "example distribution SHA/tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			docs := map[string]string{}
			for file, contents := range valid {
				docs[file] = contents
			}
			docs[tc.file] = tc.text
			if got := strings.Join(checkDistributionExamples(docs), "\n"); !strings.Contains(got, tc.want) {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	// Source reference tables may grow before publication. Pin consistency must
	// not incorrectly assert that source inputs exist in the published binary.
	valid["docs/USAGE.md"] += "\n| `future-source-input` | Empty | Requires a new distribution. |\n"
	if got := checkDistributionExamples(valid); len(got) != 0 {
		t.Fatal(got)
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

func TestUnpublishedDistributionExamples(t *testing.T) {
	docs := map[string]string{
		"docs/DEVELOPMENT.md": "| Release | Not published |\n| Distribution commit | Not published |\n| Source revision | Not published |\n",
		"docs/USAGE.md":       "No distribution is published yet.",
		"README.md":           "- uses: nicoheidtke/setup-go-faster@DISTRIBUTION_COMMIT_SHA",
	}
	if got := checkDistributionExamples(docs); len(got) != 0 {
		t.Fatal(got)
	}
	for _, pin := range []string{"main", distributionPin} {
		docs["README.md"] = "- uses: nicoheidtke/setup-go-faster@" + pin
		if got := checkDistributionExamples(docs); len(got) == 0 {
			t.Fatalf("unpublished record accepted pin %s", pin)
		}
	}
	docs["docs/DEVELOPMENT.md"] = strings.Replace(docs["docs/DEVELOPMENT.md"], "| Release | Not published |", "| Release | missing |", 1)
	if got := checkDistributionExamples(docs); len(got) == 0 {
		t.Fatal("accepted inconsistent publication metadata")
	}
}
