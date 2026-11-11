// package builds a self-contained action distribution. Run during development or
// release preparation only; consumers never build the installer.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"setup-go-better/internal/distribution"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	out := flag.String("out", "dist", "new or existing output directory")
	archives := flag.String("archives", "", "optional directory outside output for acquisition experiment assets")
	host := flag.String("platform", "", "optional single platform, e.g. windows/amd64; default builds all six")
	clean := flag.Bool("require-clean", false, "reject local changes for release preparation")
	flag.Parse()
	platforms := distribution.Platforms
	if *host != "" {
		valid := false
		for _, p := range platforms {
			if p == *host {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("unsupported platform %q", *host)
		}
		platforms = []string{*host}
	}
	provenance, err := distribution.Source(".", platforms, *clean)
	if err != nil {
		return err
	}
	for _, p := range platforms {
		parts := strings.Split(p, "/")
		name := "setup-go-better"
		if parts[0] == "windows" {
			name += ".exe"
		}
		rel := filepath.Join("bin", strings.Join(parts, "-"), name)
		file := filepath.Join(*out, rel)
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			return err
		}
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -buildid=", "-o", file, "./cmd/setup-go-better")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+parts[0], "GOARCH="+parts[1], "GOAMD64=v1", "GOARM64=v8.0", "GOFLAGS=", "GOEXPERIMENT=", "GOTOOLCHAIN=local", "GOTELEMETRY=off")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	files := []string{"action.yml", "launch.sh", "launch.cjs", "post.cjs", "launcher.cjs", "experimental/composite/action.yml", "experimental/node/action.yml", "experimental/node/launch.cjs", "experimental/node/post.cjs", "README.md", "LICENSE", "SECURITY.md", "CONTRIBUTING.md", "ROADMAP.md", "AGENTS.md", "docs/USAGE.md", "docs/ARCHITECTURE.md", "docs/COMPARISON.md", "docs/BENCHMARKS.md", "docs/DEVELOPMENT.md", "docs/IMPLEMENTATION_PLAN.md", "docs/LIFECYCLE_VALIDATION.md", "docs/SECURITY_MODEL.md", "docs/RELEASES.md", "docs/validation-2026-09-22.md", "docs/investigation-2026-11-10.md", "internal/cachebackend/PROTOCOL.md"}
	for _, rel := range files {
		b, err := os.ReadFile(rel)
		if err != nil {
			return err
		}
		dst := filepath.Join(*out, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(dst, b, 0644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(*out, ".gitattributes"), []byte(distribution.CheckoutAttributes), 0644); err != nil {
		return err
	}
	current, err := distribution.Source(".", platforms, *clean)
	if err != nil {
		return err
	}
	if current.Revision != provenance.Revision || current.Dirty != provenance.Dirty || current.SourceSHA256 != provenance.SourceSHA256 || current.Compiler != provenance.Compiler {
		return fmt.Errorf("source changed during packaging; discard this output and rebuild")
	}
	if err := distribution.WriteProvenance(*out, provenance); err != nil {
		return err
	}
	if err := distribution.WriteChecksums(*out); err != nil {
		return err
	}
	if *archives != "" {
		return makeArchives(*out, *archives, platforms)
	}
	return nil
}

func makeArchives(root, out string, platforms []string) error {
	absRoot, e := filepath.Abs(root)
	if e != nil {
		return e
	}
	absOut, e := filepath.Abs(out)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(absRoot, absOut)
	if e != nil {
		return e
	}
	if rel == "." || filepath.IsLocal(rel) {
		return fmt.Errorf("archives directory must be outside packaged action")
	}
	if e = os.MkdirAll(out, 0755); e != nil {
		return e
	}
	for _, variant := range []string{"bundled", "launcher"} {
		f, e := os.Create(filepath.Join(out, variant+".zip"))
		if e != nil {
			return e
		}
		z := zip.NewWriter(f)
		e = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			if d.IsDir() {
				if variant == "launcher" && rel == "bin" {
					return filepath.SkipDir
				}
				return nil
			}
			st, err := d.Info()
			if err != nil {
				return err
			}
			if !st.Mode().IsRegular() {
				return fmt.Errorf("nonregular distribution file %s", p)
			}
			h := &zip.FileHeader{Name: filepath.ToSlash(rel), Method: zip.Deflate}
			h.SetModTime(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
			mode := os.FileMode(0644)
			if strings.HasPrefix(h.Name, "bin/") && !strings.HasSuffix(h.Name, ".exe") {
				mode = 0755
			}
			h.SetMode(mode)
			w, err := z.CreateHeader(h)
			if err != nil {
				return err
			}
			src, err := os.Open(p)
			if err != nil {
				return err
			}
			_, err = io.Copy(w, src)
			closeErr := src.Close()
			if err != nil {
				return err
			}
			return closeErr
		})
		ze, fe := z.Close(), f.Close()
		if e != nil {
			return e
		}
		if ze != nil {
			return ze
		}
		if fe != nil {
			return fe
		}
	}
	for _, platform := range platforms {
		name := "setup-go-better"
		if strings.HasPrefix(platform, "windows/") {
			name += ".exe"
		}
		b, e := os.ReadFile(filepath.Join(root, "bin", strings.ReplaceAll(platform, "/", "-"), name))
		if e != nil {
			return e
		}
		dst := "setup-go-better-" + strings.ReplaceAll(platform, "/", "-")
		if strings.HasSuffix(name, ".exe") {
			dst += ".exe"
		}
		if e = os.WriteFile(filepath.Join(out, dst), b, 0755); e != nil {
			return e
		}
	}
	entries, e := os.ReadDir(out)
	if e != nil {
		return e
	}
	var sums strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "SHA256SUMS" {
			continue
		}
		b, e := os.ReadFile(filepath.Join(out, entry.Name()))
		if e != nil {
			return e
		}
		sum := sha256.Sum256(b)
		fmt.Fprintf(&sums, "%x  %s\n", sum, entry.Name())
	}
	return os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(sums.String()), 0644)
}
