package cachearchive

import (
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzPortablePath(f *testing.F) {
	for _, seed := range []string{"modules/example.com/a@v1.0.0/go.mod", "build/ab/hash-d", "../outside", "/absolute", "modules/../outside", "modules/CON.txt", "modules/a\\b", "modules/a:b", "modules/a\x00b", "modules/long/../path", "modules/K", "modules/K"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if len(name) > 4096 {
			t.Skip()
		}
		if portable(name) == nil {
			// Accepted archive names must remain local on the native platform
			// and canonical under the archive's slash-based path semantics.
			if !filepath.IsLocal(filepath.FromSlash(name)) || path.Clean(name) != name || strings.ContainsAny(name, "\\:\x00") {
				t.Fatalf("accepted an escaping or ambiguous archive path %q", name)
			}
			if err := portable("modules/" + name); err != nil {
				t.Fatalf("safe path became unsafe below a safe root: %q: %v", name, err)
			}
		}
		// Case collision detection must be stable, including Unicode fold cycles.
		if key := caseKey(name); caseKey(key) != key {
			t.Fatalf("case folding is not idempotent for %q", name)
		}
	})
}
