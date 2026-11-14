package installer

import "testing"

func FuzzVersionFile(f *testing.F) {
	for _, seed := range []string{"go 1.26\n", "go 1.26.4\ntoolchain go1.27.1\n", "// go 1.99\nmodule \"go 1.99\"\ngo 1.26\n", "go 1.26\nrequire (\n go 1.99\n)\n", "go 1.26\ngo 1.27\n", "go 1.26/*comment*/\n", "go 1.26\ntoolchain default\n", "module `unterminated"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, contents string) {
		// Bound lexer work; ResolveVersionInput separately tests its 1 MiB limit.
		if len(contents) > 16<<10 {
			t.Skip()
		}
		version, err := parseVersionFile(contents)
		if err != nil {
			return
		}
		if _, err := ParseSelector(version); err != nil {
			t.Fatalf("accepted file produced an unsupported selector %q: %v", version, err)
		}
		// Successful extraction must yield a standalone directive with the same
		// meaning, free of tokens borrowed from comments or surrounding syntax.
		again, err := parseVersionFile("go " + version + "\n")
		if err != nil || again != version {
			t.Fatalf("extracted selector does not round trip: %q -> %q, %v", version, again, err)
		}
	})
}
