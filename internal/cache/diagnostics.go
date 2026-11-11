package cache

import (
	"context"
	"errors"
	"strings"

	"setup-go-better/internal/cachebackend"
)

// DiagnosticHint adds user guidance without replacing the original error or
// weakening strict/non-strict policy. Ordinary misses never reach this path.
func DiagnosticHint(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "The operation was cancelled; no retry is needed for a cancelled job."
	case errors.Is(err, context.DeadlineExceeded):
		return "The operation timed out; check runner connectivity and cache size."
	case errors.Is(err, cachebackend.ErrPermission):
		return "Check the runner's cache access policy; use cache: restore when writes are unavailable."
	case errors.Is(err, cachebackend.ErrConflict):
		return "Another writer may own this immutable entry; a conflict does not confirm a usable saved snapshot."
	case strings.Contains(err.Error(), "credentials unavailable"):
		return "Use the root Node action on a cache-v2-capable GitHub runner; do not supply runtime tokens manually."
	case strings.Contains(err.Error(), "GITHUB_STATE"):
		return "Automatic post-save requires the root Node action; the Bash composite installs Go only."
	default:
		return "Check the operation details with GitHub step debug logging; cache: off disables caching."
	}
}
