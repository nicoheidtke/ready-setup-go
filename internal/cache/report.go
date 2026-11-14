package cache

import (
	"encoding/json"
	"os"
	"time"

	"setup-go-faster/internal/actionlog"
)

type Metric struct {
	Kind               string  `json:"kind"`
	Key                string  `json:"key"`
	MatchedKey         string  `json:"matched_key,omitempty"`
	MatchType          string  `json:"match_type,omitempty"`
	RestoreReason      string  `json:"restore_reason,omitempty"`
	Hit                bool    `json:"hit"`
	Skipped            bool    `json:"skipped"`
	Bytes              int64   `json:"transferred_bytes"`
	Files              int64   `json:"archive_files"`
	PayloadBytes       int64   `json:"archive_payload_bytes"`
	Chunks             int     `json:"chunks,omitempty"`
	ReusedChunks       int     `json:"reused_chunks,omitempty"`
	ReusedBytes        int64   `json:"reused_bytes,omitempty"`
	LookupMS           float64 `json:"lookup_ms"`
	TransferMS         float64 `json:"transfer_ms"`
	ArchiveMS          float64 `json:"archive_ms"`
	ArchiveStageMS     float64 `json:"archive_stage_ms,omitempty"`
	ArchivePreflightMS float64 `json:"archive_preflight_ms,omitempty"`
	ArchiveMergeMS     float64 `json:"archive_merge_ms,omitempty"`
	ArchiveCleanupMS   float64 `json:"archive_cleanup_ms,omitempty"`
	PublishedFiles     int64   `json:"published_files,omitempty"`
	LinkedFiles        int64   `json:"linked_files,omitempty"`
	CopiedFiles        int64   `json:"copied_files,omitempty"`
	CopiedBytes        int64   `json:"copied_bytes,omitempty"`
	ModuleCheckMS      float64 `json:"module_check_ms,omitempty"`
	SaveReason         string  `json:"save_reason,omitempty"`
	Error              string  `json:"error,omitempty"`
}
type Report struct {
	Phase          string   `json:"phase"`
	PreparationMS  float64  `json:"preparation_ms"`
	PrefetchWaitMS float64  `json:"prefetch_wait_ms,omitempty"`
	SkippedReason  string   `json:"skipped_reason,omitempty"`
	TotalMS        float64  `json:"total_ms"`
	Snapshots      []Metric `json:"snapshots"`
}

func ms(start time.Time) float64 { return float64(time.Since(start)) / float64(time.Millisecond) }

func writeReport(path string, r Report) error {
	logReport(actionlog.Default, r)
	actionlog.Default.DebugJSON("cache "+r.Phase+" report", r)
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func postReportPath(path string) string {
	if path != "" {
		return path + ".post.json"
	}
	return ""
}
