package cache

import (
	"context"
	"io"
	"os"

	"ready-setup-go/internal/cachebackend"
)

type Backend interface {
	Lookup(context.Context, string, string, []string) (*cachebackend.Entry, error)
	Download(context.Context, cachebackend.Entry, io.Writer) (int64, error)
	Upload(context.Context, string, string, io.ReadSeeker, int64) error
}

func newBackend(c Config) (Backend, error) {
	url, token := os.Getenv("ACTIONS_RESULTS_URL"), os.Getenv("ACTIONS_RUNTIME_TOKEN")
	if url == "" || token == "" {
		return nil, ErrCredentialsUnavailable
	}
	return &cachebackend.Client{BaseURL: url, Token: token, MaxBytes: c.MaxBytes, Concurrency: c.UploadConcurrency, BlockSize: c.UploadChunkBytes, DownloadConcurrency: c.DownloadConcurrency, DownloadBlockSize: c.DownloadChunkBytes}, nil
}

// The runner may grant only a subset of cache operations. Unknown/unset modes
// preserve compatibility with runners predating this optional capability.
func permissions() (read, write bool) {
	switch os.Getenv("ACTIONS_CACHE_MODE") {
	case "none":
		return false, false
	case "read":
		return true, false
	case "write-only":
		return false, true
	default:
		return true, true
	}
}
