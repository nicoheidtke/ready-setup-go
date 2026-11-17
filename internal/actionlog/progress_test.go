package actionlog

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProgressThrottlesAndFailureNeverClaimsCompletion(t *testing.T) {
	var out bytes.Buffer
	p := New(&out, true).Progress("Download\n::error::input", "bytes", 100)
	now := p.last
	p.now = func() time.Time { return now }
	p.Add(20)
	if out.Len() != 0 {
		t.Fatal("short operation logged progress")
	}
	now = now.Add(2 * time.Second)
	p.Add(80)
	p.Add(1)
	p.Finish(errors.New("cancelled"))
	p.Finish(nil)
	p.Add(10)
	got := out.String()
	if strings.Count(got, "\n") != 2 || !strings.Contains(got, "99%") || !strings.Contains(got, "101 B · stopped") || strings.Contains(got, "100%") || strings.Contains(got, "finished") || strings.Contains(got, "\n::error::") {
		t.Fatalf("dishonest or unthrottled output: %q", got)
	}
}

func TestTransferAggregatesConcurrentReadersAndIgnoresChunkTotals(t *testing.T) {
	var out bytes.Buffer
	ctx, finish := New(&out, true).Transfer(context.Background(), "Download chunks", 0, false)
	p := ctx.Value(transferKey{}).(transferProgress).p
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			TransferTotal(ctx, 100)
			_, _ = io.Copy(io.Discard, TransferReader(ctx, strings.NewReader("1234567")))
		})
	}
	workers.Wait()
	finish(nil)
	if p.count != 56 || p.total != 0 {
		t.Fatalf("incorrect aggregate %+v", p)
	}
	if out.Len() != 0 {
		t.Fatalf("short chunks spammed logs: %q", out.String())
	}
}

func TestTransferTotalAndSilentStandalone(t *testing.T) {
	var out bytes.Buffer
	log := New(&out, true)
	ctx, finish := log.Transfer(context.Background(), "Download archive", 0, true)
	p := ctx.Value(transferKey{}).(transferProgress).p
	TransferTotal(ctx, 100)
	TransferTotal(ctx, 20) // A later range's length is not the overall size.
	Transferred(ctx, 40)
	finish(context.Canceled)
	if p.total != 100 || p.count != 40 {
		t.Fatalf("wrong total/count: %+v", p)
	}
	plain := New(&out, false)
	base := context.Background()
	ctx, finish = plain.Transfer(base, "silent", 100, true)
	if ctx != base || plain.Progress("silent", "files", 0) != nil {
		t.Fatal("standalone progress allocated a scope")
	}
	Transferred(ctx, 100)
	finish(nil)
	if out.Len() != 0 {
		t.Fatal("standalone stdout/stderr polluted")
	}
}

func TestPublicationProgressHasNoInventedTotal(t *testing.T) {
	var out bytes.Buffer
	p := New(&out, true).Progress("Publish validated cache files", "files published", 0)
	p.now = func() time.Time { return p.last.Add(2 * time.Second) }
	p.Add(3)
	p.Finish(nil)
	if got := out.String(); strings.Contains(got, "%") || !strings.Contains(got, "3 files published") || !strings.Contains(got, "finished") {
		t.Fatal(got)
	}
}
