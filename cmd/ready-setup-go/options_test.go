package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"ready-setup-go/internal/cache"
)

// Read only the repository's flat action input/default subset. This keeps the
// contract tied to real metadata without introducing a runtime YAML dependency.
func actionInputDefaults(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	alias, err := os.ReadFile("../../experimental/node/action.yml")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(alias) {
		t.Fatal("Node alias metadata differs from root action")
	}
	inputs := map[string]string{}
	section, name := false, ""
	for _, line := range strings.Split(string(data), "\n") {
		if line == "inputs:" {
			section = true
			continue
		}
		if section && len(line) > 0 && line[0] != ' ' {
			break
		}
		if !section {
			continue
		}
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.HasSuffix(line, ":") {
			name = strings.TrimSuffix(strings.TrimSpace(line), ":")
			if _, exists := inputs[name]; exists {
				t.Fatalf("duplicate action input %s", name)
			}
			inputs[name] = ""
		}
		if strings.HasPrefix(line, "    default:") {
			inputs[name] = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "    default:")), "\"'")
		}
	}
	if len(inputs) == 0 {
		t.Fatal("no action inputs read")
	}
	return inputs
}

// Observe parsed behavior, including inverted public booleans, rather than
// comparing duplicated flag declaration strings.
func parsedInputs(o setupOptions) map[string]string {
	c := o.Cache
	age := c.BuildMaxAge.String()
	if c.BuildMaxAge == 0 {
		age = "0"
	}
	level := strconv.Itoa(c.GzipLevel)
	if c.GzipLevel == 0 {
		level = "default"
	}
	return map[string]string{
		"go-version": o.Version, "go-version-file": o.VersionFile, "check-latest": strconv.FormatBool(o.CheckLatest),
		"summary": strconv.FormatBool(!c.DisableSummary), "cache": c.Mode, "cache-scope": c.Scope,
		"cache-module-scope": c.ModuleScope, "cache-gzip-level": level, "cache-layout": c.Layout,
		"cache-storage": c.Storage, "cache-overlap": strconv.FormatBool(c.Overlap), "cache-compression": c.Compression,
		"cache-dependency-path": c.DependencyPaths, "cache-build-save": strconv.FormatBool(!c.DisableBuildSave),
		"cache-build-max-age": age, "cache-max-bytes": fmt.Sprint(c.MaxBytes), "cache-strict": strconv.FormatBool(c.Strict),
		"cache-chunk-concurrency": fmt.Sprint(c.ChunkConcurrency), "cache-compression-concurrency": fmt.Sprint(c.CompressionConcurrency),
		"cache-restore-concurrency": fmt.Sprint(c.RestoreConcurrency), "cache-upload-concurrency": fmt.Sprint(c.UploadConcurrency),
		"cache-download-concurrency": fmt.Sprint(c.DownloadConcurrency), "cache-upload-chunk-bytes": fmt.Sprint(c.UploadChunkBytes),
		"cache-download-chunk-bytes": fmt.Sprint(c.DownloadChunkBytes), "cache-save-on-failure": strconv.FormatBool(c.SaveOnFailure),
		"cache-report": c.Report,
	}
}

