// acquisition measures transfer and unpack costs outside GitHub's bundled
// "Set up job" step. It is a controlled experiment, not a claim about runner timings.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	Success          bool    `json:"success"`
	Failure          string  `json:"failure,omitempty"`
}

type pairedSummary struct {
	Status        string  `json:"status"`
	Count         int     `json:"successful_pairs"`
	Incomplete    int     `json:"failed_or_incomplete_pairs"`
	DeltaMedianMS float64 `json:"delta_median_ms"`
	DeltaP95MS    float64 `json:"delta_p95_ms"`
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
	extracting := false
	defer func() {
		elapsed := float64(time.Since(start)) / float64(time.Millisecond)
		if extracting {
			unpack = elapsed
		} else {
			transfer = elapsed
		}
	}()
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
	extracting = true
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
	return runWith(os.Args[1:], fetch)
}

func runWith(args []string, fetcher fetchAsset) error {
	fs := flag.NewFlagSet("acquisition", flag.ContinueOnError)
	bundleURL := fs.String("bundle-url", "", "immutable ZIP containing the bundled action")
	bundleSHA := fs.String("bundle-sha256", "", "expected bundle SHA-256")
	launcherURL := fs.String("launcher-url", "", "immutable ZIP of the same launcher without binaries")
	launcherSHA := fs.String("launcher-sha256", "", "expected launcher ZIP SHA-256")
	binaryURL := fs.String("binary-url", "", "immutable matching native executable")
	binarySHA := fs.String("binary-sha256", "", "expected executable SHA-256")
	count := fs.Int("samples", 20, "samples per variant, at least 20")
	out := fs.String("out", "acquisition.json", "report path")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	for _, p := range [][2]string{{*bundleURL, *bundleSHA}, {*launcherURL, *launcherSHA}, {*binaryURL, *binarySHA}} {
		sum, e := hex.DecodeString(p[1])
		u, parseErr := url.Parse(p[0])
		if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || e != nil || len(sum) != 32 {
			return fmt.Errorf("all three public HTTPS URLs without credentials, queries or fragments and SHA-256 values are required")
		}
	}
	if *count < 20 {
		return fmt.Errorf("at least 20 samples required")
	}
	variants := map[string][]asset{"bundled": {{*bundleURL, *bundleSHA, true}}, "matching-binary": {{*launcherURL, *launcherSHA, true}, {*binaryURL, *binarySHA, false}}}
	samples, measurementErr := measure(context.Background(), variants, *count, fetcher)
	summaries, paired := summarize(samples)
	r := map[string]any{"samples": samples, "summary": summaries, "paired_summary": paired, "requested_pairs": *count, "complete": measurementErr == nil, "bundle_url": *bundleURL, "launcher_url": *launcherURL, "binary_url": *binaryURL, "image_os": os.Getenv("ImageOS"), "image_version": os.Getenv("ImageVersion"), "revision": os.Getenv("GITHUB_SHA"), "notes": []string{"Transfer includes checksum verification. ZIP extraction is measured separately.", "Controlled HTTPS acquisition experiment; not GitHub runner action preparation time.", "Peak logical bytes excludes filesystem metadata and allocation rounding. CDN caches are not controlled.", "Paired deltas are matching-binary minus bundled; only successful complete pairs contribute. Inspect retained failures and incomplete status.", "Failure fields contain fixed categories, never raw transport errors or remote content. Asset URLs must be public and contain no credentials, query or fragment."}}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return errors.Join(measurementErr, e)
	}
	return errors.Join(measurementErr, os.WriteFile(*out, append(b, '\n'), 0600))
}

type fetchAsset func(context.Context, asset, string, int) (int64, int64, float64, float64, error)

// Stop at the first failure, retaining earlier samples and partial counters for
// the failed observation. Raw errors may contain credentials and are excluded.
func measure(parent context.Context, variants map[string][]asset, count int, fetch fetchAsset) ([]sample, error) {
	var samples []sample
	for i := 0; i < count; i++ {
		order := []string{"bundled", "matching-binary"}
		if i%2 == 1 {
			order[0], order[1] = order[1], order[0]
		}
		for _, name := range order {
			s := sample{Index: i, Variant: name}
			dir, e := os.MkdirTemp("", "setup-go-faster-acquisition-")
			if e != nil {
				s.Failure = "staging-failed"
				return append(samples, s), errors.New("acquisition staging failed; partial observations retained")
			}
			ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
			start := time.Now()
			for j, a := range variants[name] {
				b, u, transfer, unpack, e := fetch(ctx, a, dir, j)
				s.Bytes += b
				s.PeakLogicalBytes += b + u
				s.TransferMS += transfer
				s.UnpackMS += unpack
				if e != nil {
					s.Failure = "asset-fetch-failed"
					if ctx.Err() != nil {
						s.Failure = "cancelled-or-timed-out"
					}
					break
				}
			}
			s.TotalMS = float64(time.Since(start)) / float64(time.Millisecond)
			cancel()
			if e = os.RemoveAll(dir); e != nil {
				// Cleanup failure invalidates this sample too; do not publish success.
				s.Failure = "cleanup-failed"
			}
			s.Success = s.Failure == ""
			samples = append(samples, s)
			if !s.Success {
				return samples, errors.New("acquisition sample failed; partial observations retained")
			}
		}
	}
	return samples, nil
}

func summarize(samples []sample) (map[string]map[string]float64, pairedSummary) {
	summaries := map[string]map[string]float64{}
	for _, name := range []string{"bundled", "matching-binary"} {
		var v []float64
		for _, s := range samples {
			if s.Variant == name && s.Success {
				v = append(v, s.TotalMS)
			}
		}
		if len(v) > 0 {
			summaries[name] = map[string]float64{"median_ms": percentile(v, 50), "p95_ms": percentile(v, 95)}
		}
	}
	pairs := map[int]map[string]sample{}
	for _, s := range samples {
		if pairs[s.Index] == nil {
			pairs[s.Index] = map[string]sample{}
		}
		pairs[s.Index][s.Variant] = s
	}
	paired := pairedSummary{Status: "incomplete: fewer than 20 successful pairs"}
	var deltas []float64
	for _, pair := range pairs {
		a, b := pair["bundled"], pair["matching-binary"]
		if !a.Success || !b.Success {
			paired.Incomplete++
			continue
		}
		deltas = append(deltas, b.TotalMS-a.TotalMS)
	}
	paired.Count = len(deltas)
	if len(deltas) > 0 {
		paired.DeltaMedianMS, paired.DeltaP95MS = percentile(deltas, 50), percentile(deltas, 95)
	}
	if paired.Count >= 20 {
		paired.Status = "descriptive paired measurements; inspect failures and repeat across platforms before drawing conclusions"
	}
	return summaries, paired
}

func percentile(values []float64, p int) float64 {
	sort.Float64s(values)
	n := len(values)
	if p == 50 && n%2 == 0 {
		return (values[n/2-1] + values[n/2]) / 2
	}
	return values[(p*n+99)/100-1]
}
