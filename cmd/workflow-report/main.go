// workflow-report retrieves coarse GitHub step timings for the manual benchmark.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type step struct {
	Name       string    `json:"name"`
	Conclusion string    `json:"conclusion"`
	Started    time.Time `json:"started_at"`
	Completed  time.Time `json:"completed_at"`
}
type job struct {
	Name       string    `json:"name"`
	Runner     string    `json:"runner_name"`
	Labels     []string  `json:"labels"`
	Steps      []step    `json:"steps"`
	Conclusion string    `json:"conclusion"`
	Status     string    `json:"status"`
	Started    time.Time `json:"started_at"`
	Completed  time.Time `json:"completed_at"`
}
type observation struct {
	Job        string  `json:"job"`
	Step       string  `json:"step"`
	Runner     string  `json:"runner"`
	Seconds    float64 `json:"seconds"`
	Conclusion string  `json:"conclusion"`
	Timed      bool    `json:"timed"`
	Post       bool    `json:"post"`
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	out := flag.String("out", "workflow-timings.json", "report file")
	flag.Parse()
	repo, run, attempt := os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_RUN_ID"), os.Getenv("GITHUB_RUN_ATTEMPT")
	if repo == "" || run == "" || attempt == "" {
		return fmt.Errorf("GitHub repository, run ID and run attempt are required")
	}
	base := os.Getenv("GITHUB_API_URL")
	if base == "" {
		base = "https://api.github.com"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var jobs []job
	for page := 1; ; page++ {
		u := fmt.Sprintf("%s/repos/%s/actions/runs/%s/attempts/%s/jobs?per_page=100&page=%d", base, repo, run, attempt, page)
		req, e := http.NewRequestWithContext(ctx, "GET", u, nil)
		if e != nil {
			return e
		}
		req.Header.Set("Authorization", "Bearer "+os.Getenv("GH_TOKEN"))
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("jobs API request failed")
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
		resp.Body.Close()
		if e != nil {
			return fmt.Errorf("jobs API response read failed")
		}
		if len(b) > 16<<20 {
			return fmt.Errorf("jobs API response exceeds size limit")
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("jobs API HTTP %d", resp.StatusCode)
		}
		var result struct {
			Jobs []job `json:"jobs"`
		}
		if e = json.Unmarshal(b, &result); e != nil {
			return e
		}
		jobs = append(jobs, result.Jobs...)
		if len(result.Jobs) < 100 {
			break
		}
	}
	r := summarizeJobs(jobs)
	r["revision"] = os.Getenv("GITHUB_SHA")
	r["run_id"] = run
	r["run_attempt"] = attempt
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(*out, append(b, '\n'), 0644)
}

// Keep complete selected jobs and failures, including post hooks. Normal-step
// artifact uploads happen before cache post-save; this collector runs downstream.
func summarizeJobs(jobs []job) map[string]any {
	var observations []observation
	var selected []job
	groups := map[string][]float64{}
	var failed []string
	for _, j := range jobs {
		installer := strings.HasPrefix(j.Name, "benchmark/")
		if !installer && !strings.HasPrefix(j.Name, "cache-benchmark/") && !strings.HasPrefix(j.Name, "cache-validation/") {
			continue
		}
		selected = append(selected, j)
		if j.Conclusion != "success" {
			failed = append(failed, j.Name+": "+j.Conclusion)
		}
		parts := strings.Split(j.Name, "/")
		groupBase := j.Name
		if installer && len(parts) == 4 {
			groupBase = parts[1] + "/" + parts[2]
		}
		jobTimed := !j.Started.IsZero() && !j.Completed.IsZero() && !j.Completed.Before(j.Started)
		whole := observation{Job: j.Name, Step: "__job__", Runner: j.Runner, Conclusion: j.Conclusion, Timed: jobTimed}
		if jobTimed {
			whole.Seconds = j.Completed.Sub(j.Started).Seconds()
		}
		observations = append(observations, whole)
		for _, s := range j.Steps {
			post := strings.HasPrefix(s.Name, "Post ")
			if installer && !strings.HasPrefix(s.Name, "measure/") && s.Name != "Set up job" && !post {
				continue
			}
			timed := !s.Started.IsZero() && !s.Completed.IsZero() && !s.Completed.Before(s.Started)
			o := observation{Job: j.Name, Step: s.Name, Runner: j.Runner, Conclusion: s.Conclusion, Timed: timed, Post: post}
			if timed {
				o.Seconds = s.Completed.Sub(s.Started).Seconds()
			}
			observations = append(observations, o)
			if timed && s.Conclusion == "success" {
				group := groupBase + "/" + s.Name
				groups[group] = append(groups[group], o.Seconds)
			}
		}
	}
	summaries := map[string]any{}
	for name, values := range groups {
		sort.Float64s(values)
		n := len(values)
		median := values[n/2]
		if n%2 == 0 {
			median = (median + values[n/2-1]) / 2
		}
		status := "descriptive only; compare paired samples"
		if n < 20 {
			status = "incomplete: fewer than 20 successful samples"
		}
		summaries[name] = map[string]any{"count": n, "median_seconds": median, "p95_seconds": values[(95*n+99)/100-1], "status": status}
	}
	return map[string]any{"jobs": selected, "observations": observations, "summary": summaries, "failed_jobs": failed, "notes": []string{"GitHub API timestamps have coarse resolution; subsecond differences are inconclusive.", "Set up job combines action acquisition and other preparation and cannot be attributed to one action.", "Selected jobs include real post-hook steps and whole-job timestamps. Earlier same-job artifacts cannot contain a later post-save report.", "Failed and skipped steps are retained; statistical summaries contain only successful timed steps and explicitly flag fewer than 20 samples.", "Cache benchmark native JSON contains the 20 paired restore/workload/save samples; one workflow helper step is not 20 workflow observations.", "Inspect raw failures, paired differences and p95 before making performance claims."}}
}
