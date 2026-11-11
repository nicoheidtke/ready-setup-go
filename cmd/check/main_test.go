package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	err := execute(context.Background(), check{"test", "setup-go-better-missing-check-tool", nil, false}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "CONTRIBUTING.md") {
		t.Fatalf("missing-tool error is not actionable: %v", err)
	}
}
