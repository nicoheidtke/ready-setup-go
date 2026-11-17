package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRaceChecksAreOptInAndFailuresPropagate(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("module fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		var args []string
		if enabled {
			args = []string{"--race"}
		}
		failure := errors.New("race fixture failure")
		seen := false
		var out bytes.Buffer
		err := run(context.Background(), args, &out, func(_ context.Context, step check, _ io.Writer) error {
			if slices.Contains(step.args, "-race") {
				seen = true
				if !slices.Contains(step.args, "-count=1") || !slices.Contains(step.args, "-timeout=10m") || !slices.Contains(step.args, "./internal/cachearchive") {
					t.Fatalf("race execution lacks its bounds or runtime coverage: %v", step.args)
				}
				return failure
			}
			return nil
		})
		if seen != enabled || enabled && (!errors.Is(err, failure) || strings.Contains(out.String(), "All checks passed")) || !enabled && err != nil {
			t.Fatalf("enabled=%v seen=%v err=%v output=%s", enabled, seen, err, &out)
		}
	}
}

func TestRunStopsAtFailedCheck(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("module fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("fixture failure")
	var ran []string
	var output bytes.Buffer
	err := run(context.Background(), nil, &output, func(_ context.Context, step check, _ io.Writer) error {
		ran = append(ran, step.label)
		if step.label == "documentation" {
			return failure
		}
		return nil
	})
	if !errors.Is(err, failure) || len(ran) != 2 || ran[1] != "documentation" {
		t.Fatalf("failure was not propagated immediately: ran=%v err=%v", ran, err)
	}
	if strings.Contains(output.String(), "All checks passed") {
		t.Fatal("failure reported success")
	}
}

func TestHelpDoesNotNeedRepositoryOrTools(t *testing.T) {
	t.Chdir(t.TempDir())
	var output bytes.Buffer
	err := run(context.Background(), []string{"--help"}, &output, func(context.Context, check, io.Writer) error {
		t.Fatal("help executed a check")
		return nil
	})
	if err != nil || !strings.Contains(output.String(), "--workflows") {
		t.Fatalf("help failed: %v, %s", err, &output)
	}
}

func TestRunCancellationDoesNotStartChecks(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("module fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := run(ctx, nil, io.Discard, func(context.Context, check, io.Writer) error {
		t.Fatal("started a check after cancellation")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestFormattingCheckDoesNotRewriteSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unformatted.go")
	original := []byte("package fixture\nfunc f( ){}\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := execute(context.Background(), check{"format", "gofmt", []string{"-l", path}, true}, &output)
	if err == nil || !strings.Contains(output.String(), path) {
		t.Fatalf("format drift not reported: %v, %s", err, &output)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(after, original) {
		t.Fatalf("source changed: %q, %v", after, readErr)
	}
}

func TestMissingExecutableExplainsPrerequisite(t *testing.T) {
	err := execute(context.Background(), check{"test", "ready-setup-go-missing-check-tool", nil, false}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "CONTRIBUTING.md") {
		t.Fatalf("missing-tool error is not actionable: %v", err)
	}
}
