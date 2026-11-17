package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"ready-setup-go/internal/actionenv"
	"ready-setup-go/internal/actionlog"
	"ready-setup-go/internal/cache"
	"ready-setup-go/internal/installer"
	"strings"
	"time"
)

func run() (runErr error) {
	if len(os.Args) >= 2 && os.Args[1] == "post" {
		if len(os.Args) != 2 {
			return fmt.Errorf("post does not accept arguments")
		}
		ctx, stop := signal.NotifyContext(context.Background(), terminationSignals...)
		defer stop()
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		return cache.Post(ctx)
	}
	if len(os.Args) < 2 || os.Args[1] != "setup" {
		return fmt.Errorf("usage: ready-setup-go setup (--go-version VERSION | --go-version-file FILE) [--check-latest] [--json FILE|-]")
	}
	options, help, err := parseSetupOptions(os.Args[2:], os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		for _, line := range strings.Split(strings.TrimSuffix(help, "\n"), "\n") {
			actionlog.Default.Printf("%s\n", line)
		}
		return nil
	}
	if err != nil {
		return err
	}
	c := options.Cache
	summaryWritten := false
	defer func() {
		if !summaryWritten && runErr != nil {
			actionlog.Default.Summary(c.DisableSummary, "Go setup", []string{"Result", "Reason"}, []actionlog.SummaryRow{{"ERROR", runErr.Error()}})
		}
	}()
	workspace := ""
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		workspace = os.Getenv("GITHUB_WORKSPACE")
	}
	endResolution := actionlog.Default.Stage("Resolve Go version")
	defer endResolution()
	version, err := installer.ResolveVersionInput(options.Version, options.VersionFile, workspace)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals...)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var pending *cache.Prefetch
	defer func() {
		if pending != nil {
			pending.Close()
		}
	}()
	endToolchain := func() {}
	defer func() { endToolchain() }()
	r, err := installer.DefaultConfig().SetupResolved(ctx, version, options.CheckLatest, func(selected string) {
		endResolution()
		title := "Prepare Go toolchain"
		if c.Mode != "off" && c.Overlap {
			title += " (cache prefetch runs concurrently)"
		}
		endToolchain = actionlog.Default.Stage(title)
		pending = cache.BeginPrefetch(ctx, c, selected)
	})
	if err != nil {
		return err
	}
	if err = actionenv.Export(r.Version, r.Root); err != nil {
		return err
	}
	if options.Report != "" {
		b, e := json.MarshalIndent(r, "", "  ")
		if e != nil {
			return e
		}
		b = append(b, '\n')
		if options.Report == "-" {
			_, err = os.Stdout.Write(b)
		} else {
			err = os.WriteFile(options.Report, b, 0644)
		}
		if err != nil {
			return err
		}
	}
	source := map[string]string{"runner-tool-cache": "reused runner tool cache", "installed": "reused installed toolchain", "download": "downloaded and verified official toolchain"}[r.Source]
	if source == "" {
		source = "toolchain prepared"
	}
	actionlog.Default.Printf("Go %s %s · %s · %s\n", r.Version, actionlog.Default.Status("READY"), source, actionlog.Duration(r.Metrics.TotalMS))
	actionlog.Default.DebugJSON("toolchain result", r)
	actionlog.Default.Summary(c.DisableSummary, "Go toolchain", []string{"Go", "Result", "Source", "Setup work"}, []actionlog.SummaryRow{{r.Version, "READY", source, actionlog.Duration(r.Metrics.TotalMS)}}, "Toolchain work may overlap cache prefetch; phase durations are not additive.")
	summaryWritten = true
	endToolchain()
	if c.Mode == "restore-save" {
		if err = actionenv.Environment("READY_SETUP_GO_SAVE_ON_FAILURE_"+os.Getenv("GITHUB_ACTION"), fmt.Sprint(c.SaveOnFailure)); err != nil {
			return err
		}
	}
	return cache.StartPrefetched(ctx, c, r.Version, r.Root, pending)
}
func main() {
	if err := run(); err != nil {
		reportError(err)
		os.Exit(1)
	}
}
