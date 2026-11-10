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
	b.SetBytes(int64(data.Len()))
	b.ResetTimer()
	for i := range b.N {
		dest := filepath.Join(base, fmt.Sprintf("dest-%d", i))
		if _, err := Restore(context.Background(), bytes.NewReader(data.Bytes()), []Root{{"modules", dest}}, Options{}); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := os.RemoveAll(dest); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
