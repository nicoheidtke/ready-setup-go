package cachearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCopyFallbackNeverReplacesLocalFiles(t *testing.T) {
	source, dest := tempDir(t), tempDir(t)
	mtime := time.Unix(1700000000, 0)
	write(t, source, "file", "snapshot bytes", 0600, mtime)
	write(t, dest, "collision", "local bytes", 0444, mtime)
	r, err := os.OpenRoot(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	open := func() (*os.File, error) { return os.Open(filepath.Join(source, "file")) }
	stats := Stats{}
	hdr := tar.Header{Mode: 0444, ModTime: mtime}
	buf := make([]byte, 32768)
	if err := publishCopy(context.Background(), r, "new", hdr, open, buf, &stats); err != nil {
		t.Fatal(err)
	}
	if err := publishCopy(context.Background(), r, "collision", hdr, open, buf, &stats); err != nil {
		t.Fatal(err)
	}
	if stats.PublishedFiles != 1 || stats.LinkedFiles != 0 || stats.CopiedFiles != 1 || stats.CopiedBytes != int64(len("snapshot bytes")) {
		t.Fatalf("bad fallback stats: %+v", stats)
	}
	if read(t, filepath.Join(dest, "collision")) != "local bytes" {
		t.Fatal("fallback overwrote local data")
	}
	if read(t, filepath.Join(dest, "new")) != "snapshot bytes" {
		t.Fatal("bad fallback payload")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publishCopy(ctx, r, "cancelled", hdr, open, buf, &stats); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := os.Stat(filepath.Join(dest, "cancelled")); !os.IsNotExist(err) {
		t.Fatalf("left partial fallback: %v", err)
	}
}

func TestStageUsesDestinationAncestorWithoutCreatingCacheRoot(t *testing.T) {
	parent := tempDir(t)
	dest := filepath.Join(parent, "absent", "modules")
	stage, err := stagingDirectory(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)
	if filepath.Dir(stage) != parent {
		t.Fatalf("stage %s is outside destination ancestor %s", stage, parent)
	}
	if _, err := os.Stat(filepath.Join(parent, "absent")); !os.IsNotExist(err) {
		t.Fatalf("staging created cache ancestry: %v", err)
	}
}

func TestRestorePublishesValidatedStagingWithoutSecondPayloadCopy(t *testing.T) {
	parent := tempDir(t)
	source, dest := filepath.Join(parent, "source"), filepath.Join(parent, "destination")
	mtime := time.Unix(1700000000, 123456700)
	write(t, source, "deep/module@v1/sub/file", strings.Repeat("payload", 10000), 0444, mtime)
	write(t, source, "keep", "snapshot", 0444, mtime)
	write(t, dest, "keep", "existing local", 0444, mtime)
	data := snapshot(t, []Root{{"modules", source}}, Options{Compression: "gzip"})
	stats, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{Compression: "gzip", ContentIdentity: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 || stats.PublishedFiles != 1 {
		t.Fatalf("unexpected publication stats: %+v", stats)
	}
	linked := int64(1)
	if treeMovesSupported {
		linked = 0
	}
	if stats.LinkedFiles != linked || stats.CopiedBytes != 0 {
		t.Fatalf("same-volume publication copied payload: %+v", stats)
	}
	if stats.StageMS < 0 || stats.PreflightMS < 0 || stats.MergeMS < 0 || stats.CleanupMS < 0 || stats.StageMS+stats.MergeMS <= 0 || stats.ContentDigest == "" {
		t.Fatalf("missing metrics: %+v", stats)
	}
	if read(t, filepath.Join(dest, "keep")) != "existing local" {
		t.Fatal("overwrote local file")
	}
	if read(t, filepath.Join(dest, "deep/module@v1/sub/file")) != strings.Repeat("payload", 10000) {
		t.Fatal("bad publication")
	}
	for _, dir := range []string{parent, dest} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".setup-go-faster-") {
				t.Fatalf("left private staging behind: %s", entry.Name())
			}
		}
	}
}

func TestCorruptRestoreCleansDestinationVolumeStaging(t *testing.T) {
	parent := tempDir(t)
	dest := filepath.Join(parent, "not-created", "modules")
	data := archive(t, []fixture{{name: "modules/new", body: "payload"}})
	data[len(data)-1] = 1
	if _, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{}); err == nil {
		t.Fatal("accepted corrupt trailer")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("corrupt restore created destination or leaked staging: %v", entries)
	}
}
