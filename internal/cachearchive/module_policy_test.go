package cachearchive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

func TestPackContentIdentity(t *testing.T) {
	dir := tempDir(t)
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	roots := []Root{{Name: "modules", Path: dir}}
	for _, codec := range []string{"none", "gzip", "zstd"} {
		t.Run(codec, func(t *testing.T) {
			var plain, identified bytes.Buffer
			opts := Options{Compression: codec}
			ordinary, err := Pack(context.Background(), &plain, roots, opts)
			if err != nil || ordinary.ContentDigest != "" {
				t.Fatalf("ordinary packing must not hash content: %+v %v", ordinary, err)
			}
			opts.ContentIdentity = true
			packed, err := Pack(context.Background(), &identified, roots, opts)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint, err := Fingerprint(context.Background(), roots, 0)
			if err != nil || packed.ContentDigest == "" || packed.ContentDigest != fingerprint.ContentDigest {
				t.Fatalf("pack/fingerprint mismatch: %+v %+v %v", packed, fingerprint, err)
			}
			if !bytes.Equal(plain.Bytes(), identified.Bytes()) {
				t.Fatal("content hashing changed archive bytes")
			}
			restored, err := Restore(context.Background(), bytes.NewReader(identified.Bytes()), []Root{{Name: "modules", Path: filepath.Join(tempDir(t), "restore")}}, opts)
			if err != nil || restored.ContentDigest != packed.ContentDigest {
				t.Fatalf("restore identity mismatch: %+v %v", restored, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Pack(ctx, io.Discard, roots, Options{ContentIdentity: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := Pack(context.Background(), io.Discard, roots, Options{ContentIdentity: true, MaxBytes: 512}); err == nil {
		t.Fatal("archive limit ignored")
	}
}

// Both policies use production walkers/codecs. Fused packing is experimental:
// it always creates an archive, even if its content digest avoids an upload.
// Counters describe logical source reads and temporary file bytes, not disk I/O.
type modulePolicySample struct {
	elapsed                     time.Duration
	sourceBytes, temporaryBytes int64
	archiveBytes                int64
	skipped                     bool
}

func measureModulePolicy(ctx context.Context, roots []Root, baseline, codec, policy, temporary string) (sample modulePolicySample, err error) {
	started := time.Now()
	defer func() { sample.elapsed = time.Since(started) }()
	if policy == "check-first" {
		checked, err := Fingerprint(ctx, roots, 0)
		if err != nil {
			return sample, err
		}
		sample.sourceBytes += checked.Bytes
		if checked.ContentDigest == baseline {
			sample.skipped = true
			return sample, nil
		}
	} else if policy != "pack-and-hash" {
		return sample, fmt.Errorf("unknown module policy")
	}
	f, err := os.CreateTemp(temporary, "module-policy-*")
	if err != nil {
		return sample, err
	}
	defer func() { err = errors.Join(err, f.Close(), os.Remove(f.Name())) }()
	packed, err := Pack(ctx, f, roots, Options{Compression: codec, ContentIdentity: policy == "pack-and-hash"})
	if err != nil {
		return sample, err
	}
	sample.sourceBytes += packed.Bytes
	sample.temporaryBytes, err = f.Seek(0, io.SeekCurrent)
	sample.skipped = policy == "pack-and-hash" && packed.ContentDigest == baseline
	if !sample.skipped {
		sample.archiveBytes = sample.temporaryBytes
	}
	return sample, err
}

func modulePolicyCorpus(t testing.TB, files, size int) []Root {
	t.Helper()
	dir := tempDir(t)
	random := rand.New(rand.NewPCG(1, 2))
	payload := make([]byte, size)
	stamp := time.Unix(1700000000, 0)
	const source = "package example\n// synthetic module fixture\n"
	for i := range files {
		// Alternate repetitive source-like and pseudo-random payloads. Fixed
		// seeds and mtimes keep each policy's corpus and archive identical.
		for j := range payload {
			if i%2 == 0 {
				payload[j] = source[j%len(source)]
			} else {
				payload[j] = byte(random.Uint32())
			}
		}
		name := filepath.Join(dir, fmt.Sprintf("module-%04d", i))
		if err := os.WriteFile(name, payload, 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(name, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	return []Root{{Name: "modules", Path: dir}}
}

func modulePolicyBaseline(t testing.TB, roots []Root, changed bool) string {
	t.Helper()
	baseline, err := Fingerprint(context.Background(), roots, 0)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		name := filepath.Join(roots[0].Path, "added-source")
		if err := os.WriteFile(name, []byte("package added\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return baseline.ContentDigest
}

func TestModulePolicyDecisions(t *testing.T) {
	for _, changed := range []bool{false, true} {
		roots := modulePolicyCorpus(t, 4, 1024)
		baseline := modulePolicyBaseline(t, roots, changed)
		var previous modulePolicySample
		for _, policy := range []string{"check-first", "pack-and-hash"} {
			sample, err := measureModulePolicy(context.Background(), roots, baseline, "gzip", policy, tempDir(t))
			if err != nil || sample.skipped == changed {
				t.Fatalf("changed=%v policy=%s sample=%+v err=%v", changed, policy, sample, err)
			}
			if policy == "pack-and-hash" {
				if changed && (previous.sourceBytes != 2*sample.sourceBytes || previous.archiveBytes != sample.archiveBytes) {
					t.Fatal("changed-policy read/archive counters disagree")
				}
				if !changed && (previous.temporaryBytes != 0 || sample.temporaryBytes == 0 || previous.sourceBytes != sample.sourceBytes) {
					t.Fatal("unchanged-policy temporary/read counters disagree")
				}
			}
			previous = sample
		}
	}
}

// Opt-in diagnostic: raw logs retain all 20 alternating pairs per comparison.
// Failure exits the test with preceding observations still in its output.
func TestMeasureModuleSavePolicies(t *testing.T) {
	if os.Getenv("SETUP_GO_FASTER_MODULE_POLICY_MEASURE") != "1" {
		t.Skip("set SETUP_GO_FASTER_MODULE_POLICY_MEASURE=1 for synthetic paired diagnosis")
	}
	t.Logf("go=%s os=%s arch=%s gomaxprocs=%d synthetic warm-page-cache local-only; CPU/RSS/physical I/O/network unmeasured", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0))
	for _, corpus := range []struct {
		name        string
		files, size int
	}{{"small-files", 256, 4096}, {"larger-files", 16, 256 << 10}} {
		for _, changed := range []bool{false, true} {
			roots := modulePolicyCorpus(t, corpus.files, corpus.size)
			baseline := modulePolicyBaseline(t, roots, changed)
			for _, codec := range []string{"none", "gzip", "zstd"} {
				var deltas []float64
				temporary := tempDir(t)
				for pair := range 20 {
					order := []string{"check-first", "pack-and-hash"}
					if pair%2 == 1 {
						order[0], order[1] = order[1], order[0]
					}
					results := map[string]float64{}
					for _, policy := range order {
						sample, err := measureModulePolicy(t.Context(), roots, baseline, codec, policy, temporary)
						if err != nil {
							t.Fatal(err)
						}
						elapsed := float64(sample.elapsed) / float64(time.Millisecond)
						results[policy] = elapsed
						t.Logf("corpus=%s changed=%v codec=%s pair=%d policy=%s ms=%.3f logical_source_bytes=%d temporary_bytes=%d archive_bytes=%d skipped=%v", corpus.name, changed, codec, pair, policy, elapsed, sample.sourceBytes, sample.temporaryBytes, sample.archiveBytes, sample.skipped)
					}
					deltas = append(deltas, results["pack-and-hash"]-results["check-first"])
				}
				sort.Float64s(deltas)
				t.Logf("corpus=%s changed=%v codec=%s successful_pairs=20 pack_and_hash_minus_check_first_median_ms=%.3f p95_ms=%.3f", corpus.name, changed, codec, (deltas[9]+deltas[10])/2, deltas[18])
			}
		}
	}
}

// Select one policy per CPU profile; the profile includes fixture setup and Go
// test overhead. B/op is allocation volume, not peak memory or CPU time.
func BenchmarkModuleSavePolicy(b *testing.B) {
	for _, changed := range []bool{false, true} {
		for _, policy := range []string{"check-first", "pack-and-hash"} {
			b.Run(fmt.Sprintf("changed=%v/%s", changed, policy), func(b *testing.B) {
				roots := modulePolicyCorpus(b, 64, 64<<10)
				baseline := modulePolicyBaseline(b, roots, changed)
				temporary := b.TempDir()
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					sample, err := measureModulePolicy(b.Context(), roots, baseline, "gzip", policy, temporary)
					if err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(sample.sourceBytes), "source-B/op")
					b.ReportMetric(float64(sample.temporaryBytes), "temp-B/op")
					b.ReportMetric(float64(sample.archiveBytes), "archive-B/op")
				}
			})
		}
	}
}
