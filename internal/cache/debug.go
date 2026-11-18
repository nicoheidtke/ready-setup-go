package cache

import "ready-setup-go/internal/actionlog"

// Keep this allowlist explicit. Scope, manifest/report paths, private post state,
// runtime credentials and backend URLs do not belong in configuration logs.
func debugConfig(c Config) {
	if !actionlog.Default.DebugEnabled() {
		return
	}
	actionlog.Default.DebugJSON("cache configuration", map[string]any{
		"mode": c.Mode, "layout": c.Layout, "storage": c.Storage, "compression": c.Compression,
		"overlap": c.Overlap, "strict": c.Strict, "save_on_failure": c.SaveOnFailure,
		"save_policy": c.SavePolicy, "save_branches": c.SaveBranches,
		"compression_workers": c.CompressionConcurrency, "restore_workers": c.RestoreConcurrency,
		"chunk_workers": c.ChunkConcurrency, "upload_workers": c.UploadConcurrency,
		"download_workers": c.DownloadConcurrency, "upload_chunk_bytes": c.UploadChunkBytes,
		"download_chunk_bytes": c.DownloadChunkBytes, "max_bytes": c.MaxBytes,
		"build_max_age": c.BuildMaxAge.String(),
	})
}
