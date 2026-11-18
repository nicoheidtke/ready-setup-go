package cachearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTreeMoveNeverReplacesDestination(t *testing.T) {
	for _, kind := range []string{"file", "empty-directory", "nonempty-directory"} {
		t.Run(kind, func(t *testing.T) {
			base := tempDir(t)
			source, dest := filepath.Join(base, "source"), filepath.Join(base, "dest")
			write(t, source, "archive", "new", 0444, time.Unix(1, 0))
			if kind == "file" {
				write(t, base, "dest", "local", 0444, time.Unix(2, 0))
			} else {
				if err := os.Mkdir(dest, 0755); err != nil {
					t.Fatal(err)
				}
				if kind == "nonempty-directory" {
					write(t, dest, "local", "keep", 0444, time.Unix(2, 0))
				}
			}
			if err := moveTreeNoReplace(source, dest); err == nil {
				t.Fatal("replaced existing destination")
			}
			if read(t, filepath.Join(source, "archive")) != "new" {
				t.Fatal("lost source")
			}
			if kind == "file" && read(t, dest) != "local" {
				t.Fatal("replaced local file")
			}
			if kind == "nonempty-directory" && read(t, filepath.Join(dest, "local")) != "keep" {
				t.Fatal("replaced local directory")
			}
			if kind == "empty-directory" {
				entries, err := os.ReadDir(dest)
				if err != nil || len(entries) != 0 {
					t.Fatalf("changed empty directory: %v %v", entries, err)
				}
			}
		})
	}
}

func TestRestoreMovesMissingWindowsTrees(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			base := tempDir(t)
			source, dest := filepath.Join(base, "source"), filepath.Join(base, "absent", "dest")
			stamp := time.Unix(1700000000, 123456700)
			for _, name := range []string{"a/deep/file", "b/file"} {
				write(t, source, name, name, 0444, stamp)
			}
			if err := os.MkdirAll(filepath.Join(source, "empty"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(source, "a"), 0555); err != nil {
				t.Fatal(err)
			}
			if existing {
				write(t, dest, "local", "keep", 0444, stamp)
			}
			data := snapshot(t, []Root{{"modules", source}}, Options{})
			stats, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			if stats.PublishedFiles != 2 || stats.LinkedFiles != 0 || stats.CopiedFiles != 0 {
				t.Fatalf("did not publish whole trees: %+v", stats)
			}
			for _, name := range []string{"a/deep/file", "b/file"} {
				target := filepath.Join(dest, name)
				info, err := os.Stat(target)
				if err != nil || info.Mode().Perm()&0200 != 0 || !info.ModTime().Equal(stamp) || read(t, target) != name {
					t.Fatalf("lost metadata/content: %v %v", info, err)
				}
			}
			info, err := os.Stat(filepath.Join(dest, "a"))
			if err != nil || info.Mode().Perm()&0200 != 0 {
				t.Fatalf("lost directory mode: %v %v", info, err)
			}
			if _, err := os.Stat(filepath.Join(dest, "empty")); err != nil {
				t.Fatal(err)
			}
			if existing && read(t, filepath.Join(dest, "local")) != "keep" {
				t.Fatal("lost local file")
			}
		})
	}
}

func TestTreeMoveFallbackAndLateDestination(t *testing.T) {
	for _, collision := range []string{"unsupported", "directory", "file", "cancelled"} {
		t.Run(collision, func(t *testing.T) {
			base := tempDir(t)
			stage, dest := filepath.Join(base, "stage"), filepath.Join(base, "dest")
			stamp := time.Unix(1700000000, 0)
			write(t, filepath.Join(stage, "payload"), "deep/file", "archive", 0600, stamp)
			entries := []entry{
				{name: "modules/deep/file", hdr: tar.Header{Typeflag: tar.TypeReg, Mode: 0444, ModTime: stamp}},
				{name: "modules/deep", hdr: tar.Header{Typeflag: tar.TypeDir, Mode: 0555, ModTime: stamp}},
				{name: "modules", hdr: tar.Header{Typeflag: tar.TypeDir, Mode: 0755, ModTime: stamp}},
			}
			f, err := os.OpenFile(filepath.Join(stage, "payload/deep/file"), os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			entries[0].stagedMetadata, err = finalizeStagedFile(f, &entries[0].hdr)
			if err := errors.Join(err, f.Close()); err != nil {
				t.Fatal(err)
			}
			roots := map[string]Root{"modules": {"modules", dest}}
			stages := map[string]string{"modules": stage}
			checked := make(map[string]fs.FileInfo)
			if _, err := preflightPath(dest, checked); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stats := Stats{}
			remaining, err := publishMissingTrees(ctx, stages, entries, roots, checked, func(source, target string) error {
				switch collision {
				case "directory":
					write(t, target, "deep/file", "local", 0444, stamp)
				case "file":
					write(t, base, "dest", "local", 0444, stamp)
				case "cancelled":
					cancel()
				}
				return errors.New("move refused")
			}, &stats)
			if err != nil {
				t.Fatal(err)
			}
			if len(remaining) != len(entries) || stats.PublishedFiles != 0 {
				t.Fatalf("discarded fallback entries: %+v", stats)
			}
			info, err := os.Stat(filepath.Join(stage, "payload/deep/file"))
			if err != nil || info.Mode().Perm()&0200 == 0 {
				t.Fatalf("left read-only staging: %v %v", info, err)
			}
			err = merge(ctx, stages, remaining, roots, 4, &stats)
			if collision == "cancelled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("ignored cancellation: %v", err)
				}
			} else if collision == "file" {
				if err == nil || read(t, dest) != "local" {
					t.Fatalf("lost late local file: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				want := "archive"
				if collision == "directory" {
					want = "local"
				}
				if got := read(t, filepath.Join(dest, "deep/file")); got != want {
					t.Fatalf("got %q", got)
				}
			}
		})
	}
}

func TestTreeMoveLongPaths(t *testing.T) {
	base := tempDir(t)
	parent := filepath.Join(base, strings.Repeat("long-directory-", 12), strings.Repeat("deep-", 18))
	if err := os.MkdirAll(parent, 0755); err != nil {
		t.Fatal(err)
	}
	source, dest := filepath.Join(parent, "source"), filepath.Join(parent, "destination")
	write(t, source, "file", "content", 0444, time.Unix(1, 0))
	if err := moveTreeNoReplace(source, dest); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dest, "file")) != "content" {
		t.Fatal("lost long-path payload")
	}
}
