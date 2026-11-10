package cachearchive

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"sync"
)

// Fixed member boundaries make gzip output independent of worker scheduling and
// Write call sizes. Readers see one continuous tar stream through gzip's normal
// concatenated-member support. Each member retains the standard compression level.
const gzipBlockBytes = 4 << 20

type gzipJob struct {
	raw     []byte
	encoded *bytes.Buffer
	err     error
	done    chan struct{}
}

// parallelGzipWriter bounds submitted, not merely active, blocks. A slow first
// block or destination therefore cannot grow an unbounded reorder queue. The
// steady payload-buffer allowance is three block-sized buffers per worker plus
// one producer block. Codec state, buffer growth and GC/runtime overhead are
// additional; this allowance is not a strict process-memory/RSS bound.
type parallelGzipWriter struct {
	ctx         context.Context
	cancel      context.CancelFunc
	dst         io.Writer
	blockSize   int
	jobs        chan *gzipJob
	ordered     chan *gzipJob
	slots       chan struct{}
	workers     sync.WaitGroup
	collected   chan struct{}
	rawPool     sync.Pool
	encodedPool sync.Pool
	current     []byte
	submitted   int
	closed      bool
	closeErr    error
	errMu       sync.Mutex
	err         error
}

func newParallelGzipWriter(ctx context.Context, dst io.Writer, workers int) *parallelGzipWriter {
	return newBlockedGzipWriter(ctx, dst, workers, gzipBlockBytes)
}

func newBlockedGzipWriter(ctx context.Context, dst io.Writer, workers, blockSize int) *parallelGzipWriter {
	ctx, cancel := context.WithCancel(ctx)
	p := &parallelGzipWriter{ctx: ctx, cancel: cancel, dst: dst, blockSize: blockSize,
		jobs: make(chan *gzipJob), ordered: make(chan *gzipJob, workers),
		slots: make(chan struct{}, workers), collected: make(chan struct{})}
	p.rawPool.New = func() any { return make([]byte, 0, blockSize) }
	p.encodedPool.New = func() any { return &bytes.Buffer{} }
	for range workers {
		p.workers.Add(1)
		go p.compress()
	}
	go p.collect()
	return p
}

func (p *parallelGzipWriter) fail(err error) {
	if err == nil {
		return
	}
	p.errMu.Lock()
	if p.err == nil {
		p.err = err
		p.cancel()
	}
	p.errMu.Unlock()
}

func (p *parallelGzipWriter) failure() error {
	p.errMu.Lock()
	err := p.err
	p.errMu.Unlock()
	if err != nil {
		return err
	}
	return p.ctx.Err()
}

func (p *parallelGzipWriter) compress() {
	defer p.workers.Done()
	gz := gzip.NewWriter(io.Discard)
	for job := range p.jobs {
		if job.err = p.ctx.Err(); job.err == nil {
			job.encoded = p.encodedPool.Get().(*bytes.Buffer)
			job.encoded.Reset()
			gz.Reset(job.encoded)
			_, writeErr := gz.Write(job.raw)
			job.err = errors.Join(writeErr, gz.Close(), p.ctx.Err())
		}
		close(job.done)
	}
}

func (p *parallelGzipWriter) collect() {
	defer close(p.collected)
	for job := range p.ordered {
		<-job.done
		p.fail(job.err)
		if p.failure() == nil {
			data := job.encoded.Bytes()
			n, err := p.dst.Write(data)
			if err == nil && n != len(data) {
				err = io.ErrShortWrite
			}
			p.fail(err)
		}
		if job.encoded != nil {
			p.encodedPool.Put(job.encoded)
		}
		p.rawPool.Put(job.raw[:0])
		<-p.slots
	}
}

func (p *parallelGzipWriter) submit() error {
	if err := p.failure(); err != nil {
		return err
	}
	select {
	case p.slots <- struct{}{}:
	case <-p.ctx.Done():
		return p.failure()
	}
	job := &gzipJob{raw: p.current, done: make(chan struct{})}
	select {
	case p.jobs <- job:
		// A slot reserves ordered-channel space until the collector releases it.
		p.ordered <- job
		p.current = nil
		p.submitted++
		return nil
	case <-p.ctx.Done():
		<-p.slots
		return p.failure()
	}
}

func (p *parallelGzipWriter) Write(data []byte) (n int, err error) {
	if p.closed {
		return 0, errors.New("write to closed gzip archive")
	}
	if err := p.failure(); err != nil {
		return 0, err
	}
	for len(data) > 0 {
		if p.current == nil {
			p.current = p.rawPool.Get().([]byte)[:0]
		}
		count := min(len(data), p.blockSize-len(p.current))
		p.current = append(p.current, data[:count]...)
		data, n = data[count:], n+count
		if len(p.current) == p.blockSize {
			if err := p.submit(); err != nil {
				return n, err
			}
		}
	}
	return n, p.failure()
}

func (p *parallelGzipWriter) Close() error {
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	if p.failure() == nil && (len(p.current) > 0 || p.submitted == 0) {
		p.fail(p.submit())
	}
	close(p.jobs)
	close(p.ordered)
	p.workers.Wait()
	<-p.collected
	err := p.failure()
	p.closeErr = err
	p.cancel()
	return err
}
