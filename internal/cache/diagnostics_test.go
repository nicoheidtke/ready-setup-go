package cache

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"setup-go-better/internal/cachebackend"
)

func TestDiagnosticHintsRetainPolicyAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{cachebackend.ErrPermission, "cache: restore"},
		{cachebackend.ErrConflict, "does not confirm"},
		{context.Canceled, "cancelled"},
		{context.DeadlineExceeded, "timed out"},
		{ErrCredentialsUnavailable, "root Node action"},
		{ErrPostStateUnavailable, "Automatic post"},
	} {
		wrapped := fmt.Errorf("restore modules: %w", tc.err)
		if !strings.Contains(DiagnosticHint(wrapped), tc.want) || !errors.Is(wrapped, tc.err) {
			t.Fatalf("incorrect hint or identity: %v", wrapped)
		}
	}
}

func TestDiagnosticHintDoesNotClassifyUnrelatedErrorText(t *testing.T) {
	for _, message := range []string{"credentials unavailable in another service", "cannot write GITHUB_STATE"} {
		if hint := DiagnosticHint(errors.New(message)); !strings.Contains(hint, "step debug logging") {
			t.Fatalf("classified unrelated error %q: %s", message, hint)
		}
	}
}

func TestBackendMissingCredentialsIdentity(t *testing.T) {
	for _, missing := range []string{"ACTIONS_RESULTS_URL", "ACTIONS_RUNTIME_TOKEN"} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv("ACTIONS_RESULTS_URL", "https://example.invalid")
			t.Setenv("ACTIONS_RUNTIME_TOKEN", "test-token")
			t.Setenv(missing, "")
			_, err := newBackend(DefaultConfig())
			if !errors.Is(err, ErrCredentialsUnavailable) {
				t.Fatalf("missing credential lost error identity: %v", err)
			}
		})
	}
}

func TestMissingPostStateIdentity(t *testing.T) {
	t.Setenv("GITHUB_STATE", "")
	output := filepath.Join(t.TempDir(), "outputs")
	put(t, output, "")
	t.Setenv("GITHUB_OUTPUT", output)
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Setenv("ACTIONS_CACHE_MODE", "")
	t.Setenv("GOCACHE", filepath.Join(t.TempDir(), "build"))
	t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "modules"))
	c := DefaultConfig()
	c.Mode, c.Strict = "restore-save", true
	pending := &Prefetch{done: make(chan struct{}), state: state(t, c, "deps", roots(t)), backend: &memoryBackend{}}
	close(pending.done)
	err := StartPrefetched(context.Background(), c, "1.26.8", runtime.GOROOT(), pending)
	if !errors.Is(err, ErrPostStateUnavailable) {
		t.Fatalf("missing post bridge lost error identity: %v", err)
	}
}
