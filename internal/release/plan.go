// Package release prepares and publishes this action's bundled prereleases.
// It never dispatches workflows or changes repository settings.
package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"setup-go-better/internal/distribution"
)

var shaPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var tagPattern = regexp.MustCompile(`^v0\.0\.0-preview\.[0-9]{8}(\.[1-9][0-9]*)?$`)

type Config struct {
	Root, Repository, Remote, SourceRef, PreviewRef, Tag string
	Dist, Assets, ReproDist, ReproAssets, Notes          string
}

// Plan contains no credential. Keep it under an ignored directory and review it
// before prepare/publish. Its expected remote refs are compare-and-swap guards.
type Plan struct {
	Config          Config            `json:"config"`
	Source          string            `json:"source"`
	ExpectedSource  string            `json:"expected_source"`
	ExpectedPreview string            `json:"expected_preview"`
	Files           map[string]string `json:"files_sha256"`
	Assets          map[string]string `json:"assets_sha256"`
	Distribution    string            `json:"distribution,omitempty"`
	TagObject       string            `json:"tag_object,omitempty"`
	URL             string            `json:"published_url,omitempty"`
}

// Runner's only injectable boundaries are private test seams. The CLI exposes
// no alternate API host, shell command, force-push or token-in-URL option.
type Runner struct {
	Root   string
	remote func(context.Context, Config) (map[string]string, error)
	push   func(context.Context, *Plan) error
}

func New(root string) *Runner { return &Runner{Root: root} }

func (r *Runner) git(ctx context.Context, input []byte, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = bytes.NewReader(input)
	b, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Git operation failed (%s); inspect local refs/authentication without exposing credentials", args[0])
	}
	return b, nil
}

func configValid(c Config) error {
	if !repoPattern.MatchString(c.Repository) || !tagPattern.MatchString(c.Tag) {
		return fmt.Errorf("repository must be owner/name and tag a dated v0.0.0-preview.YYYYMMDD[.N]")
	}
	if c.Remote == "" || strings.HasPrefix(c.Remote, "-") || strings.ContainsAny(c.Remote, " /\\\r\n\x00") {
		return fmt.Errorf("invalid named Git remote")
	}
	for _, ref := range []string{c.SourceRef, c.PreviewRef} {
		if !strings.HasPrefix(ref, "refs/heads/") || strings.ContainsAny(ref, " ~^:?*[\\\r\n\x00") || strings.Contains(ref, "..") || strings.HasSuffix(ref, "/") {
			return fmt.Errorf("invalid branch ref")
		}
	}
	if c.SourceRef == c.PreviewRef {
		return fmt.Errorf("source and preview refs must differ")
	}
	if len(c.Notes) == 0 || len(c.Notes) > 64<<10 {
		return fmt.Errorf("release notes must contain 1–65536 bytes")
	}
	return nil
}

