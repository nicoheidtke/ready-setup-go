package main

import (
	"os"
	"strings"
	"testing"
)

func TestHelpSucceedsWithoutSourceOrPublication(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"plan", "--root", "missing-source-checkout", "--help"}} {
		func() {
			oldArgs, oldOut, oldErr := os.Args, os.Stdout, os.Stderr
			defer func() { os.Args, os.Stdout, os.Stderr = oldArgs, oldOut, oldErr }()
			f, err := os.CreateTemp(t.TempDir(), "help")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			os.Args = append([]string{"release"}, args...)
			os.Stdout, os.Stderr = f, f
			if err := run(); err != nil {
				t.Fatalf("help failed: %v", err)
			}
			b, err := os.ReadFile(f.Name())
			if err != nil || !strings.Contains(string(b), "plan") {
				t.Fatalf("missing useful help: %q %v", b, err)
			}
		}()
	}
}
