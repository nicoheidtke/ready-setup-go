// check runs the repository's portable contributor checks without changing source.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
)

type check struct {
	label   string
	program string
	args    []string
	format  bool
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, execute); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer, execCheck func(context.Context, check, io.Writer) error) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(out)
	node := fs.String("node", "node", "Node 24 executable (name or path)")
	workflows := fs.Bool("workflows", false, "also run pinned actionlint; may download the tool and dependencies")
	fs.Usage = func() {
		fmt.Fprintln(out, "Usage: go run ./cmd/check [--node PATH] [--workflows]\n\nRun from the repository root. Checks formatting, documentation, Go tests/vet,\nand Node tests without rewriting source. Does not execute hosted workflows.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(*node) == "" {
		return fmt.Errorf("--node must name a Node 24 executable")
	}
	if _, err := os.Stat("go.mod"); err != nil {
		return fmt.Errorf("run checks from the repository root: %w", err)
	}
	checks := []check{
		{"Go formatting", "gofmt", []string{"-l", "cmd", "internal", "testdata/smoke", "testdata/packages"}, true},
		{"documentation", "go", []string{"run", "./cmd/check-docs"}, false},
		{"Go tests", "go", []string{"test", "./..."}, false},
		{"Go vet", "go", []string{"vet", "./..."}, false},
		{"Node tests", *node, []string{"--test", "launcher.test.cjs", ".github/actions/cache-benchmark/run.test.cjs", ".github/actions/node-runtime/locate.test.cjs", ".github/scripts/cache-probe.test.cjs"}, false},
	}
	if *workflows {
		checks = append(checks, check{"workflow syntax", "go", []string{"run", "github.com/rhysd/actionlint/cmd/actionlint@v1.7.12", "-shellcheck=", "-pyflakes="}, false})
	}
	for _, step := range checks {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprintf(out, "Checking %s...\n", step.label)
		if err := execCheck(ctx, step, out); err != nil {
			return fmt.Errorf("%s failed: %w", step.label, err)
		}
	}
	fmt.Fprintln(out, "All checks passed.")
	return nil
}

func execute(ctx context.Context, step check, out io.Writer) error {
	cmd := exec.CommandContext(ctx, step.program, step.args...)
	cmd.Stderr = out
	var formatted bytes.Buffer
	if step.format {
		cmd.Stdout = &formatted
	} else {
		cmd.Stdout = out
	}
	if err := cmd.Run(); err != nil {
		var missing *exec.Error
		if errors.As(err, &missing) && errors.Is(missing.Err, exec.ErrNotFound) {
			return fmt.Errorf("required executable %q was not found; see CONTRIBUTING.md or use --node PATH for Node: %w", step.program, err)
		}
		return err
	}
	if step.format && formatted.Len() != 0 {
		fmt.Fprint(out, formatted.String())
		return fmt.Errorf("run gofmt on the files listed above")
	}
	return nil
}
