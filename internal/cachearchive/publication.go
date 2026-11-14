package cachearchive

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"

	"setup-go-faster/internal/actionlog"
)

type publicationJob struct {
	root         *os.Root
	name, source string
	hdr          *tar.Header
	private      bool
}

// publishFiles owns the worker lifetime. Every worker has private counters and
// a fixed copy buffer; callers may clean up staging and restore directory modes
// only after this function joins all workers, including on failure/cancellation.
func publishFiles(ctx context.Context, jobs []publicationJob, workers int, publish func(context.Context, publicationJob, []byte, *Stats) error) (total Stats, err error) {
	if workers < 1 || workers > 16 {
		return total, errors.New("restore publication concurrency must be 1..16")
	}
	if err := ctx.Err(); err != nil {
		return total, err
	}
	if len(jobs) == 0 {
		return total, nil
	}
	workers = min(workers, len(jobs))
	progress := actionlog.Default.Progress("Publish validated cache files", "files published", 0)
	defer func() { progress.Finish(err) }()
	if workers == 1 {
		buf := make([]byte, 32*1024)
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			before := total.PublishedFiles
			if err := publish(ctx, job, buf, &total); err != nil {
				return total, err
			}
			progress.Add(total.PublishedFiles - before)
		}
		return total, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan publicationJob)
	results := make([]Stats, workers)
	var joined sync.WaitGroup
	var first sync.Once
	var firstErr error
	for worker := range workers {
		joined.Add(1)
		go func() {
			defer joined.Done()
			buf := make([]byte, 32*1024)
			for job := range queue {
				if ctx.Err() != nil {
					return
				}
				before := results[worker].PublishedFiles
				if err := publish(ctx, job, buf, &results[worker]); err != nil {
					first.Do(func() { firstErr = err; cancel() })
					return
				}
				progress.Add(results[worker].PublishedFiles - before)
			}
		}()
	}
feed:
	for _, job := range jobs {
		select {
		case queue <- job:
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	joined.Wait()
	for _, result := range results {
		total.PublishedFiles += result.PublishedFiles
		total.LinkedFiles += result.LinkedFiles
		total.CopiedFiles += result.CopiedFiles
		total.CopiedBytes += result.CopiedBytes
	}
	if firstErr != nil {
		return total, firstErr
	}
	return total, ctx.Err()
}

func publishFile(ctx context.Context, job publicationJob, buf []byte, stats *Stats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, name := job.root, job.name
	if job.private {
		if linkErr := r.Link(job.source, name); linkErr == nil {
			// Unlink writable staging before setting Windows read-only attributes.
			if err := r.Remove(job.source); err != nil {
				r.Remove(name)
				return err
			}
			if err := r.Chmod(name, fs.FileMode(job.hdr.Mode)); err != nil {
				r.Chmod(name, 0600)
				r.Remove(name)
				return err
			}
			if err := r.Chtimes(name, job.hdr.ModTime, job.hdr.ModTime); err != nil {
				r.Chmod(name, 0600)
				r.Remove(name)
				return err
			}
			stats.PublishedFiles++
			stats.LinkedFiles++
			return nil
		} else if errors.Is(linkErr, os.ErrExist) {
			info, err := r.Lstat(name)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsafe destination %q", name)
			}
			return nil
		}
	}
	openSource := func() (*os.File, error) {
		if job.private {
			return r.Open(job.source)
		}
		return os.Open(job.source)
	}
	return publishCopy(ctx, r, name, *job.hdr, openSource, buf, stats)
}
