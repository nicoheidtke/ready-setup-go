package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIncludesCachePostAndWholeJobFailures(t *testing.T) {
	start := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	jobs := []job{
		{Name: "cache-validation/ubuntu/split/gzip", Runner: "runner1", Conclusion: "failure", Started: start, Completed: start.Add(20 * time.Second), Steps: []step{
			{Name: "measure/restore-through-node-action", Conclusion: "success", Started: start, Completed: start.Add(time.Second)},
			{Name: "measure/pinned-package-workload", Conclusion: "success", Started: start.Add(time.Second), Completed: start.Add(10 * time.Second)},
			{Name: "Post measure/restore-through-node-action", Conclusion: "failure", Started: start.Add(10 * time.Second), Completed: start.Add(20 * time.Second)},
			{Name: "skipped step", Conclusion: "skipped"},
		}},
		{Name: "cache-benchmark/ubuntu/small/unchanged/split/gzip", Conclusion: "success", Steps: []step{{Name: "measure/paired-native-cache-lifecycle", Conclusion: "success", Started: start, Completed: start.Add(time.Second)}}},
		{Name: "report", Conclusion: "in_progress"},
	}
	r := summarizeJobs(jobs)
	selected := r["jobs"].([]job)
	if len(selected) != 2 {
		t.Fatal(selected)
	}
	observations := r["observations"].([]observation)
	var whole, post, skipped bool
	for _, o := range observations {
		if o.Job == jobs[0].Name {
			switch o.Step {
			case "__job__":
				whole = o.Timed && o.Seconds == 20 && o.Conclusion == "failure"
			case "Post measure/restore-through-node-action":
				post = o.Post && o.Timed && o.Seconds == 10 && o.Conclusion == "failure"
			case "skipped step":
				skipped = !o.Timed && o.Conclusion == "skipped"
			}
		}
	}
	if !whole || !post || !skipped {
		t.Fatalf("lost whole=%v post=%v skipped=%v observations=%+v", whole, post, skipped, observations)
	}
	if failed := r["failed_jobs"].([]string); len(failed) != 1 {
		t.Fatal(failed)
	}
	for key := range r["summary"].(map[string]any) {
		if strings.Contains(key, "Post measure/") {
			t.Fatal("failed post included in successful summary")
		}
	}
}

func TestInstallerSamplesStillGroupAcrossTwentyJobs(t *testing.T) {
	start := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	var jobs []job
	for i := 0; i < 20; i++ {
		jobs = append(jobs, job{Name: fmt.Sprintf("benchmark/ubuntu/cold/%d", i), Conclusion: "success", Steps: []step{{Name: "measure/better", Conclusion: "success", Started: start, Completed: start.Add(time.Duration(i+1) * time.Second)}}})
	}
	r := summarizeJobs(jobs)
	summaries := r["summary"].(map[string]any)
	s := summaries["ubuntu/cold/measure/better"].(map[string]any)
	if s["count"] != 20 || s["median_seconds"] != 10.5 || s["p95_seconds"] != 19.0 {
		t.Fatal(s)
	}
	if strings.Contains(s["status"].(string), "incomplete") {
		t.Fatal(s)
	}
}
