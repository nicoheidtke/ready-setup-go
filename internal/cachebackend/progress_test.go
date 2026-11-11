package cachebackend

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"setup-go-better/internal/actionlog"
)

func TestRangeProgressUsesOneAggregateWithoutSignedURL(t *testing.T) {
	const data = "abcdefghijkl"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var first, last int
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &first, &last); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", first, last, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		// One slow first range crosses the logging threshold; subsequent ranges
		// exercise concurrent accumulation without needing per-worker loggers.
		if first == 0 {
			time.Sleep(2100 * time.Millisecond)
		}
		_, _ = w.Write([]byte(data[first : last+1]))
	}))
	defer server.Close()
	var out bytes.Buffer
	ctx, finish := actionlog.New(&out, true).Transfer(context.Background(), "Download build cache", 0, true)
	c := Client{DownloadConcurrency: 2, DownloadBlockSize: 4}
	n, err := c.Download(ctx, Entry{URL: server.URL + "?sig=credential"}, downloadFile(t))
	finish(err)
	if err != nil || n != int64(len(data)) {
		t.Fatalf("download %d: %v", n, err)
	}
	got := out.String()
	if !strings.Contains(got, "12 B / 12 B · finished") || strings.Contains(got, "credential") || strings.Contains(got, "100%") || strings.Count(got, "Progress:") != 2 {
		t.Fatalf("bad progress: %q", got)
	}
}
