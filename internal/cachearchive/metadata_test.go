package cachearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMetadataBudgetBoundaries(t *testing.T) {
	b := metadataBudget{limits: metadataLimits{entries: 2, pathBytes: 7, depth: 2}}
	// a costs one byte; a/b costs one for a plus three for a/b.
	if err := b.add("a"); err != nil {
		t.Fatal(err)
	}
	if err := b.add("a/b"); err != nil {
		t.Fatal(err)
	}
	if err := b.add("c"); err == nil || !strings.Contains(err.Error(), "entry limit") {
		t.Fatalf("entry limit: %v", err)
	}
	for _, tc := range []struct {
		name   string
		limits metadataLimits
		want   string
	}{
		{"a/b", metadataLimits{entries: 1, pathBytes: 3, depth: 2}, "path byte limit"},
		{strings.Repeat("a", 1024), metadataLimits{entries: 1, pathBytes: 1000, depth: 2}, "path byte limit"},
		{"a/b/c", metadataLimits{entries: 1, pathBytes: 100, depth: 2}, "path depth limit"},
	} {
		budget := metadataBudget{limits: tc.limits}
		if err := budget.add(tc.name); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%q: %v", tc.name, err)
		}
	}
}

func TestMetadataLimitsPreserveAcceptedBytesAndIdentity(t *testing.T) {
	source := tempDir(t)
	write(t, source, "file", "content", 0644, time.Unix(123, 0))
	roots := []Root{{"modules", source}}
	limits := metadataLimits{entries: 2, pathBytes: 26, headerBytes: 100, depth: 2}
	want := snapshot(t, roots, Options{})
	var got bytes.Buffer
	if _, err := pack(context.Background(), &got, roots, Options{}, limits); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, got.Bytes()) {
		t.Fatal("metadata limits changed accepted archive bytes")
	}
	fingerprint, err := fingerprint(context.Background(), roots, 0, limits)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restore(context.Background(), &got, []Root{{"modules", filepath.Join(tempDir(t), "missing")}}, Options{ContentIdentity: true}, limits)
	if err != nil || restored.ContentDigest != fingerprint.ContentDigest {
		t.Fatalf("accepted identity mismatch: %+v, %v", restored, err)
	}
}

func TestMetadataLimitsAcrossOperations(t *testing.T) {
	source := tempDir(t)
	for i := range 4 {
		write(t, source, fmt.Sprintf("d/file%d", i), "", 0644, time.Unix(123, 0))
	}
	roots := []Root{{"modules", source}}
	data := snapshot(t, roots, Options{})
	for _, tc := range []struct {
		name   string
		limits metadataLimits
		want   string
	}{
		{"entries", metadataLimits{entries: 3, pathBytes: 10000, headerBytes: 10000, depth: 128}, "entry limit"},
		{"paths", metadataLimits{entries: 100, pathBytes: 40, headerBytes: 10000, depth: 128}, "path byte limit"},
		{"depth", metadataLimits{entries: 100, pathBytes: 10000, headerBytes: 10000, depth: 2}, "path depth limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var packed bytes.Buffer
			_, packErr := pack(context.Background(), &packed, roots, Options{}, tc.limits)
			_, fingerprintErr := fingerprint(context.Background(), roots, 0, tc.limits)
			dest := tempDir(t)
			write(t, dest, "existing", "keep", 0644, time.Unix(456, 0))
			_, restoreErr := restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{ContentIdentity: true}, tc.limits)
			for operation, err := range map[string]error{"pack": packErr, "fingerprint": fingerprintErr, "restore": restoreErr} {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("%s: %v", operation, err)
				}
			}
			if got := read(t, filepath.Join(dest, "existing")); got != "keep" {
				t.Fatalf("existing content changed: %q", got)
			}
			entries, err := os.ReadDir(dest)
			if err != nil || len(entries) != 1 {
				t.Fatalf("published on rejection: %v, %v", entries, err)
			}
			stages, err := filepath.Glob(filepath.Join(filepath.Dir(dest), ".ready-setup-go-cache-*"))
			if err != nil || len(stages) != 0 {
				t.Fatalf("staging leaked: %v, %v", stages, err)
			}
		})
	}
}

func TestRestoreHeaderMetadataLimit(t *testing.T) {
	var data bytes.Buffer
	w := tar.NewWriter(&data)
	for _, name := range []string{"modules/a", "modules/b"} {
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0644, Typeflag: tar.TypeReg, PAXRecords: map[string]string{"custom": "value"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	dest := tempDir(t)
	_, err := restore(context.Background(), &data, []Root{{"modules", dest}}, Options{}, metadataLimits{entries: 10, pathBytes: 1000, headerBytes: 15, depth: 128})
	if err == nil || !strings.Contains(err.Error(), "header byte limit") {
		t.Fatalf("header budget: %v", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 0 {
		t.Fatalf("published on rejection: %v, %v", entries, err)
	}
}

func TestRetainedHeaderDropsUnusedMetadata(t *testing.T) {
	h := tar.Header{Name: "modules/a", Typeflag: tar.TypeReg, Mode: 0755, Size: 3, ModTime: time.Unix(123, 0), Uname: "owner", PAXRecords: map[string]string{"custom": "value"}, Xattrs: map[string]string{"custom": "value"}}
	got := retainedHeader(&h)
	if got.Name != "" || got.Uname != "" || got.PAXRecords != nil || got.Xattrs != nil || got.Mode != h.Mode || got.Size != h.Size || got.ModTime != h.ModTime || got.Typeflag != h.Typeflag {
		t.Fatalf("unexpected retained header: %+v", got)
	}
}
