package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"setup-go-better/internal/actionenv"
	"setup-go-better/internal/cache"
	"setup-go-better/internal/installer"
	"strconv"
	"time"
)

func run() error {
	if len(os.Args) >= 2 && os.Args[1] == "post" {
		if len(os.Args) != 2 {
			return fmt.Errorf("post does not accept arguments")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		return cache.Post(ctx)
	}
	if len(os.Args) < 2 || os.Args[1] != "setup" {
		return fmt.Errorf("usage: setup-go-better setup (--go-version VERSION | --go-version-file FILE) [--check-latest] [--json FILE|-]")
	}
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	v := fs.String("go-version", os.Getenv("INPUT_GO-VERSION"), "exact version or minor selector")
	versionFile := fs.String("go-version-file", os.Getenv("INPUT_GO-VERSION-FILE"), "go.mod or go.work path; explicit go-version takes precedence")
	latest := fs.Bool("check-latest", false, "resolve latest official patch")
	report := fs.String("json", "", "write result and phase timings to file, or - for stdout")
	c := cache.DefaultConfig()
	input := func(name, fallback string) string {
		if s := os.Getenv("INPUT_" + name); s != "" {
			return s
		}
		return fallback
	}
	fs.StringVar(&c.Mode, "cache", input("CACHE", c.Mode), "off, restore, or restore-save")
	fs.StringVar(&c.Layout, "cache-layout", input("CACHE-LAYOUT", c.Layout), "split or combined")
	fs.StringVar(&c.Storage, "cache-storage", input("CACHE-STORAGE", c.Storage), "archive or chunked")
	overlap := fs.String("cache-overlap", input("CACHE-OVERLAP", "true"), "overlap toolchain installation with cache prefetch")
	fs.StringVar(&c.Compression, "cache-compression", input("CACHE-COMPRESSION", c.Compression), "gzip or none")
	fs.StringVar(&c.Scope, "cache-scope", input("CACHE-SCOPE", ""), "explicit matrix/variant isolation scope")
	fs.StringVar(&c.DependencyPaths, "cache-dependency-path", input("CACHE-DEPENDENCY-PATH", ""), "newline-separated workspace-relative dependency roots/files")
	fs.StringVar(&c.Report, "cache-report", input("CACHE-REPORT", ""), "restore JSON report path; post report adds .post.json")
	// Register booleans/durations as strings so invalid action inputs fail visibly.
	strict := fs.String("cache-strict", input("CACHE-STRICT", "false"), "fail on cache errors")
	saveFailure := fs.String("cache-save-on-failure", input("CACHE-SAVE-ON-FAILURE", "false"), "save failed (never cancelled) jobs")
	maxAge := fs.String("cache-build-max-age", input("CACHE-BUILD-MAX-AGE", "0"), "optional archive-only age filter, e.g. 120h; 0 disables")
	maxBytes := fs.String("cache-max-bytes", input("CACHE-MAX-BYTES", fmt.Sprint(c.MaxBytes)), "maximum archive payload bytes")
	chunkWorkers := fs.String("cache-chunk-concurrency", input("CACHE-CHUNK-CONCURRENCY", fmt.Sprint(c.ChunkConcurrency)), "parallel chunk workers, 1..16")
	workers := fs.String("cache-upload-concurrency", input("CACHE-UPLOAD-CONCURRENCY", fmt.Sprint(c.UploadConcurrency)), "parallel upload workers, 1..32")
	chunk := fs.String("cache-upload-chunk-bytes", input("CACHE-UPLOAD-CHUNK-BYTES", fmt.Sprint(c.UploadChunkBytes)), "upload block size bytes")
	downloadWorkers := fs.String("cache-download-concurrency", input("CACHE-DOWNLOAD-CONCURRENCY", fmt.Sprint(c.DownloadConcurrency)), "parallel archive download workers, 1..32; 1 uses a single GET")
	downloadChunk := fs.String("cache-download-chunk-bytes", input("CACHE-DOWNLOAD-CHUNK-BYTES", fmt.Sprint(c.DownloadChunkBytes)), "archive download range size bytes")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	var err error
	if c.Overlap, err = strconv.ParseBool(*overlap); err != nil {
		return fmt.Errorf("cache-overlap: %w", err)
	}
	if c.Strict, err = strconv.ParseBool(*strict); err != nil {
		return fmt.Errorf("cache-strict: %w", err)
	}
	if c.SaveOnFailure, err = strconv.ParseBool(*saveFailure); err != nil {
		return fmt.Errorf("cache-save-on-failure: %w", err)
	}
	if c.BuildMaxAge, err = time.ParseDuration(*maxAge); err != nil {
		return fmt.Errorf("cache-build-max-age: %w", err)
	}
	if c.MaxBytes, err = strconv.ParseInt(*maxBytes, 10, 64); err != nil {
		return fmt.Errorf("cache-max-bytes: %w", err)
	}
	if c.ChunkConcurrency, err = strconv.Atoi(*chunkWorkers); err != nil {
		return fmt.Errorf("cache-chunk-concurrency: %w", err)
	}
	if c.UploadConcurrency, err = strconv.Atoi(*workers); err != nil {
		return fmt.Errorf("cache-upload-concurrency: %w", err)
	}
	if c.UploadChunkBytes, err = strconv.ParseInt(*chunk, 10, 64); err != nil {
		return fmt.Errorf("cache-upload-chunk-bytes: %w", err)
	}
	if c.DownloadConcurrency, err = strconv.Atoi(*downloadWorkers); err != nil {
		return fmt.Errorf("cache-download-concurrency: %w", err)
	}
	if c.DownloadChunkBytes, err = strconv.ParseInt(*downloadChunk, 10, 64); err != nil {
		return fmt.Errorf("cache-download-chunk-bytes: %w", err)
	}
	if err = c.Validate(); err != nil {
		return err
	}
	workspace := ""
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		workspace = os.Getenv("GITHUB_WORKSPACE")
	}
	version, err := installer.ResolveVersionInput(*v, *versionFile, workspace)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var pending *cache.Prefetch
	defer func() {
		if pending != nil {
			pending.Close()
		}
	}()
	r, err := installer.DefaultConfig().SetupResolved(ctx, version, *latest, func(selected string) { pending = cache.BeginPrefetch(ctx, c, selected) })
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
	if c.Mode == "restore-save" {
		if err = actionenv.Environment("SETUP_GO_BETTER_SAVE_ON_FAILURE_"+os.Getenv("GITHUB_ACTION"), fmt.Sprint(c.SaveOnFailure)); err != nil {
			return err
		}
	}
	return cache.StartPrefetched(ctx, c, r.Version, r.Root, pending)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "setup-go-better:", err)
		os.Exit(1)
	}
}
