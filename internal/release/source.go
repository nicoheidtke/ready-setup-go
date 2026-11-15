package release

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SourceSelection separates the action snapshot from the workflow controller.
type SourceSelection struct {
	SourceRef   string `json:"source_ref"`
	Source      string `json:"source"`
	Anchor      string `json:"anchor"`
	PreviousTag string `json:"previous_tag,omitempty"`
}

// ResolveSource fetches an approved repository branch and freezes one commit
// from its history. Arbitrary rev expressions and fork/PR refs are not accepted.
func (r *Runner) ResolveSource(ctx context.Context, repository, remote, branch, revision, tag string) (SourceSelection, error) {
	var result SourceSelection
	c := Config{Repository: repository, Remote: remote, SourceRef: "refs/heads/" + branch, DistributionRef: "refs/heads/dist", Tag: "v0.0.0", Notes: "source resolution"}
	if tag != "" {
		c.Tag = tag
	}
	if err := configValid(c); err != nil {
		return result, err
	}
	if revision != "" && !shaPattern.MatchString(revision) {
		return result, fmt.Errorf("source commit must be a full lowercase 40-character SHA")
	}
	if _, err := r.git(ctx, nil, nil, "check-ref-format", c.SourceRef); err != nil {
		return result, fmt.Errorf("invalid source branch")
	}
	refs, err := r.refs(ctx, c)
	if err != nil {
		return result, err
	}
	if refs[c.SourceRef] == "" {
		return result, fmt.Errorf("source branch does not exist in the selected repository")
	}
	if _, err = r.git(ctx, nil, nil, "fetch", "--no-tags", remote, "+"+c.SourceRef+":refs/setup-go-faster/source-anchor"); err != nil {
		return result, err
	}
	anchor, err := r.git(ctx, nil, nil, "rev-parse", "refs/setup-go-faster/source-anchor^{commit}")
	if err != nil {
		return result, err
	}
	result.SourceRef, result.Anchor = c.SourceRef, strings.TrimSpace(string(anchor))
	if revision == "" {
		revision = result.Anchor
	}
	if _, err = r.git(ctx, nil, nil, "merge-base", "--is-ancestor", revision, result.Anchor); err != nil {
		return SourceSelection{}, fmt.Errorf("selected source commit must belong to the selected branch")
	}
	kind, err := r.git(ctx, nil, nil, "cat-file", "-t", revision)
	if err != nil || strings.TrimSpace(string(kind)) != "commit" {
		return SourceSelection{}, fmt.Errorf("source SHA must identify a commit object")
	}
	result.Source = revision
	if tag != "" {
		result.PreviousTag = PreviousVersion(tag, refs)
	}
	return result, nil
}

// Request is written by the trusted controller before candidate code executes.
// Comparing every configurable destination/policy prevents a candidate-produced
// plan from redirecting the privileged publisher to another ref or repository.
type Request struct {
	Config Config `json:"config"`
	Source string `json:"source"`
}

func CheckRequest(path string, p *Plan) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) > 256<<10 {
		return fmt.Errorf("trusted request exceeds limit")
	}
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&request); err != nil {
		return fmt.Errorf("invalid trusted release request")
	}
	if request.Config != p.Config || request.Source != p.Source {
		return fmt.Errorf("prepared plan differs from trusted release request")
	}
	return nil
}
