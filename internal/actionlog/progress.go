package actionlog

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Progress emits at most one line every two seconds as work advances. It owns
// no timer or goroutine; a blocked operation cannot fabricate activity. Nil
// progress is the allocation-free, silent path outside GitHub Actions.
type Progress struct {
	mu              sync.Mutex
	log             *Logger
	label, unit     string
	count, total    int64
	last            time.Time
	now             func() time.Time
	emitted, closed bool
}

func (l *Logger) Progress(label, unit string, total int64) *Progress {
	if !l.actions {
		return nil
	}
	return &Progress{log: l, label: progressText(label), unit: unit, total: total, last: time.Now(), now: time.Now}
}

func progressText(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}

func (p *Progress) Add(n int64) {
	if p == nil || n <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.count += n
	if now := p.now(); now.Sub(p.last) >= 2*time.Second {
		p.last, p.emitted = now, true
		p.print("in progress")
	}
}

func (p *Progress) setTotal(total int64) {
	if p == nil || total <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.total == 0 {
		p.total = total
	}
}

func (p *Progress) print(state string) {
	amount := fmt.Sprintf("%d %s", p.count, p.unit)
	if p.unit == "bytes" {
		amount = Bytes(p.count)
	}
	// Publication may retain existing local files. Its total would describe
	// examined jobs, not published files, so callers leave that total unknown.
	if p.total > 0 && p.count <= p.total {
		total := fmt.Sprintf("%d %s", p.total, p.unit)
		if p.unit == "bytes" {
			total = Bytes(p.total)
		}
		amount += " / " + total
		if state == "in progress" {
			amount += fmt.Sprintf(" (%.0f%%)", min(99.0, float64(p.count)*100/float64(p.total)))
		}
	}
	p.log.Printf("  Progress: %s · %s · %s\n", p.label, amount, state)
}

// Finish is idempotent. Only operations slow enough to emit progress get a
// closing line; errors never print a fabricated 100%% or a success message.
func (p *Progress) Finish(err error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.emitted {
		state := "finished"
		if err != nil {
			state = "stopped"
		}
		p.print(state)
	}
}

type transferKey struct{}
type transferProgress struct {
	p             *Progress
	discoverTotal bool
}

// Transfer shares one counter across a snapshot's concurrent ranges or chunks.
// Chunked snapshots have no known transfer total while deduplication is running.
func (l *Logger) Transfer(ctx context.Context, label string, total int64, discoverTotal bool) (context.Context, func(error)) {
	p := l.Progress(label, "bytes", total)
	if p == nil {
		return ctx, func(error) {}
	}
	return context.WithValue(ctx, transferKey{}, transferProgress{p, discoverTotal}), p.Finish
}

func Transferred(ctx context.Context, n int64) {
	if p, ok := ctx.Value(transferKey{}).(transferProgress); ok {
		p.p.Add(n)
	}
}

func TransferTotal(ctx context.Context, total int64) {
	if p, ok := ctx.Value(transferKey{}).(transferProgress); ok && p.discoverTotal {
		p.p.setTotal(total)
	}
}

type progressReader struct {
	ctx context.Context
	r   io.Reader
}

func (r progressReader) Read(b []byte) (int, error) {
	n, err := r.r.Read(b)
	Transferred(r.ctx, int64(n))
	return n, err
}

// TransferReader counts actual bytes consumed without changing the destination's
// concrete type (parallel downloads depend on retaining *os.File).
func TransferReader(ctx context.Context, r io.Reader) io.Reader {
	if ctx.Value(transferKey{}) == nil {
		return r
	}
	return progressReader{ctx, r}
}
