package cachearchive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFingerprintMatchesArchiveAndIgnoresMtime(t *testing.T) {
	for _, compression := range []string{"none", "gzip"} {
		t.Run(compression, func(t *testing.T) {
			ctx := context.Background()
			src := tempDir(t)
			name := filepath.Join(src, "module")
			if err := os.WriteFile(name, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			rs := []Root{{Name: "modules", Path: src}}
			before, err := Fingerprint(ctx, rs, 0)
			if err != nil {
				t.Fatal(err)
			}
			var archive bytes.Buffer
			if _, err := Pack(ctx, &archive, rs, Options{Compression: compression}); err != nil {
				t.Fatal(err)
			}
			restored, err := Restore(ctx, bytes.NewReader(archive.Bytes()), []Root{{Name: "modules", Path: filepath.Join(tempDir(t), "restored")}}, Options{Compression: compression, ContentIdentity: true})
			if err != nil {
				t.Fatal(err)
			}
			if before.ContentDigest == "" || before.ContentDigest != restored.ContentDigest {
				t.Fatalf("identity mismatch: %+v / %+v", before, restored)
			}
			stamp := time.Unix(1740000000, 123456700)
			if err := os.Chtimes(name, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			after, err := Fingerprint(ctx, rs, 0)
			if err != nil || before.ContentDigest != after.ContentDigest {
				t.Fatalf("mtime changed identity: %+v %v", after, err)
			}
			if err := os.WriteFile(name, []byte("mutated!"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(name, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			changed, err := Fingerprint(ctx, rs, 0)
			if err != nil || before.ContentDigest == changed.ContentDigest {
				t.Fatalf("same-size mutation was missed: %+v %v", changed, err)
			}
		})
	}
}

func TestFingerprintTracksPathsModesAndOrder(t *testing.T) {
	ctx := context.Background()
	dir := tempDir(t)
	name := filepath.Join(dir, "file")
	if err := os.WriteFile(name, []byte("same bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	rs := []Root{{Name: "modules", Path: dir}}
	a, err := Fingerprint(ctx, rs, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(name, 0444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(name, 0644) })
	b, err := Fingerprint(ctx, rs, 0)
	if err != nil || a.ContentDigest == b.ContentDigest {
		t.Fatalf("mode change was missed: %+v %v", b, err)
	}
	if err := os.Chmod(name, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(name, filepath.Join(dir, "renamed")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "renamed"), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := Fingerprint(ctx, rs, 0)
	if err != nil || a.ContentDigest == c.ContentDigest {
		t.Fatalf("rename was missed: %+v %v", c, err)
	}
	first, reverse := newContentIdentity(), newContentIdentity()
	first.add("modules/a", 0644, false, []byte{1})
	first.add("modules/b", 0755, true, nil)
	reverse.add("modules/b", 0755, true, nil)
	reverse.add("modules/a", 0644, false, []byte{1})
	if first.sum() != reverse.sum() {
		t.Fatal("tar ordering changed identity")
	}
}

func TestFingerprintLimitsCancellationAndMissingRoots(t *testing.T) {
	dir := tempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}
	rs := []Root{{Name: "modules", Path: dir}}
	if _, err := Fingerprint(context.Background(), rs, 3); err == nil {
		t.Fatal("size bound ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fingerprint(ctx, rs, 0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stats, err := Fingerprint(context.Background(), []Root{{Name: "modules", Path: filepath.Join(dir, "missing")}}, 0)
	if err != nil || stats.Files != 0 || stats.ContentDigest == "" {
		t.Fatalf("missing root: %+v %v", stats, err)
	}
}
