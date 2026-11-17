package main

import (
	"bytes"
	"flag"
	"fmt"
	"strconv"
	"time"

	"ready-setup-go/internal/cache"
)

type setupOptions struct {
	Version, VersionFile string
	CheckLatest          bool
	Report               string
	Cache                cache.Config
}

// parseSetupOptions keeps input validation separate from installation and runner
// protocols. Explicit flags override nonempty action inputs. check-latest keeps
// its existing CLI default: the Node launcher translates that action input into
// a flag, just as it translates the standalone version fallback variables.
func parseSetupOptions(args []string, getenv func(string) string) (setupOptions, string, error) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	// Capture untrusted flag diagnostics; only the caller may render them through
	// the safe action logger. Parsing itself performs no I/O or environment writes.
	var flagOutput bytes.Buffer
	fs.SetOutput(&flagOutput)
	v := fs.String("go-version", getenv("INPUT_GO-VERSION"), "exact version or minor selector")
	versionFile := fs.String("go-version-file", getenv("INPUT_GO-VERSION-FILE"), "go.mod or go.work path; explicit go-version takes precedence")
	latest := fs.Bool("check-latest", false, "resolve latest official patch")
	report := fs.String("json", "", "write result and phase timings to file, or - for stdout")
	c := cache.DefaultConfig()
	input := func(name, fallback string) string {
		if s := getenv("INPUT_" + name); s != "" {
			return s
		}
		return fallback
	}
	fs.StringVar(&c.Mode, "cache", input("CACHE", c.Mode), "off, restore, or restore-save")
	fs.StringVar(&c.Layout, "cache-layout", input("CACHE-LAYOUT", c.Layout), "split or combined")
	fs.StringVar(&c.Storage, "cache-storage", input("CACHE-STORAGE", c.Storage), "archive or chunked")
	overlap := fs.String("cache-overlap", input("CACHE-OVERLAP", strconv.FormatBool(c.Overlap)), "overlap toolchain installation with cache prefetch")
	fs.StringVar(&c.Compression, "cache-compression", input("CACHE-COMPRESSION", c.Compression), "gzip, zstd, or none")
	fs.StringVar(&c.ModuleScope, "cache-module-scope", input("CACHE-MODULE-SCOPE", c.ModuleScope), "opt-in repository module sharing group; split layout only")
	gzipLevel := fs.String("cache-gzip-level", input("CACHE-GZIP-LEVEL", "default"), "gzip compression level: default or 1..9")
	fs.StringVar(&c.Scope, "cache-scope", input("CACHE-SCOPE", c.Scope), "explicit matrix/variant isolation scope")
	fs.StringVar(&c.DependencyPaths, "cache-dependency-path", input("CACHE-DEPENDENCY-PATH", c.DependencyPaths), "newline-separated workspace-relative dependency roots/files")
	fs.StringVar(&c.Report, "cache-report", input("CACHE-REPORT", c.Report), "restore JSON report path; post report adds .post.json")
	// Register booleans/durations as strings so invalid action inputs fail visibly.
	strict := fs.String("cache-strict", input("CACHE-STRICT", strconv.FormatBool(c.Strict)), "fail on cache errors")
	summary := fs.String("summary", input("SUMMARY", strconv.FormatBool(!c.DisableSummary)), "write a concise GitHub job summary")
	buildSave := fs.String("cache-build-save", input("CACHE-BUILD-SAVE", strconv.FormatBool(!c.DisableBuildSave)), "save build snapshot in split layout; use a workflow expression to control frequency")
	saveFailure := fs.String("cache-save-on-failure", input("CACHE-SAVE-ON-FAILURE", strconv.FormatBool(c.SaveOnFailure)), "save failed (never cancelled) jobs")
	maxAge := fs.String("cache-build-max-age", input("CACHE-BUILD-MAX-AGE", "0"), "optional snapshot-only age filter, e.g. 120h; 0 disables")
	maxBytes := fs.String("cache-max-bytes", input("CACHE-MAX-BYTES", fmt.Sprint(c.MaxBytes)), "maximum archive payload bytes")
	chunkWorkers := fs.String("cache-chunk-concurrency", input("CACHE-CHUNK-CONCURRENCY", fmt.Sprint(c.ChunkConcurrency)), "parallel chunk workers, 1..16")
	compressionWorkers := fs.String("cache-compression-concurrency", input("CACHE-COMPRESSION-CONCURRENCY", fmt.Sprint(c.CompressionConcurrency)), "whole-archive compression workers, 1..16")
	restoreWorkers := fs.String("cache-restore-concurrency", input("CACHE-RESTORE-CONCURRENCY", fmt.Sprint(c.RestoreConcurrency)), "restored-file publication workers, 1..16")
	workers := fs.String("cache-upload-concurrency", input("CACHE-UPLOAD-CONCURRENCY", fmt.Sprint(c.UploadConcurrency)), "parallel upload workers, 1..32")
	chunk := fs.String("cache-upload-chunk-bytes", input("CACHE-UPLOAD-CHUNK-BYTES", fmt.Sprint(c.UploadChunkBytes)), "upload block size bytes")
	downloadWorkers := fs.String("cache-download-concurrency", input("CACHE-DOWNLOAD-CONCURRENCY", fmt.Sprint(c.DownloadConcurrency)), "parallel archive download workers, 1..32; 1 uses a single GET")
	downloadChunk := fs.String("cache-download-chunk-bytes", input("CACHE-DOWNLOAD-CHUNK-BYTES", fmt.Sprint(c.DownloadChunkBytes)), "archive download range size bytes")
	if err := fs.Parse(args); err != nil {
		return setupOptions{}, flagOutput.String(), err
	}
	if fs.NArg() != 0 {
		return setupOptions{}, "", fmt.Errorf("unexpected positional arguments")
	}
	var err error
	if *gzipLevel != "default" {
		if c.GzipLevel, err = strconv.Atoi(*gzipLevel); err != nil || c.GzipLevel < 1 || c.GzipLevel > 9 {
			return setupOptions{}, "", fmt.Errorf("cache-gzip-level must be default or 1..9")
		}
	}
	showSummary, err := strconv.ParseBool(*summary)
	if err != nil {
		return setupOptions{}, "", fmt.Errorf("summary: %w", err)
	}
	c.DisableSummary = !showSummary
	saveBuild, err := strconv.ParseBool(*buildSave)
	if err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-build-save: %w", err)
	}
	c.DisableBuildSave = !saveBuild
	if c.Overlap, err = strconv.ParseBool(*overlap); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-overlap: %w", err)
	}
	if c.Strict, err = strconv.ParseBool(*strict); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-strict: %w", err)
	}
	if c.SaveOnFailure, err = strconv.ParseBool(*saveFailure); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-save-on-failure: %w", err)
	}
	if c.BuildMaxAge, err = time.ParseDuration(*maxAge); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-build-max-age: %w", err)
	}
	if c.MaxBytes, err = strconv.ParseInt(*maxBytes, 10, 64); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-max-bytes: %w", err)
	}
	if c.ChunkConcurrency, err = strconv.Atoi(*chunkWorkers); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-chunk-concurrency: %w", err)
	}
	if c.CompressionConcurrency, err = strconv.Atoi(*compressionWorkers); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-compression-concurrency: %w", err)
	}
	if c.RestoreConcurrency, err = strconv.Atoi(*restoreWorkers); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-restore-concurrency: %w", err)
	}
	if c.UploadConcurrency, err = strconv.Atoi(*workers); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-upload-concurrency: %w", err)
	}
	if c.UploadChunkBytes, err = strconv.ParseInt(*chunk, 10, 64); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-upload-chunk-bytes: %w", err)
	}
	if c.DownloadConcurrency, err = strconv.Atoi(*downloadWorkers); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-download-concurrency: %w", err)
	}
	if c.DownloadChunkBytes, err = strconv.ParseInt(*downloadChunk, 10, 64); err != nil {
		return setupOptions{}, "", fmt.Errorf("cache-download-chunk-bytes: %w", err)
	}
	if err = c.Validate(); err != nil {
		return setupOptions{}, "", err
	}
	return setupOptions{Version: *v, VersionFile: *versionFile, CheckLatest: *latest, Report: *report, Cache: c}, "", nil
}
