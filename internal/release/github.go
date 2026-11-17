package release

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type asset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	State  string `json:"state"`
}
type remoteRelease struct {
	ID         int64   `json:"id"`
	Tag        string  `json:"tag_name"`
	Target     string  `json:"target_commitish"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Body       string  `json:"body"`
	UploadURL  string  `json:"upload_url"`
	URL        string  `json:"html_url"`
	Assets     []asset `json:"assets"`
}
type github struct {
	token, base string
	client      *http.Client
	localTest   bool
}

func (g *github) request(ctx context.Context, method, address string, body io.Reader, size int64, contentType string, out any) (int, error) {
	u, err := url.Parse(address)
	if err != nil || u.User != nil || (!g.localTest && (u.Scheme != "https" || (u.Host != "api.github.com" && u.Host != "uploads.github.com"))) {
		return 0, fmt.Errorf("unexpected GitHub endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return 0, fmt.Errorf("create GitHub request")
	}
	req.ContentLength = size
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "ready-setup-go-release")
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	client := *g.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("GitHub %s request failed; resume with the same plan to inspect its outcome", method)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound && method == http.MethodGet {
		return resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("GitHub %s returned HTTP %d; no automatic write retry", method, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read GitHub response; resume with the same plan")
	}
	if len(data) >= 8<<20 {
		return resp.StatusCode, fmt.Errorf("GitHub response exceeds limit")
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return resp.StatusCode, fmt.Errorf("invalid GitHub response; inspect outcome before resuming")
	}
	return resp.StatusCode, nil
}
func (g *github) json(ctx context.Context, method, address string, body, out any) (int, error) {
	var b []byte
	var err error
	if body != nil {
		b, err = json.Marshal(body)
		if err != nil {
			return 0, err
		}
	}
	return g.request(ctx, method, address, bytes.NewReader(b), int64(len(b)), "application/json", out)
}

func releaseBody(p *Plan) string {
	return p.Config.Notes + "\n\nAction distribution: `" + p.Distribution + "`\n\nExact source: `" + p.Source + "`. Checksums and source metadata are included; local verification is not a hosted build attestation.\n"
}

// GitHub's by-tag endpoint returns published releases. Draft discovery uses the
// authenticated list endpoint, including after an ambiguous create response.
func (g *github) findRelease(ctx context.Context, base, tag string) (*remoteRelease, error) {
	var found *remoteRelease
	for page := 1; page <= 10; page++ {
		var releases []remoteRelease
		status, err := g.json(ctx, http.MethodGet, base+"?per_page=100&page="+strconv.Itoa(page), nil, &releases)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("release list was unavailable; refusing to create a draft")
		}
		for _, release := range releases {
			if release.Tag == tag {
				if found != nil {
					return nil, fmt.Errorf("multiple releases use the planned tag")
				}
				copy := release
				found = &copy
			}
		}
		if len(releases) < 100 {
			return found, nil
		}
	}
	return nil, fmt.Errorf("release list pagination exceeded limit; inspect releases before publication")
}

func (g *github) publish(ctx context.Context, p *Plan) (string, error) {
	base := g.base + "/repos/" + p.Config.Repository + "/releases"
	latest := "false"
	if p.MakeLatest {
		latest = "true"
	}
	found, err := g.findRelease(ctx, base, p.Config.Tag)
	if err != nil {
		return "", err
	}
	var release remoteRelease
	if found != nil {
		release = *found
	}
	body := releaseBody(p)
	if found == nil {
		_, err = g.json(ctx, http.MethodPost, base, map[string]any{"tag_name": p.Config.Tag, "target_commitish": p.Distribution, "name": p.Config.Tag, "body": body, "draft": true, "prerelease": false, "make_latest": latest}, &release)
		if err != nil {
			return "", err
		}
	}
	if release.ID <= 0 || release.Tag != p.Config.Tag || release.Target != p.Distribution || release.Body != body || release.Prerelease {
		return "", fmt.Errorf("existing release differs from this plan; refusing to replace it")
	}
	assetsURL := base + "/" + strconv.FormatInt(release.ID, 10) + "/assets?per_page=100"
	var existing []asset
	status, err := g.json(ctx, http.MethodGet, assetsURL, nil, &existing)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("release asset list was unavailable")
	}
	byName := map[string]asset{}
	for _, a := range existing {
		if p.Assets[a.Name] == "" || byName[a.Name].Name != "" {
			return "", fmt.Errorf("unexpected or duplicate remote asset")
		}
		byName[a.Name] = a
	}
	for _, name := range sortedKeys(p.Assets) {
		info, err := os.Stat(filepath.Join(p.Config.Assets, name))
		if err != nil {
			return "", err
		}
		a, found := byName[name]
		if !found {
			if !release.Draft {
				return "", fmt.Errorf("published release is missing an asset; refusing to modify it")
			}
			address, _, _ := strings.Cut(release.UploadURL, "{")
			if !g.localTest && address != "https://uploads.github.com/repos/"+p.Config.Repository+"/releases/"+strconv.FormatInt(release.ID, 10)+"/assets" {
				return "", fmt.Errorf("unexpected release upload endpoint")
			}
			f, err := os.Open(filepath.Join(p.Config.Assets, name))
			if err != nil {
				return "", err
			}
			_, err = g.request(ctx, http.MethodPost, address+"?name="+url.QueryEscape(name), f, info.Size(), "application/octet-stream", &a)
			f.Close()
			if err != nil {
				return "", err
			}
		}
		if a.Name != name || a.Size != info.Size() || a.Digest != "sha256:"+p.Assets[name] || a.State != "uploaded" {
			return "", fmt.Errorf("remote asset mismatch: %s; do not replace it automatically", name)
		}
	}
	if release.Draft {
		if _, err = g.json(ctx, http.MethodPatch, base+"/"+strconv.FormatInt(release.ID, 10), map[string]any{"draft": false, "prerelease": false, "make_latest": latest}, &release); err != nil {
			return "", err
		}
	}
	// A lost PATCH response can be resumed: an exactly matching published release
	// is verified, never edited. Re-fetch every digest after publication.
	status, err = g.json(ctx, http.MethodGet, base+"/tags/"+url.PathEscape(p.Config.Tag), nil, &release)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("published release could not be re-fetched")
	}
	if release.Draft || release.Prerelease || release.Target != p.Distribution || release.Tag != p.Config.Tag || release.Body != body || len(release.Assets) != len(p.Assets) {
		return "", fmt.Errorf("published release verification failed")
	}
	seen := map[string]bool{}
	for _, a := range release.Assets {
		if seen[a.Name] || p.Assets[a.Name] == "" {
			return "", fmt.Errorf("published asset set differs")
		}
		info, err := os.Stat(filepath.Join(p.Config.Assets, a.Name))
		if err != nil || a.Digest != "sha256:"+p.Assets[a.Name] || a.State != "uploaded" || a.Size != info.Size() {
			return "", fmt.Errorf("published asset verification failed")
		}
		seen[a.Name] = true
	}
	if release.URL != "https://github.com/"+p.Config.Repository+"/releases/tag/"+p.Config.Tag && !g.localTest {
		return "", fmt.Errorf("unexpected published release URL")
	}
	return release.URL, nil
}

func (r *Runner) credential(ctx context.Context) (string, error) {
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if value := os.Getenv(name); value != "" {
			return value, nil
		}
	}
	b, err := r.git(ctx, []byte("protocol=https\nhost=github.com\n\n"), nil, "credential", "fill")
	if err != nil {
		return "", fmt.Errorf("GitHub authentication unavailable; configure Git credentials or GITHUB_TOKEN")
	}
	for _, line := range strings.Split(string(b), "\n") {
		if value, ok := strings.CutPrefix(line, "password="); ok && value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("Git credential helper returned no GitHub credential")
}

// Publish is the only entry point which performs external writes. Callers must
// first review/prepare a plan; the CLI additionally requires --execute.
func (r *Runner) Publish(ctx context.Context, p *Plan, path string) error {
	done, err := r.CheckPush(ctx, p)
	if err != nil {
		return err
	}
	token, err := r.credential(ctx)
	if err != nil {
		return err
	}
	if !done {
		if err = r.pushPlan(ctx, p); err != nil {
			return err
		}
		if done, err = r.CheckPush(ctx, p); err != nil || !done {
			return fmt.Errorf("atomic push outcome not verified; inspect/resume the same plan")
		}
	}
	g := github{token: token, base: "https://api.github.com", client: &http.Client{Timeout: 2 * time.Minute}}
	p.URL, err = g.publish(ctx, p)
	if err != nil {
		return err
	}
	if err = Save(path, p); err != nil {
		return err
	}
	if err = r.promoteAliases(ctx, p); err != nil {
		return fmt.Errorf("release published; alias promotion incomplete: %w; resume the same plan", err)
	}
	return nil
}
