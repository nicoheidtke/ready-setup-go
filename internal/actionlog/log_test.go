package actionlog

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestStageEscapesCommandsAndClosesOnce(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, true)
	end := l.Stage("resolve%\r\n::error::untrusted")
	l.Printf("selected Go %s\n", "1.27.1")
	end()
	end()
	want := "::group::resolve%25%0D%0A::error::untrusted\nselected Go 1.27.1\n::endgroup::\n"
	if out.String() != want {
		t.Fatalf("unescaped or unbalanced group: %q", out.String())
	}
}

func TestStandaloneDoesNotEmitWorkflowCommands(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, false)
	end := l.Stage("Prepare Go")
	l.Printf("ordinary diagnostic\n")
	end()
	if out.String() != "ordinary diagnostic\n" {
		t.Fatalf("unexpected standalone output: %q", out.String())
	}
}

func TestConcurrentDiagnosticsStayWhole(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, true)
	end := l.Stage("Prepare Go (cache prefetch runs concurrently)")
	var workers sync.WaitGroup
	for i := range 40 {
		workers.Go(func() { l.Printf("worker %02d: %s\n", i, strings.Repeat("x", 1000)) })
	}
	workers.Wait()
	end()
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 42 || lines[41] != "::endgroup::" {
		t.Fatalf("lost lines or unmatched stage: %d lines", len(lines))
	}
	seen := make(map[string]bool)
	for _, line := range lines[1:41] {
		seen[line] = true
	}
	for i := range 40 {
		if !seen[fmt.Sprintf("worker %02d: %s", i, strings.Repeat("x", 1000))] {
			t.Fatalf("worker %d log interleaved or lost", i)
		}
	}
}
