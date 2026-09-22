// acquisition measures transfer and unpack costs outside GitHub's bundled
// "Set up job" step. It is a controlled experiment, not a claim about runner timings.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sample struct {
	Index            int     `json:"index"`
	Variant          string  `json:"variant"`
	TransferMS       float64 `json:"transfer_ms"`
	UnpackMS         float64 `json:"unpack_ms"`
	TotalMS          float64 `json:"total_ms"`
	Bytes            int64   `json:"bytes"`
	PeakLogicalBytes int64   `json:"peak_logical_bytes"`
}
type asset struct {
	url, sha string
	unzip    bool
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func fetch(ctx context.Context, a asset, dir string, index int) (bytes, unpacked int64, transfer, unpack float64, err error) {
	start := time.Now()
	req, e := http.NewRequestWithContext(ctx, "GET", a.url, nil)
	if e != nil {
		err = e
		return
	}
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		err = e
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		err = fmt.Errorf("HTTP %d for %s", resp.StatusCode, a.url)
		return
	}
	p := filepath.Join(dir, fmt.Sprintf("asset-%d", index))
	f, e := os.Create(p)
	if e != nil {
		err = e
		return
	}
	h := sha256.New()
	bytes, e = io.Copy(io.MultiWriter(f, h), resp.Body)
	ce := f.Close()
	if e != nil {
		err = e
		return
	}
	if ce != nil {
		err = ce
		return
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(a.sha) {
		err = fmt.Errorf("asset checksum mismatch for %s", a.url)
		return
	}
	transfer = float64(time.Since(start)) / float64(time.Millisecond)
	if !a.unzip {
		return
	}
	start = time.Now()
	z, e := zip.OpenReader(p)
	if e != nil {
		err = e
		return
	}
	defer z.Close()
	root := filepath.Join(dir, fmt.Sprintf("unpacked-%d", index))
	for _, entry := range z.File {
		if strings.ContainsAny(entry.Name, "\\:\x00") || !filepath.IsLocal(filepath.FromSlash(entry.Name)) || entry.Mode()&os.ModeSymlink != 0 {
			err = fmt.Errorf("unsafe bundle entry %q", entry.Name)
			return
		}
		dst := filepath.Join(root, filepath.FromSlash(entry.Name))
		if entry.FileInfo().IsDir() {
			if e = os.MkdirAll(dst, 0755); e != nil {
				err = e
				return
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
			err = e
			return
		}
		r, e := entry.Open()
		if e != nil {
			err = e
			return
		}
		w, e := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode().Perm())
		if e != nil {
			r.Close()
			err = e
			return
		}
		n, e := io.Copy(w, r)
		re := r.Close()
		we := w.Close()
		unpacked += n
		if e != nil {
			err = e
			return
		}
		if re != nil {
			err = re
			return
		}
		if we != nil {
			err = we
			return
		}
	}
	unpack = float64(time.Since(start)) / float64(time.Millisecond)
	return
}
func run() error {
	bundleURL := flag.String("bundle-url", "", "immutable ZIP containing the bundled action")
	bundleSHA := flag.String("bundle-sha256", "", "expected bundle SHA-256")
	launcherURL := flag.String("launcher-url", "", "immutable ZIP of the same launcher without binaries")
	launcherSHA := flag.String("launcher-sha256", "", "expected launcher ZIP SHA-256")
	binaryURL := flag.String("binary-url", "", "immutable matching native executable")
	binarySHA := flag.String("binary-sha256", "", "expected executable SHA-256")
	count := flag.Int("samples", 20, "samples per variant, at least 20")
	out := flag.String("out", "acquisition.json", "report path")
	flag.Parse()
	for _, p := range [][2]string{{*bundleURL, *bundleSHA}, {*launcherURL, *launcherSHA}, {*binaryURL, *binarySHA}} {
		sum, e := hex.DecodeString(p[1])
		if !strings.HasPrefix(p[0], "https://") || e != nil || len(sum) != 32 {
			return fmt.Errorf("all three HTTPS URLs and SHA-256 values are required")
		}
	}
	if *count < 20 {
		return fmt.Errorf("at least 20 samples required")
	}
	variants := map[string][]asset{"bundled": {{*bundleURL, *bundleSHA, true}}, "matching-binary": {{*launcherURL, *launcherSHA, true}, {*binaryURL, *binarySHA, false}}}
	var samples []sample
	for i := 0; i < *count; i++ {
		order := []string{"bundled", "matching-binary"}
		if i%2 == 1 {
			order[0], order[1] = order[1], order[0]
		}
		for _, name := range order {
			dir, e := os.MkdirTemp("", "setup-go-better-acquisition-")
			if e != nil {
				return e
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			s := sample{Index: i, Variant: name}
			start := time.Now()
			for j, a := range variants[name] {
				b, u, transfer, unpack, e := fetch(ctx, a, dir, j)
				if e != nil {
					cancel()
					os.RemoveAll(dir)
					return e
				}
				s.Bytes += b
				s.PeakLogicalBytes += b + u
				s.TransferMS += transfer
				s.UnpackMS += unpack
			}
			s.TotalMS = float64(time.Since(start)) / float64(time.Millisecond)
			cancel()
			if e = os.RemoveAll(dir); e != nil {
				return e
			}
			samples = append(samples, s)
		}
	}
	summaries := map[string]map[string]float64{}
	for name := range variants {
		var v []float64
		for _, s := range samples {
			if s.Variant == name {
				v = append(v, s.TotalMS)
			}
		}
		sort.Float64s(v)
		median := v[len(v)/2]
		if len(v)%2 == 0 {
			median = (median + v[len(v)/2-1]) / 2
		}
		summaries[name] = map[string]float64{"median_ms": median, "p95_ms": v[(95*len(v)+99)/100-1]}
	}
	r := map[string]any{"samples": samples, "summary": summaries, "bundle_url": *bundleURL, "launcher_url": *launcherURL, "binary_url": *binaryURL, "image_os": os.Getenv("ImageOS"), "image_version": os.Getenv("ImageVersion"), "revision": os.Getenv("GITHUB_SHA"), "notes": []string{"Transfer includes checksum verification. ZIP extraction is measured separately.", "Controlled HTTPS acquisition experiment; not GitHub runner action preparation time.", "Peak logical bytes excludes filesystem metadata and allocation rounding. CDN caches are not controlled."}}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(*out, append(b, '\n'), 0644)
}
