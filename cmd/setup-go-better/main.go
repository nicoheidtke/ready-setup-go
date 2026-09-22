package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"setup-go-better/internal/actionenv"
	"setup-go-better/internal/installer"
	"time"
)

func run() error {
	if len(os.Args) < 2 || os.Args[1] != "setup" {
		return fmt.Errorf("usage: setup-go-better setup --go-version VERSION [--check-latest] [--json FILE|-]")
	}
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	v := fs.String("go-version", os.Getenv("INPUT_GO-VERSION"), "exact version or minor selector")
	latest := fs.Bool("check-latest", false, "resolve latest official patch")
	report := fs.String("json", "", "write result and phase timings to file, or - for stdout")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *v == "" {
		return fmt.Errorf("go-version is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	r, err := installer.DefaultConfig().Setup(ctx, *v, *latest)
	if err != nil {
		return err
	}
	if err = actionenv.Export(r.Version, r.Root); err != nil {
		return err
	}
	if *report != "" {
		b, e := json.MarshalIndent(r, "", "  ")
		if e != nil {
			return e
		}
		b = append(b, '\n')
		if *report == "-" {
			_, err = os.Stdout.Write(b)
		} else {
			err = os.WriteFile(*report, b, 0644)
		}
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "Go %s: %s (%s, %.1f ms)\n", r.Version, r.Root, r.Source, r.Metrics.TotalMS)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "setup-go-better:", err)
		os.Exit(1)
	}
}
