package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setup-go-faster/internal/actionlog"
	"setup-go-faster/internal/cachebackend"
)

func TestConciseReportExplainsPartialRestoreAndLogsKeys(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	logReport(actionlog.New(&out, true), Report{Phase: "restore", TotalMS: 1234, Snapshots: []Metric{
		{Kind: "modules", Key: "private-full-key", MatchedKey: "private-matched-key", Hit: true, MatchType: "dependency-compatible", Bytes: 2 << 20, TransferMS: 1000},
		{Kind: "build", RestoreReason: "missing-chunk", Skipped: true},
	}})
	got := out.String()
	for _, want := range []string{"modules HIT", "compatible dependency snapshot", "2.0 MiB", "build MISS", "chunk missing or evicted", "partial · 1/2 snapshots restored", "1.23 s", "Lookup key: private-full-key", "Matched key: private-matched-key"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "\"phase\"") {
		t.Fatalf("verbose report leaked into ordinary logging: %q", got)
	}
}

func TestReportKeysForMissSaveSkipAndError(t *testing.T) {
	for _, tc := range []struct {
		name, phase, label string
		metric             Metric
	}{
		{"miss", "restore", "Lookup key", Metric{Kind: "build", Key: "candidate", RestoreReason: "no-match"}},
		{"save", "save", "Save key", Metric{Kind: "build", Key: "candidate"}},
		{"skip", "save", "Save key", Metric{Kind: "build", Key: "candidate", Skipped: true, SaveReason: "build-save-disabled"}},
		{"error", "restore", "Lookup key", Metric{Kind: "build", Key: "candidate", MatchedKey: "matched", Error: "invalid archive"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			logReport(actionlog.New(&out, false), Report{Phase: tc.phase, Snapshots: []Metric{tc.metric}})
			got := out.String()
			if !strings.Contains(got, tc.label+": candidate") || strings.Contains(got, "Matched key:") != (tc.metric.MatchedKey != "") {
				t.Fatal(got)
			}
		})
	}
}

func TestReportKeysSanitizeControlsAndRedactCredentials(t *testing.T) {
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "fixture-runtime-secret")
	var out bytes.Buffer
	logReport(actionlog.New(&out, false), Report{Phase: "restore", Snapshots: []Metric{{
		Kind: "build", Key: "candidate\n::error::injected\r\x1b", MatchedKey: "fixture-runtime-secret-https://example.test/signed?secret=value",
	}}})
	got := out.String()
	if !strings.Contains(got, "Lookup key:") || !strings.Contains(got, "Matched key:") {
		t.Fatal(got)
	}
	if strings.Contains(got, "\n::error::") || strings.ContainsAny(got, "\r\x1b") || strings.Contains(got, "fixture-runtime-secret") || strings.Contains(got, "example.test") || strings.Contains(got, "secret=value") {
		t.Fatalf("unsafe key presentation: %q", got)
	}
}

func TestConciseReportSkipErrorAndCommandSafety(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out bytes.Buffer
	logReport(actionlog.New(&out, true), Report{Phase: "save", Snapshots: []Metric{
		{Kind: "modules", Skipped: true, SaveReason: "unchanged-module-content"},
		{Kind: "build", Error: "failure\n::error::injected\r\x1b[31m"},
	}})
	got := out.String()
	if !strings.Contains(got, "modules SKIP · module content unchanged") || !strings.Contains(got, "build ERROR") {
		t.Fatal(got)
	}
	if strings.Contains(got, "\n::error::") || strings.ContainsAny(got, "\r\x1b") || strings.Count(got, "\n") != 3 {
		t.Fatalf("untrusted text escaped a log line: %q", got)
	}
}

func TestReportFileKeepsJSONDetails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	want := Report{Phase: "restore", Snapshots: []Metric{{Kind: "modules", Key: "full-key", MatchType: "dependency-exact", Hit: true}}}
	if err := writeReport(path, want); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Report
	if err = json.Unmarshal(b, &got); err != nil || got.Snapshots[0].Key != "full-key" || got.Snapshots[0].MatchType != "dependency-exact" {
		t.Fatalf("JSON report lost details: %s, %v", b, err)
	}
}

func TestPrefetchReportsDependencyMatchAndAccessDenial(t *testing.T) {
	c := DefaultConfig()
	c.Mode = "restore"
	s := state(t, c, "dependencies", roots(t))
	for _, tc := range []struct {
		name, key, want string
		denied          bool
	}{
		{"exact", s.Snapshots[0].Prefix + "old", "dependency-exact", false},
		{"compatible", s.Snapshots[0].CompatiblePrefix + "other-old", "dependency-compatible", false},
		{"denied", "", "", true},
		{"miss", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &memoryBackend{}
			if tc.denied {
				b.err = cachebackend.ErrPermission
			} else if tc.key != "" {
				b.entries = []memoryEntry{{key: tc.key, version: s.Snapshots[0].Version, data: []byte("archive not merged by this test")}}
			}
			p := &Prefetch{state: s, backend: b}
			p.download(context.Background())
			defer p.Close()
			m := p.report.Snapshots[0]
			if m.MatchType != tc.want {
				t.Fatalf("match type %q, want %q", m.MatchType, tc.want)
			}
			if tc.denied && m.RestoreReason != "access-denied" || tc.name == "miss" && m.RestoreReason != "no-match" {
				t.Fatalf("missing decision: %+v", m)
			}
		})
	}
}
