package cachearchive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
func write(t *testing.T, root, name, body string, mode os.FileMode, mtime time.Time) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func snapshot(t *testing.T, roots []Root, o Options) []byte {
	t.Helper()
	var b bytes.Buffer
	if _, err := Pack(context.Background(), &b, roots, o); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type fixture struct {
	name, body string
	kind       byte
	mode       int64
}

func archive(t *testing.T, entries []fixture) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, e := range entries {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0644
		}
		h := &tar.Header{Name: e.name, Typeflag: kind, Mode: mode, Size: int64(len(e.body)), ModTime: time.Unix(123456, 0)}
		if kind == tar.TypeSymlink || kind == tar.TypeLink {
			h.Linkname = "../../outside"
			h.Size = 0
		}
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := io.WriteString(w, e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestRoundTripDeterministicMerge(t *testing.T) {
	for _, compression := range []string{"none", "gzip"} {
		t.Run(compression, func(t *testing.T) {
			source, dest := tempDir(t), tempDir(t)
			mtime := time.Unix(1700000000, 123456700)
			write(t, source, "example.com/m@v1/readonly.go", "restored", 0444, mtime)
			write(t, source, "example.com/m@v1/collision.go", "snapshot", 0444, mtime)
			write(t, source, "tool", "executable", 0755, mtime)
			dir := filepath.Join(source, "example.com/m@v1")
			if err := os.Chmod(dir, 0555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0755) })
			roots := []Root{{"module", source}}
			o := Options{Compression: compression}
			first, second := snapshot(t, roots, o), snapshot(t, roots, o)
			if !bytes.Equal(first, second) {
				t.Fatal("unchanged snapshot is nondeterministic")
			}
			write(t, dest, "example.com/m@v1/collision.go", "local", 0444, mtime)
			write(t, dest, "unrelated", "keep", 0644, mtime)
			destModule := filepath.Join(dest, "example.com/m@v1")
			os.Chmod(destModule, 0555)
			t.Cleanup(func() { os.Chmod(destModule, 0755) })
			stats, err := Restore(context.Background(), bytes.NewReader(first), []Root{{"module", dest}}, o)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Files != 3 || stats.Bytes != int64(len("restoredsnapshotexecutable")) {
				t.Fatalf("stats: %+v", stats)
			}
			if got := read(t, filepath.Join(dest, "example.com/m@v1/readonly.go")); got != "restored" {
				t.Fatal(got)
			}
			if got := read(t, filepath.Join(dest, "example.com/m@v1/collision.go")); got != "local" {
				t.Fatal(got)
			}
			if read(t, filepath.Join(dest, "unrelated")) != "keep" {
				t.Fatal("lost unrelated file")
			}
			info, err := os.Stat(filepath.Join(dest, "example.com/m@v1/readonly.go"))
			if err != nil {
				t.Fatal(err)
			}
			if !info.ModTime().Equal(mtime) {
				t.Fatalf("mtime: %v want %v", info.ModTime(), mtime)
			}
			if runtime.GOOS != "windows" {
				if info.Mode().Perm() != 0444 {
					t.Fatalf("mode %v", info.Mode())
				}
				info, _ = os.Stat(destModule)
				if info.Mode().Perm() != 0555 {
					t.Fatalf("module directory mode %v", info.Mode())
				}
				info, _ = os.Stat(filepath.Join(dest, "tool"))
				if info.Mode().Perm() != 0755 {
					t.Fatalf("executable mode %v", info.Mode())
				}
			}
			if _, err := Restore(context.Background(), bytes.NewReader(first), []Root{{"module", dest}}, o); err != nil {
				t.Fatal("second merge", err)
			}
		})
	}
}
func TestRejectMaliciousArchivesWithoutDestinationWrites(t *testing.T) {
	cases := map[string][]fixture{
		"traversal":              {{name: "build/../../escaped", body: "bad"}},
		"absolute":               {{name: "/build/file", body: "bad"}},
		"backslash":              {{name: "build/a\\b", body: "bad"}},
		"drive":                  {{name: "build/C:file", body: "bad"}},
		"reserved":               {{name: "build/NUL.txt", body: "bad"}},
		"reserved superscript":   {{name: "build/COM¹.txt", body: "bad"}},
		"trailing dot":           {{name: "build/file.", body: "bad"}},
		"trailing space":         {{name: "build/file ", body: "bad"}},
		"control":                {{name: "build/a\nb", body: "bad"}},
		"duplicate":              {{name: "build/file", body: "one"}, {name: "build/file", body: "two"}},
		"case collision":         {{name: "build/File", body: "one"}, {name: "build/file", body: "two"}},
		"unicode case collision": {{name: "build/Σ", body: "one"}, {name: "build/ς", body: "two"}},
		"implicit parent case":   {{name: "build/Dir/one", body: "one"}, {name: "build/dir/two", body: "two"}},
		"file parent":            {{name: "build/dir", body: "one"}, {name: "build/dir/two", body: "two"}},
		"late file parent":       {{name: "build/dir/two", body: "two"}, {name: "build/dir", body: "one"}},
		"symlink":                {{name: "build/link", kind: tar.TypeSymlink}},
		"hardlink":               {{name: "build/link", kind: tar.TypeLink}},
		"fifo":                   {{name: "build/fifo", kind: tar.TypeFifo}},
		"setuid":                 {{name: "build/file", body: "bad", mode: 04755}},
		"unknown root":           {{name: "other/file", body: "bad"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			dest := tempDir(t)
			write(t, dest, "local", "preserved", 0644, time.Now())
			entries = append([]fixture{{name: "build/new", body: "must not publish"}}, entries...)
			_, err := Restore(context.Background(), bytes.NewReader(archive(t, entries)), []Root{{"build", dest}}, Options{})
			if err == nil {
				t.Fatal("accepted malicious archive")
			}
			if read(t, filepath.Join(dest, "local")) != "preserved" {
				t.Fatal("changed local file")
			}
			if _, err := os.Stat(filepath.Join(dest, "new")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("published before validation: %v", err)
			}
		})
	}
}
func TestCorruptionAndLimitsPreserveLocalData(t *testing.T) {
	valid := archive(t, []fixture{{name: "build/new", body: strings.Repeat("x", 2048)}})
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	gz.Write(valid)
	gz.Close()
	damaged := append([]byte(nil), compressed.Bytes()...)
	damaged[len(damaged)-8] ^= 1
	cases := []struct {
		name string
		data []byte
		opts Options
	}{
		{"truncated data", valid[:700], Options{}},
		{"missing terminator", valid[:len(valid)-1024], Options{}},
		{"one terminator", valid[:len(valid)-512], Options{}},
		{"nonzero trailer", append(append([]byte(nil), valid...), 1), Options{}},
		{"gzip checksum", damaged, Options{Compression: "gzip"}},
		{"gzip truncated", compressed.Bytes()[:compressed.Len()-3], Options{Compression: "gzip"}},
		{"size bound", valid, Options{MaxBytes: 1024}},
		{"gzip bomb bound", compressed.Bytes(), Options{Compression: "gzip", MaxBytes: 1024}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parent := tempDir(t)
			dest := filepath.Join(parent, "absent")
			if _, err := Restore(context.Background(), bytes.NewReader(c.data), []Root{{"build", dest}}, c.opts); err == nil {
				t.Fatal("accepted corrupt/oversize archive")
			}
			if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("destination created before validation: %v", err)
			}
		})
	}
	if _, err := Restore(context.Background(), bytes.NewReader(valid), []Root{{"build", tempDir(t)}}, Options{MaxBytes: int64(len(valid))}); err != nil {
		t.Fatal("exact bound", err)
	}
}
func TestDestinationSymlinksAndConflicts(t *testing.T) {
	outside, dest := tempDir(t), tempDir(t)
	write(t, outside, "existing", "untouched", 0644, time.Now())
	if err := os.Symlink(outside, filepath.Join(dest, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, name := range []string{"build/link/new", "build/link/existing"} {
		data := archive(t, []fixture{{name: "build/normal", body: "no publication"}, {name: name, body: "bad"}})
		if _, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"build", dest}}, Options{}); err == nil {
			t.Fatal("followed destination symlink")
		}
	}
	if read(t, filepath.Join(outside, "existing")) != "untouched" {
		t.Fatal("modified outside root")
	}
	if _, err := os.Stat(filepath.Join(dest, "normal")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preflight failed after write")
	}
	if _, err := Pack(context.Background(), io.Discard, []Root{{"build", dest}}, Options{}); err == nil {
		t.Fatal("packed symlink")
	}
}
func TestDestinationConflictIsPreflighted(t *testing.T) {
	dest := tempDir(t)
	write(t, dest, "dir", "local file", 0644, time.Now())
	data := archive(t, []fixture{{name: "build/new", body: "new"}, {name: "build/dir/child", body: "bad"}})
	if _, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"build", dest}}, Options{}); err == nil {
		t.Fatal("accepted conflicting destination")
	}
	if _, err := os.Stat(filepath.Join(dest, "new")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published before conflict preflight")
	}
}
func TestBuildTrimIsOptInAndModuleSafe(t *testing.T) {
	build, module := tempDir(t), tempDir(t)
	old, newer := time.Unix(1700000000, 0), time.Unix(1700100000, 0)
	oldName := "ab/ab" + strings.Repeat("0", 62) + "-d"
	freshName := "cd/cd" + strings.Repeat("0", 62) + "-a"
	write(t, build, oldName, "old", 0644, old)
	write(t, build, freshName, "fresh", 0644, newer)
	write(t, build, "README", "keep unknown", 0644, old)
	write(t, module, oldName, "module", 0444, old)
	roots := []Root{{"module", module}, {"build", build}}
	for _, trim := range []bool{false, true} {
		o := Options{}
		if trim {
			o.TrimBefore = old.Add(time.Hour)
		}
		data := snapshot(t, roots, o)
		destBuild, destModule := tempDir(t), tempDir(t)
		stats, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"build", destBuild}, {"module", destModule}}, o)
		if err != nil {
			t.Fatal(err)
		}
		want := int64(4)
		if trim {
			want = 3
		}
		if stats.Files != want {
			t.Fatalf("files %d want %d", stats.Files, want)
		}
		_, err = os.Stat(filepath.Join(destBuild, filepath.FromSlash(oldName)))
		if trim && !errors.Is(err, os.ErrNotExist) {
			t.Fatal("old object restored")
		}
		if !trim && err != nil {
			t.Fatal(err)
		}
		if read(t, filepath.Join(destModule, filepath.FromSlash(oldName))) != "module" {
			t.Fatal("trimmed module")
		}
	}
	if read(t, filepath.Join(build, filepath.FromSlash(oldName))) != "old" {
		t.Fatal("trim deleted local data")
	}
}
func TestCancellationAndOptionValidation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := tempDir(t)
	write(t, source, "file", "data", 0644, time.Now())
	if _, err := Pack(ctx, io.Discard, []Root{{"build", source}}, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pack cancellation: %v", err)
	}
	data := snapshot(t, []Root{{"build", source}}, Options{Compression: "gzip"})
	dest := filepath.Join(tempDir(t), "absent")
	if _, err := Restore(ctx, bytes.NewReader(data), []Root{{"build", dest}}, Options{Compression: "gzip"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("restore cancellation: %v", err)
	}
	for _, o := range []Options{{Compression: "zip"}, {MaxBytes: -1}, {MaxBytes: 512}} {
		if _, err := Pack(context.Background(), io.Discard, []Root{{"build", source}}, o); err == nil {
			t.Fatalf("accepted options %+v", o)
		}
	}
	for _, roots := range [][]Root{{{"../bad", source}}, {{"build", source}, {"BUILD", source}}, {{"build", ""}}} {
		if _, err := Pack(context.Background(), io.Discard, roots, Options{}); err == nil {
			t.Fatal("accepted invalid roots")
		}
	}
}

func TestSnapshotRoundtripByteIdentity(t *testing.T) {
	for _, compression := range []string{"none", "gzip"} {
		t.Run(compression, func(t *testing.T) {
			source, dest := tempDir(t), filepath.Join(tempDir(t), "new-root")
			write(t, source, "dir/file", "module payload", 0444, time.Unix(1700000000, 123456700))
			dir := filepath.Join(source, "dir")
			if err := os.Chmod(dir, 0555); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(dir, 0755); os.Chmod(filepath.Join(dest, "dir"), 0755) })
			o := Options{Compression: compression}
			before := snapshot(t, []Root{{"module", source}}, o)
			if _, err := Restore(context.Background(), bytes.NewReader(before), []Root{{"module", dest}}, o); err != nil {
				t.Fatal(err)
			}
			after := snapshot(t, []Root{{"module", dest}}, o)
			if !bytes.Equal(before, after) {
				t.Fatal("empty destination roundtrip changed archive bytes")
			}
		})
	}
}
