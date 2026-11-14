package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Keep the shipped surface explicit: runner entrypoints and consumer documentation.
// Contributor guidance and development tools remain in the source repository.
var packagedFiles = []string{
	"action.yml", "launch.sh", "launch.cjs", "post.cjs", "launcher.cjs", "native-download.cjs",
	"experimental/composite/action.yml", "experimental/node/action.yml",
	"experimental/node/launch.cjs", "experimental/node/post.cjs",
	"README.md", "LICENSE", "THIRD_PARTY_NOTICES.txt", "SECURITY.md", "docs/USAGE.md",
	"docs/ARCHITECTURE.md", "docs/COMPARISON.md", "docs/SECURITY_MODEL.md", "docs/TROUBLESHOOTING.md",
	"internal/cachebackend/PROTOCOL.md",
}

var sourceDocuments = []string{
	"CONTRIBUTING.md", "ROADMAP.md", "AGENTS.md", "CLAUDE.md",
	"docs/BENCHMARKS.md", "docs/DEVELOPMENT.md", "docs/RELEASES.md",
}

// sourceDocumentLinks redirects only known development-document destinations.
// Consumer links remain relative and usable in a checkout or extracted bundle.
// For dirty diagnostic builds the revision identifies the base commit, not edits.
func sourceDocumentLinks(data []byte, name, revision string) []byte {
	if !strings.HasSuffix(name, ".md") {
		return data
	}
	text := string(data)
	for _, target := range sourceDocuments {
		rel, err := filepath.Rel(filepath.FromSlash(path.Dir(name)), filepath.FromSlash(target))
		if err != nil {
			continue
		}
		from := "](" + filepath.ToSlash(rel)
		to := "](https://github.com/nicoheidtke/setup-go-faster/blob/" + revision + "/" + target
		// Match the complete path, preserving an optional heading fragment.
		text = strings.ReplaceAll(text, from+")", to+")")
		text = strings.ReplaceAll(text, from+"#", to+"#")
	}
	return []byte(text)
}

// Requiring fresh output avoids retaining files from an older package manifest
// without deleting arbitrary files from a caller-selected directory.
func requireEmptyDirectory(name string) error {
	entries, err := os.ReadDir(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect output %s: %w", name, err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("output directory %s is not empty; choose a new or empty directory", name)
	}
	return nil
}

func validateOutputDirectories(out, archives string) error {
	if err := requireEmptyDirectory(out); err != nil {
		return err
	}
	if archives == "" {
		return nil
	}
	absOut, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	absArchives, err := filepath.Abs(archives)
	if err != nil {
		return err
	}
	// Neither output may contain the other: binaries, ZIPs and checksum files
	// must remain separate, even when both directories do not exist yet.
	for _, pair := range [][2]string{{absOut, absArchives}, {absArchives, absOut}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			continue // Different volumes cannot contain one another.
		}
		if rel == "." || filepath.IsLocal(rel) {
			return fmt.Errorf("distribution and archives directories must be separate, non-nested paths")
		}
	}
	return requireEmptyDirectory(archives)
}
