package cachearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestUnsupportedStagedWindowsMetadataRetainsLegacyPath(t *testing.T) {
	for _, code := range []syscall.Errno{1, 50, 87, 120, 124} {
		if !unsupportedStagedMetadata(code) {
			t.Fatalf("unsupported operation %v did not select legacy metadata", code)
		}
	}
	for _, code := range []syscall.Errno{5, 6, 32, 112} {
		if unsupportedStagedMetadata(code) {
			t.Fatalf("real metadata failure %v selected legacy metadata", code)
		}
	}
	base := tempDir(t)
	stage, dest := filepath.Join(base, "stage"), filepath.Join(base, "dest")
	stamp := time.Unix(1700000000, 123456700)
	write(t, filepath.Join(stage, "payload"), "file", "private payload", 0600, time.Unix(1, 0))
	entries := []entry{
		{name: "modules/file", hdr: tar.Header{Typeflag: tar.TypeReg, Mode: 0444, ModTime: stamp}},
		{name: "modules", hdr: tar.Header{Typeflag: tar.TypeDir, Mode: 0755, ModTime: stamp}},
	}
	// Metadata-ready remains false after the unsupported-handle fallback. The
	// unchanged tree-publication path must still set archived times and modes.
	stats := Stats{}
	remaining, err := publishMissingTrees(context.Background(), map[string]string{"modules": stage}, entries, map[string]Root{"modules": {Name: "modules", Path: dest}}, make(map[string]os.FileInfo), moveTreeNoReplace, &stats)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("legacy metadata publication: remaining=%v err=%v", remaining, err)
	}
	info, err := os.Stat(filepath.Join(dest, "file"))
	if err != nil || info.Mode().Perm()&0200 != 0 || !info.ModTime().Equal(stamp) {
		t.Fatalf("legacy metadata lost: info=%v err=%v", info, err)
	}
}

func TestStagedWindowsFileMetadataMatchesPathMetadata(t *testing.T) {
	if got := unsafe.Sizeof(fileBasicInfo{}); got != 40 {
		t.Fatalf("FILE_BASIC_INFO size = %d, want 40", got)
	}
	for _, stamp := range []time.Time{time.Unix(1700000000, 123456700), time.Unix(-1, 0), time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC)} {
		for _, mode := range []int64{0000, 0020, 0444, 0644, 0755} {
			base := tempDir(t)
			legacy, staged := filepath.Join(base, "legacy"), filepath.Join(base, "staged")
			if err := os.WriteFile(legacy, []byte("payload"), 0600); err != nil {
				t.Fatal(err)
			}
			legacyErr := os.Chtimes(legacy, stamp, stamp)
			if legacyErr == nil {
				legacyErr = os.Chmod(legacy, os.FileMode(mode))
			}
			f, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString("payload"); err != nil {
				f.Close()
				t.Fatal(err)
			}
			ready, metadataErr := finalizeStagedFile(f, &tar.Header{Mode: mode, ModTime: stamp})
			closeErr := f.Close()
			if (legacyErr == nil) != (metadataErr == nil) || closeErr != nil {
				t.Fatalf("stamp %v mode %o: legacy %v, staged %v, close %v", stamp, mode, legacyErr, metadataErr, closeErr)
			}
			if metadataErr != nil {
				continue
			}
			want, err := os.Stat(legacy)
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.Stat(staged)
			if err != nil || !ready || got.Mode() != want.Mode() || !got.ModTime().Equal(want.ModTime()) {
				t.Fatalf("stamp %v mode %o: metadata differs: ready=%v got=%v want=%v err=%v", stamp, mode, ready, got, want, err)
			}
		}
	}
}

func TestStagedWindowsFileZeroTimeAndFailure(t *testing.T) {
	f, err := os.Create(filepath.Join(tempDir(t), "file"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.Stat()
	if err != nil {
		f.Close()
		t.Fatal(err)
	}
	ready, err := finalizeStagedFile(f, &tar.Header{Mode: 0644})
	if err != nil || !ready {
		f.Close()
		t.Fatalf("zero time: %v, %v", ready, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(f.Name())
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("zero time changed timestamp: before=%v after=%v err=%v", before, after, err)
	}
	ready, err = finalizeStagedFile(f, &tar.Header{Mode: 0444, ModTime: time.Unix(123, 0)})
	var pathErr *os.PathError
	if ready || !errors.As(err, &pathErr) {
		t.Fatalf("closed-handle metadata error: ready=%v err=%v", ready, err)
	}
}

func TestRejectedWindowsArchiveRemovesReadonlyStaging(t *testing.T) {
	for _, compression := range []string{"none", "gzip", "zstd"} {
		t.Run(compression, func(t *testing.T) {
			base := tempDir(t)
			source, dest := filepath.Join(base, "source"), filepath.Join(base, "destination")
			write(t, source, "deep/readonly", "private staged bytes", 0444, time.Unix(123, 0))
			data := snapshot(t, []Root{{"modules", source}}, Options{Compression: compression})
			// Trailing nonzero data invalidates the complete stream, after regular
			// files have been staged and their readonly metadata finalized.
			data = append(data, 1)
			_, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{Compression: compression})
			if err == nil {
				t.Fatal("accepted corrupt archive")
			}
			entries, err := os.ReadDir(base)
			if err != nil || len(entries) != 1 || entries[0].Name() != "source" {
				t.Fatalf("published files or leaked readonly staging: %v %v", entries, err)
			}
		})
	}
}
