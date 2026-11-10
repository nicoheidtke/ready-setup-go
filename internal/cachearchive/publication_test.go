package cachearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicationWorkerBoundAndCounterReduction(t *testing.T) {
	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			jobs := make([]publicationJob, 9)
			started, release := make(chan struct{}, len(jobs)), make(chan struct{})
			var active, peak atomic.Int32
			done := make(chan struct{})
			var result Stats
			var resultErr error
			go func() {
				result, resultErr = publishFiles(context.Background(), jobs, workers, func(_ context.Context, _ publicationJob, buf []byte, stats *Stats) error {
					n := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
					}
					started <- struct{}{}
					<-release
					if len(buf) != 32*1024 {
						return errors.New("unexpected worker buffer size")
					}
					stats.PublishedFiles++
					stats.CopiedFiles++
					stats.CopiedBytes += 7
					return nil
				})
				close(done)
			}()
			for range workers {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("workers did not start")
				}
			}
			select {
			case <-started:
				t.Fatal("exceeded worker bound")
			default:
			}
			for range jobs {
				release <- struct{}{}
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("workers did not join")
			}
			if resultErr != nil || peak.Load() != int32(workers) || active.Load() != 0 || result.PublishedFiles != 9 || result.CopiedFiles != 9 || result.CopiedBytes != 63 {
				t.Fatalf("invalid worker result: %+v err=%v peak=%d active=%d", result, resultErr, peak.Load(), active.Load())
			}
		})
	}
}

func TestPublicationCancellationAndFailureJoinWorkers(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started, failNow := make(chan struct{}, 3), make(chan struct{})
			jobs := make([]publicationJob, 20)
			for i := range jobs {
				jobs[i].name = fmt.Sprint(i)
			}
			var active atomic.Int32
			done := make(chan error, 1)
			failure := errors.New("publication failed")
			go func() {
				_, err := publishFiles(ctx, jobs, 3, func(ctx context.Context, job publicationJob, _ []byte, _ *Stats) error {
					active.Add(1)
					defer active.Add(-1)
					started <- struct{}{}
					if fail && job.name == "0" {
						<-failNow
						return failure
					}
					<-ctx.Done()
					return ctx.Err()
				})
				done <- err
			}()
			for range 3 {
				select {
				case <-started:
				case <-time.After(5 * time.Second):
					t.Fatal("workers did not start")
				}
			}
			if fail {
				close(failNow)
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if (fail && !errors.Is(err, failure)) || (!fail && !errors.Is(err, context.Canceled)) {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("failed workers did not join")
			}
			if active.Load() != 0 {
				t.Fatal("returned with active workers")
			}
		})
	}
}

func TestParallelRestorePreservesModesTimesAndLocalFiles(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprint(workers), func(t *testing.T) {
			parent := tempDir(t)
			source, dest := filepath.Join(parent, "source"), filepath.Join(parent, "dest")
			stamp := time.Unix(1700000000, 123456700)
			for i := range 32 {
				write(t, source, fmt.Sprintf("module@v1/sub/file%d", i), fmt.Sprint(i), 0444, stamp)
			}
			write(t, source, "new-readonly/deep/file", "new readonly tree", 0444, stamp)
			if err := os.Chmod(filepath.Join(source, "new-readonly"), 0555); err != nil {
				t.Fatal(err)
			}
			write(t, dest, "module@v1/sub/file7", "local wins", 0444, stamp)
			if err := os.Chmod(filepath.Join(source, "module@v1"), 0555); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(dest, "module@v1"), 0555); err != nil {
				t.Fatal(err)
			}
			data := snapshot(t, []Root{{"modules", source}}, Options{})
			stats, err := Restore(context.Background(), bytes.NewReader(data), []Root{{"modules", dest}}, Options{RestoreConcurrency: workers})
			if err != nil {
				t.Fatal(err)
			}
			if stats.PublishedFiles != 32 || stats.LinkedFiles != 32 || stats.CopiedBytes != 0 {
				t.Fatalf("bad publication stats: %+v", stats)
			}
			for i := range 32 {
				name := filepath.Join(dest, "module@v1/sub", fmt.Sprintf("file%d", i))
				want := fmt.Sprint(i)
				if i == 7 {
					want = "local wins"
				}
				if read(t, name) != want {
					t.Fatalf("wrong file %d", i)
				}
				info, err := os.Stat(name)
				if err != nil || !info.ModTime().Equal(stamp) || info.Mode().Perm()&0200 != 0 {
					t.Fatalf("metadata %d: %v %v", i, info, err)
				}
			}
			info, err := os.Stat(filepath.Join(dest, "module@v1"))
			if err != nil || info.Mode().Perm()&0200 != 0 {
				t.Fatalf("existing directory mode changed: %v %v", info, err)
			}
			info, err = os.Stat(filepath.Join(dest, "new-readonly"))
			if err != nil || info.Mode().Perm()&0200 != 0 || !info.ModTime().Equal(time.Unix(0, 0)) {
				t.Fatalf("new directory metadata not restored after publication: %v %v", info, err)
			}
			if read(t, filepath.Join(dest, "new-readonly/deep/file")) != "new readonly tree" {
				t.Fatal("new readonly tree was not populated")
			}
		})
	}
}

func TestPublicationCollisionAfterPreparationKeepsLocalFile(t *testing.T) {
	dir := tempDir(t)
	write(t, dir, "private/source", "archive", 0600, time.Unix(1, 0))
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	write(t, dir, "target", "local wins", 0444, time.Unix(2, 0))
	job := publicationJob{root: r, name: "target", source: "private/source", private: true, hdr: &tar.Header{Mode: 0444, ModTime: time.Unix(1, 0)}}
	stats := Stats{}
	if err := publishFile(context.Background(), job, make([]byte, 32768), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.PublishedFiles != 0 || read(t, filepath.Join(dir, "target")) != "local wins" {
		t.Fatal("publication replaced late local file")
	}
	if err := os.Chmod(filepath.Join(dir, "target"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "target")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "target"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := publishFile(context.Background(), job, make([]byte, 32768), &stats); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("accepted incompatible late directory: %v", err)
	}
}
