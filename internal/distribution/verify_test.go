package distribution

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, root, name string, data []byte, mode os.FileMode) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, mode); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root, assets := t.TempDir(), t.TempDir()
	for _, file := range []string{"action.yml", "launch.cjs", "post.cjs", "launcher.cjs", "launch.sh", "experimental/node/action.yml", "experimental/node/launch.cjs", "experimental/node/post.cjs", "experimental/composite/action.yml", "README.md"} {
		put(t, root, file, []byte("fixture "+file), 0644)
	}
	for _, platform := range Platforms {
		b := []byte("executable " + platform)
		put(t, root, Binary(platform), b, 0755)
		put(t, assets, Asset(platform), b, 0755)
	}
	p := Provenance{Revision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64), Compiler: "go version go1.27.1 fixture/amd64", Platforms: Platforms}
	if err := WriteProvenance(root, p); err != nil {
		t.Fatal(err)
	}
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

func zipFixture(t *testing.T, root, assets, variant, wrong string, badMode os.FileMode) {
	t.Helper()
	f, err := os.Create(filepath.Join(assets, variant+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	files, err := regularFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if variant == "launcher" && strings.HasPrefix(name, "bin/") {
			continue
		}
		mode := os.FileMode(0644)
		if strings.HasPrefix(name, "bin/") && !strings.HasSuffix(name, ".exe") {
			mode = 0755
		}
		if name == wrong {
			mode = badMode
		}
		h := zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(mode)
		w, err := z.CreateHeader(&h)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCompleteDistributionAndTampering(t *testing.T) {
	for _, tc := range []string{"valid", "binary", "missing", "unlisted", "standalone", "zip-mode", "source-marker", "dirty", "platform"} {
		t.Run(tc, func(t *testing.T) {
			root, assets := fixture(t)
			switch tc {
			case "binary":
				put(t, root, Binary(Platforms[0]), []byte("tampered"), 0755)
			case "missing":
				if err := os.Remove(filepath.Join(root, filepath.FromSlash(Binary(Platforms[0])))); err != nil {
					t.Fatal(err)
				}
			case "unlisted":
				put(t, root, "unexpected", []byte("extra"), 0644)
			case "standalone":
				put(t, assets, Asset(Platforms[0]), []byte("tampered"), 0755)
				if err := WriteChecksums(assets); err != nil {
					t.Fatal(err)
				}
			case "zip-mode":
				zipFixture(t, root, assets, "bundled", Binary(Platforms[0]), 0644)
				if err := WriteChecksums(assets); err != nil {
					t.Fatal(err)
				}
			case "source-marker":
				put(t, root, "SOURCE_REVISION", []byte(strings.Repeat("c", 40)+"\n"), 0644)
				if err := WriteChecksums(root); err != nil {
					t.Fatal(err)
				}
			case "dirty":
				p := Provenance{Revision: strings.Repeat("a", 40), Dirty: true, SourceSHA256: strings.Repeat("b", 64), Compiler: "go version go1.27.1 fixture/amd64", Platforms: Platforms}
				if err := WriteProvenance(root, p); err != nil {
					t.Fatal(err)
				}
				if err := WriteChecksums(root); err != nil {
					t.Fatal(err)
				}
				if err := Verify(root, "", true, ""); err != nil {
					t.Fatalf("explicit local dirty diagnostic rejected: %v", err)
				}
			case "platform":
				p := Provenance{Revision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64), Compiler: "go version go1.27.1 fixture/amd64", Platforms: Platforms[:1]}
				if err := WriteProvenance(root, p); err != nil {
					t.Fatal(err)
				}
				if err := WriteChecksums(root); err != nil {
					t.Fatal(err)
				}
			}
			err := Verify(root, assets, false, "")
			if tc == "valid" && err != nil || tc != "valid" && err == nil {
				t.Fatalf("verification result for %s: %v", tc, err)
			}
		})
	}
}

func TestSourceMetadataTracksDirtyAndIgnoredInputs(t *testing.T) {
	root := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, b)
		}
	}
	gitRun("init", "--quiet")
	put(t, root, ".gitignore", []byte("/output/\n"), 0644)
	put(t, root, "main.go", []byte("package main\n"), 0644)
	gitRun("add", ".")
	gitRun("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "--quiet", "-m", "fixture")
	clean, err := Source(root, Platforms, true)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "output/ignored", []byte("build bytes"), 0644)
	again, err := Source(root, Platforms, true)
	if err != nil || again.SourceSHA256 != clean.SourceSHA256 || again.Dirty {
		t.Fatalf("ignored output changed provenance: %+v %v", again, err)
	}
	put(t, root, "main.go", []byte("package changed\n"), 0644)
	if _, err := Source(root, Platforms, true); err == nil {
		t.Fatal("dirty source passed release gate")
	}
	dirty, err := Source(root, Platforms, false)
	if err != nil || !dirty.Dirty || dirty.SourceSHA256 == clean.SourceSHA256 || !strings.HasSuffix(dirty.Marker(), "-dirty\n") {
		t.Fatalf("dirty source misidentified: %+v %v", dirty, err)
	}
	put(t, root, "new.go", []byte("untracked input"), 0644)
	untracked, err := Source(root, Platforms, false)
	if err != nil || untracked.SourceSHA256 == dirty.SourceSHA256 {
		t.Fatal("untracked source absent from provenance", err)
	}
}
