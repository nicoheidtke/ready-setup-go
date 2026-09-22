package actionenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExport(t *testing.T) {
	paths := map[string]string{}
	for _, name := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT"} {
		p := filepath.Join(t.TempDir(), name)
		if e := os.WriteFile(p, nil, 0600); e != nil {
			t.Fatal(e)
		}
		t.Setenv(name, p)
		paths[name] = p
	}
	t.Setenv("GOTOOLCHAIN", "custom+auto")
	root := filepath.Join(t.TempDir(), "Go with spaces")
	if e := Export("1.26.2", root); e != nil {
		t.Fatal(e)
	}
	for name, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(b), root) {
			t.Errorf("%s: %q", name, b)
		}
	}
	if os.Getenv("GOTOOLCHAIN") != "custom+auto" {
		t.Fatal("changed toolchain policy")
	}
	if e := Export("1.26.2", root+"\nINJECT=true"); e == nil {
		t.Fatal("accepted newline injection")
	}
}
