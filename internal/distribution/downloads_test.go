package distribution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func downloadFixture(t *testing.T) (string, string) {
	t.Helper()
	root, assets := fixture(t)
	if err := WriteNativeDownloads(root, root, "https://github.com/owner/action/releases/download/v1", Platforms); err != nil {
		t.Fatal(err)
	}
	// Remove each known fixture binary, leaving the directories harmlessly empty.
	for _, platform := range Platforms {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(Binary(platform)))); err != nil {
			t.Fatal(err)
		}
	}
	put(t, root, "native-download.cjs", []byte("bootstrap"), 0644)
	if err := WriteChecksums(root); err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"bundled", "launcher"} {
		zipFixture(t, root, assets, variant, "", 0)
	}
	if err := WriteChecksums(assets); err != nil {
		t.Fatal(err)
	}
	return root, assets
}

func TestDownloadDistributionIntegrityAndReleaseBinding(t *testing.T) {
	for _, kind := range []string{"valid", "digest", "size", "platform", "extra binary", "base", "unknown field", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			root, assets := downloadFixture(t)
			m, err := ReadNativeDownloads(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := CheckNativeRelease(root, "owner/action", "v1"); err != nil {
				t.Fatal(err)
			}
			if err := CheckNativeRelease(root, "other/action", "v1"); err == nil {
				t.Fatal("wrong repository accepted")
			}
			if err := CheckNativeRelease(root, "owner/action", "v2"); err == nil {
				t.Fatal("wrong tag accepted")
			}
			switch kind {
			case "digest":
				a := m.Platforms[Platforms[0]]
				a.SHA256 = strings.Repeat("0", 64)
				m.Platforms[Platforms[0]] = a
			case "size":
				a := m.Platforms[Platforms[0]]
				a.Size++
				m.Platforms[Platforms[0]] = a
			case "platform":
				delete(m.Platforms, Platforms[0])
			case "extra binary":
				put(t, root, Binary(Platforms[0]), []byte("unexpected"), 0755)
			case "base":
				m.BaseURL = "https://evil.test/asset"
			}
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "unknown field" {
				data = append([]byte(`{"unknown":true,`), data[1:]...)
			}
			if kind == "trailing" {
				data = append(data, []byte(` {}`)...)
			}
			put(t, root, DownloadsFile, data, 0644)
			if err := WriteChecksums(root); err != nil {
				t.Fatal(err)
			}
			for _, variant := range []string{"bundled", "launcher"} {
				zipFixture(t, root, assets, variant, "", 0)
			}
			if err := WriteChecksums(assets); err != nil {
				t.Fatal(err)
			}
			err = Verify(root, assets, false, "")
			if (err == nil) != (kind == "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

func TestDownloadBaseRejectsMutableAliasesAndCredentials(t *testing.T) {
	for _, base := range []string{"http://github.com/a/b/releases/download/v1", "https://token@github.com/a/b/releases/download/v1", "https://github.com/a/b/releases/latest/download", "https://github.com/a/b/releases/download/v1?token=secret", "https://github.com/a/b/releases/download/v1#fragment"} {
		if ValidateDownloadBase(base) == nil {
			t.Fatalf("accepted %s", base)
		}
	}
}
