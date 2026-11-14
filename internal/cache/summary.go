package cache

import (
	"fmt"

	"setup-go-faster/internal/actionlog"
)

func summaryReport(c Config, r Report, savePolicy string) {
	rows := make([]actionlog.SummaryRow, 0, len(r.Snapshots))
	hits := 0
	for _, m := range r.Snapshots {
		status, reason := snapshotResult(r.Phase, m)
		rows = append(rows, actionlog.SummaryRow{m.Kind, status, reason, actionlog.Bytes(m.Bytes), actionlog.Duration(m.LookupMS + m.TransferMS + m.ArchiveMS)})
		if m.Hit {
			hits++
		}
	}
	notes := []string{fmt.Sprintf("Cache mode: %s; storage: %s; layout: %s; compression: %s.", c.Mode, c.Storage, c.Layout, c.Compression)}
	if r.SkippedReason != "" {
		notes = append(notes, "Skipped: "+r.SkippedReason)
	}
	if len(r.Snapshots) > 0 {
		if r.Phase == "restore" {
			notes = append(notes, fmt.Sprintf("Restored %d/%d snapshots; cache work: %s; preparation: %s. Cache work may overlap toolchain preparation; these are not additive setup durations.", hits, len(r.Snapshots), actionlog.Duration(r.TotalMS), actionlog.Duration(r.PreparationMS)))
		} else {
			notes = append(notes, "Save elapsed: "+actionlog.Duration(r.TotalMS)+".")
		}
	}
	if savePolicy != "" {
		notes = append(notes, savePolicy)
	}
	actionlog.Default.Summary(c.DisableSummary, "Go cache "+r.Phase, []string{"Snapshot", "Result", "Reason", "Transferred", "Cache work"}, rows, notes...)
}
