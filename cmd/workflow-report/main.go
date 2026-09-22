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
	Name       string   `json:"name"`
	Runner     string   `json:"runner_name"`
	Labels     []string `json:"labels"`
	Steps      []step   `json:"steps"`
	Conclusion string   `json:"conclusion"`
}
type observation struct {
	Job     string  `json:"job"`
	Step    string  `json:"step"`
	Runner  string  `json:"runner"`
	Seconds float64 `json:"seconds"`
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
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		if e != nil {
			return e
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
	var observations []observation
	groups := map[string][]float64{}
	var failed []string
	for _, j := range jobs {
		if !strings.HasPrefix(j.Name, "benchmark/") {
			continue
		}
		if j.Conclusion != "success" {
			failed = append(failed, j.Name+": "+j.Conclusion)
		}
		parts := strings.Split(j.Name, "/")
		if len(parts) != 4 {
			return fmt.Errorf("unexpected benchmark job name %q", j.Name)
		}
		for _, s := range j.Steps {
			if s.Conclusion != "success" || s.Started.IsZero() || s.Completed.IsZero() {
				continue
			}
			if !strings.HasPrefix(s.Name, "measure/") && s.Name != "Set up job" {
				continue
			}
			seconds := s.Completed.Sub(s.Started).Seconds()
			observations = append(observations, observation{j.Name, s.Name, j.Runner, seconds})
			group := parts[1] + "/" + parts[2] + "/" + s.Name
			groups[group] = append(groups[group], seconds)
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
	r := map[string]any{"revision": os.Getenv("GITHUB_SHA"), "run_id": run, "run_attempt": attempt, "observations": observations, "summary": summaries, "failed_jobs": failed, "notes": []string{"GitHub API timestamps have coarse resolution; subsecond differences are inconclusive.", "Set up job aggregates action acquisition and other preparation; it is not attributable to one action.", "The companion metadata artifacts contain runner image and action revisions.", "Baseline order alternates by sample. Each action gets its own installation directory for cold runs.", "Inspect failed jobs, paired differences and p95 before making performance claims."}}
	b, e := json.MarshalIndent(r, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(*out, append(b, '\n'), 0644)
}
