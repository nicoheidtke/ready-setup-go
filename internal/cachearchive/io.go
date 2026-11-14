package cachearchive

import (
	"context"
	"errors"
	"io"
)

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type boundedWriter struct {
	ctx  context.Context
	w    io.Writer
	left int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.left {
		return 0, errors.New("archive exceeds uncompressed size limit")
	}
	n, err := w.w.Write(p)
	w.left -= int64(n)
	return n, err
}

type boundedReader struct {
	r    io.Reader
	left int64
	read int64
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if r.left < 0 {
		return 0, errors.New("archive exceeds uncompressed size limit")
	}
	if int64(len(p)) > r.left {
		p = p[:r.left+1]
	}
	n, err := r.r.Read(p)
	r.left -= int64(n)
	r.read += int64(n)
	if r.left < 0 {
		return n, errors.New("archive exceeds uncompressed size limit")
	}
	return n, err
}

// contextWriter prevents a buffered codec from publishing more output after cancellation.
type contextWriter struct {
	ctx context.Context
	w   io.Writer
}

func (w *contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.w.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}
