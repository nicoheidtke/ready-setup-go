package cache

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ready-setup-go/internal/actionlog"
)

func TestCacheSummaryPartialRestoreAndSeparatePostSave(t *testing.T) {
	old := actionlog.Default
	actionlog.Default = actionlog.New(io.Discard, true)
	defer func() { actionlog.Default = old }()
	main := filepath.Join(t.TempDir(), "main-summary")
	post := filepath.Join(t.TempDir(), "post-summary")
	for _, path := range []string{main, post} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	c := DefaultConfig()
	c.Mode = "restore-save"
	t.Setenv("GITHUB_STEP_SUMMARY", main)
	summaryReport(c, Report{Phase: "restore", Snapshots: []Metric{{Kind: "modules", Hit: true, MatchType: "dependency-compatible", Bytes: 1024}, {Kind: "build", RestoreReason: "missing-chunk"}}}, "Post-job save requested.")
	b, _ := os.ReadFile(main)
	if !strings.Contains(string(b), "Restored 1/2 snapshots") || !strings.Contains(string(b), "compatible dependency snapshot") || !strings.Contains(string(b), "not additive") {
		t.Fatal(string(b))
	}
	t.Setenv("GITHUB_STEP_SUMMARY", post)
	summaryReport(c, Report{Phase: "save", Snapshots: []Metric{{Kind: "modules", Skipped: true, SaveReason: "unchanged-module-content"}, {Kind: "build", Error: "upload failed"}}}, "")
	p, _ := os.ReadFile(post)
	if !strings.Contains(string(p), "Go cache save") || !strings.Contains(string(p), "module content unchanged") || !strings.Contains(string(p), "ERROR") {
		t.Fatal(string(p))
	}
	if after, _ := os.ReadFile(main); string(after) != string(b) {
		t.Fatal("post changed main summary")
	}
}

func TestSummaryOptOutPersistsToPost(t *testing.T) {
	old := actionlog.Default
	actionlog.Default = actionlog.New(io.Discard, true)
	defer func() { actionlog.Default = old }()
	for _, disabled := range []bool{false, true} {
		c := DefaultConfig()
		c.Mode, c.DisableSummary = "restore-save", disabled
		path := filepath.Join(t.TempDir(), "post-summary")
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GITHUB_STEP_SUMMARY", path)
		t.Setenv("RUNNER_TEMP", t.TempDir())
		t.Setenv("ACTIONS_CACHE_MODE", "read")
		statePath, err := persist(state(t, c, "deps", roots(t)))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("STATE_cache_state", statePath)
		if err = Post(context.Background()); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if (len(b) == 0) != disabled {
			t.Fatalf("post summary disable=%v: %s", disabled, b)
		}
		if !disabled && !strings.Contains(string(b), "denies writes") {
			t.Fatal(string(b))
		}
	}
}
