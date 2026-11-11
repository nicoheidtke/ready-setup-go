package actionlog

import (
	"bytes"
	"strings"
	"testing"
)

func TestColorPolicyAndKnownLabels(t *testing.T) {
	for _, tc := range []struct {
		name    string
		actions bool
		noColor string
		color   bool
	}{
		{"actions", true, "", true},
		{"standalone", false, "", false},
		{"disabled", true, "1", false},
		{"nonempty-zero-disables", true, "0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tc.noColor)
			var out bytes.Buffer
			l := New(&out, tc.actions)
			for _, label := range []string{"READY", "HIT", "SAVED", "WARNING", "ERROR"} {
				got := string(l.Status(label))
				if strings.Contains(got, "\x1b[") != tc.color || !strings.Contains(got, label) {
					t.Fatalf("%s = %q, color %v", label, got, tc.color)
				}
				if tc.color && !strings.HasSuffix(got, label+"\x1b[0m") {
					t.Fatalf("label color not reset: %q", got)
				}
			}
			for _, label := range []string{"MISS", "SKIP", "unknown", "::warning::message"} {
				if got := string(l.Status(label)); got != label {
					t.Fatalf("neutral/unknown label changed: %q", got)
				}
			}
		})
	}
}

func TestColorDoesNotWrapWorkflowCommands(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var out bytes.Buffer
	l := New(&out, true)
	end := l.Stage("Prepare toolchain")
	l.Printf("Go %s · ready\n", l.Status("READY"))
	end()
	want := "::group::Prepare toolchain\nGo \x1b[32mREADY\x1b[0m · ready\n::endgroup::\n"
	if out.String() != want {
		t.Fatalf("workflow commands or color boundaries changed: %q", out.String())
	}
}