func TestSetupDefaultsMatchActionContract(t *testing.T) {
	empty := func(string) string { return "" }
	o, _, err := parseSetupOptions(nil, empty)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(o.Cache, cache.DefaultConfig()) {
		t.Fatalf("parser/cache defaults differ: %+v", o.Cache)
	}
	want := actionInputDefaults(t)
	if got := parsedInputs(o); !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed defaults = %#v; metadata = %#v", got, want)
	}
	// Catch a newly declared flag/input with no behavioral contract observation.
	_, help, err := parseSetupOptions([]string{"--help"}, empty)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^  -([a-z-]+)`).FindAllStringSubmatch(help, -1) {
		names[m[1]] = true
	}
	delete(names, "json") // standalone-only report, not an action input
	for name := range want {
		if !names[name] {
			t.Errorf("action input %s lacks a setup flag", name)
		}
		delete(names, name)
	}
	if len(names) != 0 {
		t.Fatalf("flags missing action contract coverage: %v", names)
	}
}

func TestSetupInputSourcesAndOverrides(t *testing.T) {
	values := map[string]string{
		"go-version": "1.26.8", "go-version-file": "nested module/go.mod", "check-latest": "true", "summary": "false",
		"cache": "restore-save", "cache-scope": "test variant", "cache-module-scope": "trusted modules", "cache-gzip-level": "1",
		"cache-layout": "combined", "cache-storage": "chunked", "cache-overlap": "false", "cache-compression": "zstd",
		"cache-dependency-path": "first/go.mod\nsecond/go.sum", "cache-build-save": "false", "cache-build-max-age": "2h0m0s",
		"cache-max-bytes": "12345", "cache-strict": "true", "cache-chunk-concurrency": "2", "cache-compression-concurrency": "3",
		"cache-restore-concurrency": "5", "cache-upload-concurrency": "6", "cache-download-concurrency": "7",
		"cache-upload-chunk-bytes": "1024", "cache-download-chunk-bytes": "2048", "cache-save-on-failure": "true", "cache-report": "report with spaces.json",
	}
	defaults := actionInputDefaults(t)
	if len(values) != len(defaults) {
		t.Fatal("every action input must have a nondefault source/override case")
	}
	for name, value := range values {
		t.Run(name, func(t *testing.T) {
			if _, ok := defaults[name]; !ok {
				t.Fatalf("unknown action input %s", name)
			}
			env := func(key string) string {
				if key == "INPUT_"+strings.ToUpper(name) {
					return value
				}
				return ""
			}
			o, _, err := parseSetupOptions(nil, env)
			if err != nil {
				t.Fatal(err)
			}
			if name == "check-latest" {
				// The launcher supplies this flag; native direct environment behavior is
				// intentionally unchanged. launcher.test.cjs covers the translation.
				if o.CheckLatest {
					t.Fatal("native unexpectedly consumed launcher-only environment input")
				}
			} else if got := parsedInputs(o)[name]; got != value {
				t.Fatalf("action input ignored: %q", got)
			}
			o, _, err = parseSetupOptions([]string{"--" + name + "=" + value}, func(string) string { return "" })
			if err != nil || parsedInputs(o)[name] != value {
				t.Fatalf("flag not parsed: %+v, %v", o, err)
			}
			o, _, err = parseSetupOptions([]string{"--" + name + "=" + defaults[name]}, env)
			if err != nil || parsedInputs(o)[name] != defaults[name] {
				t.Fatalf("flag did not override environment: %+v, %v", o, err)
			}
		})
	}
}

func TestSetupInvalidEnvironmentCanBeOverridden(t *testing.T) {
	for _, name := range []string{"summary", "cache-build-save", "cache-overlap", "cache-strict", "cache-save-on-failure", "cache-max-bytes", "cache-build-max-age", "cache-upload-concurrency", "cache-gzip-level"} {
		t.Run(name, func(t *testing.T) {
			env := func(key string) string {
				if key == "INPUT_"+strings.ToUpper(name) {
					return "invalid"
				}
				return ""
			}
			if _, _, err := parseSetupOptions(nil, env); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("invalid action input not rejected: %v", err)
			}
			if _, _, err := parseSetupOptions([]string{"--" + name + "=" + actionInputDefaults(t)[name]}, env); err != nil {
				t.Fatalf("valid explicit override rejected: %v", err)
			}
		})
	}
	o, _, err := parseSetupOptions([]string{"--json=report with spaces.json"}, func(string) string { return "" })
	if err != nil || o.Report != "report with spaces.json" {
		t.Fatalf("standalone report changed: %+v, %v", o, err)
	}
}
