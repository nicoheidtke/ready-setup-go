package cache

import (
	"context"
	"errors"

	"ready-setup-go/internal/actionlog"
	"ready-setup-go/internal/cachebackend"
)

// ErrCredentialsUnavailable identifies missing runner cache credentials.
var ErrCredentialsUnavailable = errors.New("runtime cache credentials unavailable; use the Node action on a cache-v2-capable GitHub runner")

// ErrPostStateUnavailable identifies a runner without the automatic post bridge.
var ErrPostStateUnavailable = errors.New("GITHUB_STATE unavailable; post-job save requires the Node action")

func warning(err error) {
	actionlog.Default.Warning("Go cache", err.Error()+" "+DiagnosticHint(err))
}
func failure(c Config, err error) error {
	if c.Strict {
		return err
	}
	warning(err)
	return nil
}

// DiagnosticHint adds user guidance without replacing the original error or
// weakening strict/non-strict policy. Ordinary misses never reach this path.
func DiagnosticHint(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "The operation was cancelled; no retry is needed for a cancelled job."
	case errors.Is(err, context.DeadlineExceeded):
		return "The operation timed out; check runner connectivity and cache size."
	case errors.Is(err, cachebackend.ErrDNS):
		return "Check runner DNS resolution and proxy configuration."
	case errors.Is(err, cachebackend.ErrTLS):
		return "Check runner certificate trust, clock and HTTPS proxy configuration; keep TLS verification enabled."
	case errors.Is(err, cachebackend.ErrConnection):
		return "Check runner network access, firewall and proxy configuration."
	case errors.Is(err, cachebackend.ErrPermission):
		return "Check the runner's cache access policy; use cache: restore when writes are unavailable."
	case errors.Is(err, cachebackend.ErrConflict):
		return "Another writer may own this immutable entry; a conflict does not confirm a usable saved snapshot."
	case errors.Is(err, ErrCredentialsUnavailable):
		return "Use the root Node action on a cache-v2-capable GitHub runner; do not supply runtime tokens manually."
	case errors.Is(err, ErrPostStateUnavailable):
		return "Automatic post-save requires the root Node action; the Bash composite installs Go only."
	default:
		return "Check the operation details with GitHub step debug logging; cache: off disables caching."
	}
}
