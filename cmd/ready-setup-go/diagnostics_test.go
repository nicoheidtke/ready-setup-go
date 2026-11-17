package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ready-setup-go/internal/actionlog"
)

func TestParserErrorsAndHelpUseSafeDiagnostics(t *testing.T) {
	for _, help := range []bool{false, true} {
		t.Run(fmt.Sprint(help), func(t *testing.T) {
			var out bytes.Buffer
			oldLog, oldStderr := actionlog.Default, os.Stderr
			actionlog.Default = actionlog.New(&out, true)
			capture, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
			if err != nil {
				t.Fatal(err)
			}
			os.Stderr = capture
			defer func() { actionlog.Default, os.Stderr = oldLog, oldStderr; capture.Close() }()
			t.Setenv("INPUT_GO-VERSION", "1.26.4\n::error::spoof\x1b[31m")
			args := []string{"setup", "--unknown\n::warning::spoof\x1b[31m"}
			if help {
				args = []string{"setup", "--help"}
			}
			err = runArgs(t, args...)
			if help {
				if err != nil || !strings.Contains(out.String(), "Usage of setup:") || !strings.Contains(out.String(), "-go-version") {
					t.Fatalf("missing safe help: %q, %v", out.String(), err)
				}
			} else {
				if err == nil {
					t.Fatal("unknown flag accepted")
				}
				reportError(err)
				if strings.Count(out.String(), "::error title=") != 1 {
					t.Fatalf("parse error not annotated once: %q", out.String())
				}
			}
			if st, _ := capture.Stat(); st.Size() != 0 {
				t.Fatal("flag parser bypassed safe logger")
			}
			if strings.Contains(out.String(), "\n::warning::") || strings.Contains(out.String(), "\n::error::") || strings.Contains(out.String(), "\x1b") {
				t.Fatalf("unsafe parser diagnostic: %q", out.String())
			}
		})
	}
}

func TestFatalDiagnosticsAnnotateMainAndPostOnce(t *testing.T) {
	for _, mode := range []string{"setup", "post"} {
		t.Run(mode, func(t *testing.T) {
			var out bytes.Buffer
			oldLog, oldArgs := actionlog.Default, os.Args
			actionlog.Default = actionlog.New(&out, true)
			os.Args = []string{"ready-setup-go", mode}
			defer func() { actionlog.Default, os.Args = oldLog, oldArgs }()
			reportError(errors.New("failure\n::error::spoof https://blob.test/?sig=private-signature"))
			got := out.String()
			if strings.Count(got, "\n") != 1 || !strings.HasPrefix(got, "::error title=") || strings.Contains(got, "private-signature") || !strings.Contains(got, "Check") {
				t.Fatalf("bad fatal diagnostic: %q", got)
			}
			if mode == "post" && !strings.Contains(got, "Save Go caches") {
				t.Fatal(got)
			}
		})
	}
}
