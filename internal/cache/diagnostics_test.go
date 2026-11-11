package cache

import (
	"context"
	"errors"
	"fmt"
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
		{errors.New("runtime cache credentials unavailable"), "root Node action"},
	} {
		wrapped := fmt.Errorf("restore modules: %w", tc.err)
		if !strings.Contains(DiagnosticHint(wrapped), tc.want) || !errors.Is(wrapped, tc.err) {
			t.Fatalf("incorrect hint or identity: %v", wrapped)
		}
	}
}
