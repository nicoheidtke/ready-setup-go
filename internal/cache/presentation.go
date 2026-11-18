package cache

import (
	"fmt"
	"strings"

	"ready-setup-go/internal/actionlog"
)

// logReport renders operational results rather than treating a cache lookup as
// evidence that the complete workload is warm. Keys identify lookup candidates,
// matches and save targets; the result establishes whether transfer succeeded.
func logReport(log *actionlog.Logger, r Report) {
	if r.SkippedReason != "" {
		log.Printf("Cache %s %s · %s\n", logText(r.Phase), log.Status("SKIP"), logText(r.SkippedReason))
	}
	hits := 0
	for _, m := range r.Snapshots {
		status, reason := snapshotResult(r.Phase, m)
		if m.Hit {
			hits++
		}
		log.Printf("  %s %s · %s · %s · %s\n", logText(m.Kind), log.Status(status),
			logText(reason), actionlog.Bytes(m.Bytes), actionlog.Duration(m.LookupMS+m.TransferMS+m.ArchiveMS))
		if m.Key != "" {
			label := "Save key"
			if r.Phase == "restore" {
				label = "Lookup key"
			}
			log.Printf("    %s: %s\n", label, logText(m.Key))
		}
		if r.Phase == "restore" && m.MatchedKey != "" {
			log.Printf("    Matched key: %s\n", logText(m.MatchedKey))
		}
	}
	if len(r.Snapshots) == 0 {
		return
	}
	if r.Phase == "restore" {
		outcome := "miss"
		if hits == len(r.Snapshots) {
			outcome = "complete"
		} else if hits > 0 {
			outcome = "partial"
		}
		log.Printf("Cache restore %s · %d/%d snapshots restored · %s\n", outcome, hits, len(r.Snapshots), actionlog.Duration(r.TotalMS))
	} else {
		log.Printf("Cache %s finished · %s\n", logText(r.Phase), actionlog.Duration(r.TotalMS))
	}
}

func snapshotResult(phase string, m Metric) (status, reason string) {
	if m.Error != "" {
		if m.RestoreReason == "access-denied" {
			return "ERROR", "cache access denied"
		}
		return "ERROR", m.Error
	}
	if phase == "restore" {
		if m.Hit {
			if m.MatchType == "dependency-compatible" {
				return "HIT", "compatible dependency snapshot"
			}
			return "HIT", "matching dependency snapshot"
		}
		if m.RestoreReason == "missing-chunk" {
			return "MISS", "snapshot chunk missing or evicted; nothing published"
		}
		return "MISS", "no matching snapshot"
	}
	if m.Skipped {
		switch m.SaveReason {
		case "dependency-match":
			return "SKIP", "matching dependency baseline restored"
		case "unchanged-module-content":
			return "SKIP", "module content unchanged"
		case "build-save-disabled":
			return "SKIP", "build save disabled by input"
		case "empty-cache":
			return "SKIP", "cache is empty"
		default:
			return "SKIP", m.SaveReason
		}
	}
	if m.ReusedChunks > 0 {
		return "SAVED", fmt.Sprintf("fresh snapshot; %d chunks reused (%s)", m.ReusedChunks, actionlog.Bytes(m.ReusedBytes))
	}
	if m.SaveReason == "module-content-or-mode-changed" {
		return "SAVED", "module content or permissions changed"
	}
	return "SAVED", "fresh snapshot"
}

// Plain results are single lines even when filesystem/backend errors contain
// untrusted names. The fixed text prefix also keeps them out of command syntax.
func logText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}
