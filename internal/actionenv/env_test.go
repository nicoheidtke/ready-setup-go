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

func TestLifecycleFieldsAndDistinctFailurePolicies(t *testing.T) {
	for _, name := range []string{"GITHUB_STATE", "GITHUB_ENV", "GITHUB_OUTPUT"} {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, nil, 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, p)
	}
	if err := Environment("SETUP_GO_BETTER_SAVE_ON_FAILURE_action1", "true"); err != nil {
		t.Fatal(err)
	}
	if err := Environment("SETUP_GO_BETTER_SAVE_ON_FAILURE_action2", "false"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(os.Getenv("GITHUB_ENV"))
	if !strings.Contains(string(b), "action1=true\n") || !strings.Contains(string(b), "action2=false\n") {
		t.Fatal(string(b))
	}
	if err := State("cache_state", filepath.Join(t.TempDir(), "state with spaces.json")); err != nil {
		t.Fatal(err)
	}
	if err := Output("cache-hit", "false"); err != nil {
		t.Fatal(err)
	}
	if err := State("bad\nfield", "injected"); err == nil {
		t.Fatal("state name injection accepted")
	}
	if err := Output("cache-hit", "false\nother=true"); err == nil {
		t.Fatal("output value injection accepted")
	}
}
