package cachearchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"path/filepath"
	"testing"
)

func TestRestoreStagingParentsAcrossEntryOrderAndRoots(t *testing.T) {
	// Parents may be implicit, appear after their children, or have no explicit
	// archive entry. Repeated parent preparation must preserve all three cases.
	data := archive(t, []fixture{
		{name: "modules/shared/deep/first", body: "first"},
		{name: "build/shared/deep/first", body: "build"},
		{name: "modules/shared/deep/second", body: "second"},
		{name: "modules/shared/deep", kind: tar.TypeDir, mode: 0555},
		{name: "modules/shared/sibling/third", body: "third"},
		{name: "modules/shared", kind: tar.TypeDir, mode: 0555},
		{name: "modules", kind: tar.TypeDir, mode: 0755},
		{name: "build", kind: tar.TypeDir, mode: 0755},
	})
	for _, compression := range []string{"none", "gzip"} {
		t.Run(compression, func(t *testing.T) {
			encoded := data
			if compression == "gzip" {
				var b bytes.Buffer
				gz := gzip.NewWriter(&b)
				if _, err := gz.Write(data); err != nil {
					t.Fatal(err)
				}
				if err := gz.Close(); err != nil {
					t.Fatal(err)
				}
				encoded = b.Bytes()
			}
			base := tempDir(t)
			modules, build := filepath.Join(base, "modules"), filepath.Join(base, "build")
			stats, err := Restore(context.Background(), bytes.NewReader(encoded), []Root{{"modules", modules}, {"build", build}}, Options{Compression: compression})
			if err != nil {
				t.Fatal(err)
			}
			if stats.Files != 4 || stats.PublishedFiles != 4 {
				t.Fatalf("restore counts: %+v", stats)
			}
			for name, want := range map[string]string{
				"modules/shared/deep/first":    "first",
				"modules/shared/deep/second":   "second",
				"modules/shared/sibling/third": "third",
				"build/shared/deep/first":      "build",
			} {
				if got := read(t, filepath.Join(base, filepath.FromSlash(name))); got != want {
					t.Fatalf("%s: got %q, want %q", name, got, want)
				}
			}
		})
	}
}
