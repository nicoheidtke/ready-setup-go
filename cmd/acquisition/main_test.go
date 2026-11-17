package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func acquisitionArgs(out, locator string) []string {
	args := []string{"--out", out}
	for _, name := range []string{"bundle", "launcher", "binary"} {
		args = append(args, "--"+name+"-url", locator, "--"+name+"-sha256", strings.Repeat("a", 64))
	}
	return args
}

func TestFailureWritesPartialObservationsWithoutSecrets(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	var staging []string
	calls := 0
	err := runWith(acquisitionArgs(out, "https://example.test/public-asset"), func(_ context.Context, _ asset, dir string, _ int) (int64, int64, float64, float64, error) {
		staging = append(staging, dir)
		calls++
		if calls == 3 {
			return 7, 3, 2, 1, errors.New("https://user:private-token@example.test/?signature=secret")
		}
		return 10, 20, 4, 5, nil
	})
	if err == nil {
		t.Fatal("failed sample reported success")
	}
	b, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(b)+err.Error(), "private-token") || strings.Contains(string(b)+err.Error(), "signature") {
		t.Fatal("raw error leaked into diagnostics or report")
	}
	var report struct {
		Samples   []sample      `json:"samples"`
		Paired    pairedSummary `json:"paired_summary"`
		Complete  bool          `json:"complete"`
		Requested int           `json:"requested_pairs"`
	}
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if report.Complete || report.Requested != 20 || len(report.Samples) != 2 || !report.Samples[0].Success || report.Samples[1].Success {
		t.Fatalf("lost success or failure observation: %+v", report)
	}
	failed := report.Samples[1]
	if failed.Bytes != 17 || failed.PeakLogicalBytes != 40 || failed.TransferMS != 6 || failed.UnpackMS != 6 || failed.Failure != "asset-fetch-failed" {
		t.Fatalf("partial counters lost: %+v", failed)
	}
	if report.Paired.Count != 0 || report.Paired.Incomplete != 1 || !strings.HasPrefix(report.Paired.Status, "incomplete:") {
		t.Fatalf("failed pair contributed to summary: %+v", report.Paired)
	}
	for _, dir := range staging {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("staging not cleaned: %s: %v", dir, err)
		}
	}
}

func TestPairedSummaryUsesOnlySuccessfulCompletePairs(t *testing.T) {
	var samples []sample
	for i := 0; i < 20; i++ {
		// Pair deltas alternate -10 and +30, so median is +10.
		delta := -10.0
		if i%2 == 1 {
			delta = 30
		}
		samples = append(samples, sample{Index: i, Variant: "bundled", Success: true, TotalMS: 100}, sample{Index: i, Variant: "matching-binary", Success: true, TotalMS: 100 + delta})
	}
	samples = append(samples, sample{Index: 20, Variant: "bundled", Success: true, TotalMS: 9999}, sample{Index: 20, Variant: "matching-binary", Failure: "asset-fetch-failed", TotalMS: 1}, sample{Index: 21, Variant: "matching-binary", Success: true, TotalMS: 8888})
	_, paired := summarize(samples)
	if paired.Count != 20 || paired.Incomplete != 2 || paired.DeltaMedianMS != 10 || paired.DeltaP95MS != 30 || !strings.HasPrefix(paired.Status, "descriptive paired") {
		t.Fatalf("incorrect paired statistics: %+v", paired)
	}
	_, empty := summarize(nil)
	if empty.Count != 0 || !strings.HasPrefix(empty.Status, "incomplete:") {
		t.Fatalf("empty measurements reported complete: %+v", empty)
	}
}

func TestAcquisitionAlternatesOrderAndWritesCompletedReport(t *testing.T) {
	out := filepath.Join(t.TempDir(), "report.json")
	var locators []string
	args := acquisitionArgs(out, "https://example.test/bundle")
	args[7] = "https://example.test/launcher"
	args[11] = "https://example.test/binary"
	err := runWith(args, func(_ context.Context, a asset, _ string, _ int) (int64, int64, float64, float64, error) {
		locators = append(locators, a.url)
		return 1, 1, 1, 1, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bundle", "launcher", "binary", "launcher", "binary", "bundle"}
	for i, suffix := range want {
		if !strings.HasSuffix(locators[i], suffix) {
			t.Fatalf("order: %v", locators[:6])
		}
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Complete bool
		Paired   pairedSummary `json:"paired_summary"`
	}
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.Paired.Count != 20 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestAcquisitionRejectsCredentialBearingLocators(t *testing.T) {
	for _, locator := range []string{"https://user:secret@example.test/asset", "https://example.test/asset?signature=secret", "https://example.test/asset#secret"} {
		err := runWith(acquisitionArgs(filepath.Join(t.TempDir(), "report.json"), locator), func(context.Context, asset, string, int) (int64, int64, float64, float64, error) {
			t.Fatal("invalid locator fetched")
			return 0, 0, 0, 0, nil
		})
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe validation error: %v", err)
		}
	}
}

func TestCancellationRetainsFailedSample(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	variants := map[string][]asset{"bundled": {{url: "fixture"}}}
	samples, err := measure(ctx, variants, 20, func(ctx context.Context, _ asset, _ string, _ int) (int64, int64, float64, float64, error) {
		cancel()
		return 3, 0, 1, 0, ctx.Err()
	})
	if err == nil || len(samples) != 1 || samples[0].Success || samples[0].Failure != "cancelled-or-timed-out" || samples[0].Bytes != 3 {
		t.Fatalf("cancelled sample lost: %+v, %v", samples, err)
	}
}
