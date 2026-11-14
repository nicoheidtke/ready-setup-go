package actionlog

import (
	"bytes"
	"strings"
	"testing"
)

type marshalProbe struct{ calls *int }

func (p marshalProbe) MarshalJSON() ([]byte, error) {
	(*p.calls)++
	return []byte(`{"metric":42}`), nil
}

func TestDebugUsesRunnerSignalAndSkipsSerialization(t *testing.T) {
	for _, tc := range []struct {
		name, runner, step, diagnostic string
		actions, enabled               bool
	}{
		{"default", "", "", "", true, false},
		{"runner runtime", "1", "", "", true, true},
		{"step configuration alone", "", "true", "", true, false},
		{"runner diagnostics alone", "", "", "true", true, false},
		{"standalone", "1", "true", "true", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RUNNER_DEBUG", tc.runner)
			t.Setenv("ACTIONS_STEP_DEBUG", tc.step)
			t.Setenv("ACTIONS_RUNNER_DEBUG", tc.diagnostic)
			var out bytes.Buffer
			log := New(&out, tc.actions)
			calls := 0
			log.DebugJSON("metrics", marshalProbe{&calls})
			if log.DebugEnabled() != tc.enabled || (calls == 1) != tc.enabled || (out.Len() > 0) != tc.enabled {
				t.Fatalf("debug gate: enabled=%v calls=%d output=%q", log.DebugEnabled(), calls, out.String())
			}
		})
	}
}

func TestDebugEscapesCommandsAndRedactsCredentials(t *testing.T) {
	t.Setenv("RUNNER_DEBUG", "1")
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "private-runtime-token")
	t.Setenv("GITHUB_TOKEN", "private-github-token")
	var out bytes.Buffer
	New(&out, true).Debugf("failure%%\r\n::error:: https://blob.invalid/cache?sig=private-signature private-runtime-token private-github-token")
	got := out.String()
	if !strings.HasPrefix(got, "::debug::failure%25%0D%0A::error::") || strings.Count(got, "\n") != 1 {
		t.Fatalf("unsafe workflow command: %q", got)
	}
	for _, secret := range []string{"private-runtime-token", "private-github-token", "private-signature", "https://blob.invalid"} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential leaked into debug output: %q", got)
		}
	}
}