func hashes(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("nonregular release input")
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.ContainsAny(rel, "\r\n\t\x00") {
			return fmt.Errorf("unsafe release filename")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	return out, err
}

func same(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (r *Runner) refs(ctx context.Context, c Config) (map[string]string, error) {
	if r.remote != nil {
		return r.remote(ctx, c)
	}
	u, err := r.git(ctx, nil, nil, "remote", "get-url", c.Remote)
	if err != nil {
		return nil, err
	}
	address := strings.TrimSpace(string(u))
	if !approvedGitURL(address, c.Repository) {
		return nil, fmt.Errorf("remote URL must identify the selected GitHub repository without embedded credentials")
	}
	pushURLs, err := r.git(ctx, nil, nil, "remote", "get-url", "--push", "--all", c.Remote)
	if err != nil {
		return nil, err
	}
	pushLines := strings.Split(strings.TrimSpace(string(pushURLs)), "\n")
	if len(pushLines) != 1 || !approvedGitURL(strings.TrimSpace(pushLines[0]), c.Repository) {
		return nil, fmt.Errorf("exactly one push URL must identify the selected GitHub repository")
	}
	b, err := r.git(ctx, nil, nil, "ls-remote", "--refs", c.Remote, c.SourceRef, c.PreviewRef, "refs/tags/"+c.Tag)
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 || !shaPattern.MatchString(f[0]) {
			return nil, fmt.Errorf("invalid remote ref response")
		}
		refs[f[1]] = f[0]
	}
	return refs, nil
}

func approvedGitURL(address, repository string) bool {
	return address == "https://github.com/"+repository+".git" || address == "https://github.com/"+repository || address == "git@github.com:"+repository+".git"
}

func (r *Runner) CreatePlan(ctx context.Context, c Config) (*Plan, error) {
	if err := configValid(c); err != nil {
		return nil, err
	}
	var err error
	c.Root, err = filepath.Abs(r.Root)
	if err != nil {
		return nil, err
	}
	paths := []*string{&c.Dist, &c.Assets, &c.ReproDist, &c.ReproAssets}
	for _, p := range paths {
		*p, err = filepath.Abs(*p)
		if err != nil {
			return nil, err
		}
	}
	if c.Dist == c.ReproDist || c.Assets == c.ReproAssets {
		return nil, fmt.Errorf("two distinct build copies are required")
	}
	for _, pair := range [][2]string{{c.Dist, c.Assets}, {c.ReproDist, c.ReproAssets}} {
		if err = distribution.Verify(pair[0], pair[1], false, ""); err != nil {
			return nil, err
		}
	}
	a, err := hashes(c.Dist)
	if err != nil {
		return nil, err
	}
	b, err := hashes(c.ReproDist)
	if err != nil {
		return nil, err
	}
	assets, err := hashes(c.Assets)
	if err != nil {
		return nil, err
	}
	assets2, err := hashes(c.ReproAssets)
	if err != nil {
		return nil, err
	}
	if !same(a, b) || !same(assets, assets2) {
		return nil, fmt.Errorf("build copies differ")
	}
	current, err := distribution.Source(c.Root, distribution.Platforms, true)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(c.Dist, "SOURCE_PROVENANCE.json"))
	if err != nil {
		return nil, err
	}
	var p distribution.Provenance
	if err = json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("invalid source provenance")
	}
	if p.Revision != current.Revision || p.SourceSHA256 != current.SourceSHA256 || p.Compiler != current.Compiler {
		return nil, fmt.Errorf("candidate must match this exact clean source checkout and compiler")
	}
	refs, err := r.refs(ctx, c)
	if err != nil {
		return nil, err
	}
	if !shaPattern.MatchString(refs[c.SourceRef]) || !shaPattern.MatchString(refs[c.PreviewRef]) || refs["refs/tags/"+c.Tag] != "" {
		return nil, fmt.Errorf("source/preview branch must exist and release tag must be unused")
	}
	if _, err = r.git(ctx, nil, nil, "merge-base", "--is-ancestor", refs[c.SourceRef], current.Revision); err != nil {
		return nil, fmt.Errorf("remote source must be an ancestor of the release source")
	}
	if _, err = r.git(ctx, nil, nil, "cat-file", "-e", refs[c.PreviewRef]+"^{commit}"); err != nil {
		return nil, fmt.Errorf("fetch the named remote's preview branch objects before planning")
	}
	branch, err := r.git(ctx, nil, nil, "rev-parse", c.SourceRef)
	if err != nil || strings.TrimSpace(string(branch)) != current.Revision {
		return nil, fmt.Errorf("local source ref must identify release source")
	}
	if local, _ := r.git(ctx, nil, nil, "show-ref", "--verify", "--hash", "refs/tags/"+c.Tag); len(local) > 0 {
		return nil, fmt.Errorf("local release tag already exists")
	}
	return &Plan{Config: c, Source: current.Revision, ExpectedSource: refs[c.SourceRef], ExpectedPreview: refs[c.PreviewRef], Files: a, Assets: assets}, nil
}

func (r *Runner) Validate(ctx context.Context, p *Plan) error {
	if err := configValid(p.Config); err != nil {
		return err
	}
	if !shaPattern.MatchString(p.Source) || !shaPattern.MatchString(p.ExpectedSource) || !shaPattern.MatchString(p.ExpectedPreview) {
		return fmt.Errorf("invalid plan revisions")
	}
	root, err := filepath.Abs(r.Root)
	if err != nil || root != p.Config.Root {
		return fmt.Errorf("plan belongs to a different source checkout")
	}
	if err = distribution.Verify(p.Config.Dist, p.Config.Assets, false, ""); err != nil {
		return err
	}
	f, err := hashes(p.Config.Dist)
	if err != nil {
		return err
	}
	a, err := hashes(p.Config.Assets)
	if err != nil {
		return err
	}
	if !same(f, p.Files) || !same(a, p.Assets) {
		return fmt.Errorf("release inputs changed after planning")
	}
	current, err := distribution.Source(root, distribution.Platforms, true)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(p.Config.Dist, "SOURCE_PROVENANCE.json"))
	if err != nil {
		return err
	}
	var provenance distribution.Provenance
	if json.Unmarshal(data, &provenance) != nil || current.Revision != p.Source || provenance.Revision != p.Source || current.SourceSHA256 != provenance.SourceSHA256 || current.Compiler != provenance.Compiler {
		return fmt.Errorf("source or compiler changed after planning")
	}
	return nil
}

func Save(path string, p *Plan) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	// Plans must be ignored so writing one cannot dirty the validated checkout.
	cmd := exec.Command("git", "check-ignore", "--quiet", "--", path)
	cmd.Dir = p.Config.Root
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("plan path must be ignored by Git (use .cache/release/plan.json)")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".release-plan-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func Load(path string) (*Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) > 256<<10 {
		return nil, fmt.Errorf("release plan exceeds limit")
	}
	var p Plan
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid release plan")
	}
	return &p, nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
