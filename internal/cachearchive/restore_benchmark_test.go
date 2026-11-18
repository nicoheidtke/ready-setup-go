package cachearchive

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkRestoreModuleTree includes validated staging, publication and cleanup.
// It is a synthetic local filesystem fixture, not a hosted workload benchmark.
func BenchmarkRestoreModuleTree(b *testing.B) {
	base, data := restoreModuleFixture(b)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := range b.N {
		dest := filepath.Join(base, fmt.Sprintf("dest-%d", i))
		if _, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{}); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := os.RemoveAll(dest); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func restoreModuleFixture(b *testing.B) (string, []byte) {
	b.Helper()
	base := b.TempDir()
	source := filepath.Join(base, "source")
	for i := range 512 {
		name := filepath.Join(source, "example.org", fmt.Sprintf("module%d@v1.0.0", i/16), "sub", "package", fmt.Sprintf("file%d.go", i))
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(name, bytes.Repeat([]byte("some Go module file content\n"), 300), 0444); err != nil {
			b.Fatal(err)
		}
	}
	var data bytes.Buffer
	if _, err := Pack(context.Background(), &data, []Root{{"modules", source}}, Options{}); err != nil {
		b.Fatal(err)
	}
	return base, data.Bytes()
}

// Run this benchmark with worker=1/4 in alternating order for local paired
// diagnosis. Report staging separately: only publication is parallelized.
func BenchmarkRestorePublicationConcurrency(b *testing.B) {
	base, data := restoreModuleFixture(b)
	for _, workers := range []int{1, 4} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			var stageMS, mergeMS float64
			b.ResetTimer()
			for i := range b.N {
				dest := filepath.Join(base, fmt.Sprintf("workers%d-dest%d", workers, i))
				stats, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{RestoreConcurrency: workers})
				if err != nil {
					b.Fatal(err)
				}
				stageMS += stats.StageMS
				mergeMS += stats.MergeMS
				b.StopTimer()
				if err := os.RemoveAll(dest); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			b.ReportMetric(stageMS/float64(b.N), "stage-ms/op")
			b.ReportMetric(mergeMS/float64(b.N), "merge-ms/op")
		})
	}
}

// Existing leaf directories force file-by-file publication rather than missing
// tree moves. Retain this case when measuring private-staging optimizations:
// whole-tree wins must not conceal their cost on the merge fallback path.
func BenchmarkRestoreExistingModuleTree(b *testing.B) {
	base, data := restoreModuleFixture(b)
	var stageMS, mergeMS float64
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		dest := filepath.Join(base, fmt.Sprintf("existing-dest-%d", i))
		for module := range 32 {
			name := filepath.Join(dest, "example.org", fmt.Sprintf("module%d@v1.0.0", module), "sub", "package")
			if err := os.MkdirAll(name, 0755); err != nil {
				b.Fatal(err)
			}
		}
		b.StartTimer()
		stats, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{RestoreConcurrency: 4})
		if err != nil {
			b.Fatal(err)
		}
		stageMS += stats.StageMS
		mergeMS += stats.MergeMS
		b.StopTimer()
		if err := os.RemoveAll(dest); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
	b.ReportMetric(stageMS/float64(b.N), "stage-ms/op")
	b.ReportMetric(mergeMS/float64(b.N), "merge-ms/op")
}
