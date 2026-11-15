package release

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"setup-go-faster/internal/distribution"
)

func (r *Runner) verifyCommit(ctx context.Context, p *Plan) error {
	if !shaPattern.MatchString(p.Distribution) {
		return fmt.Errorf("prepare the local distribution commit first")
	}
	for _, name := range sortedKeys(p.Files) {
		b, err := r.git(ctx, nil, nil, "show", p.Distribution+":"+name)
		if err != nil {
			return err
		}
		file, err := os.ReadFile(filepath.Join(p.Config.Dist, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if string(b) != string(file) {
			return fmt.Errorf("committed release bytes differ: %s", name)
		}
		mode := "100644"
		for _, target := range distribution.Platforms {
			if name == distribution.Binary(target) && !strings.HasPrefix(target, "windows/") {
				mode = "100755"
			}
		}
		entry, err := r.git(ctx, nil, nil, "ls-tree", p.Distribution, "--", name)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(entry), mode+" blob ") {
			return fmt.Errorf("incorrect committed mode: %s", name)
		}
	}
	entries, err := r.git(ctx, nil, nil, "ls-tree", "-r", "--name-only", p.Distribution)
	if err != nil {
		return err
	}
	if len(strings.Split(strings.TrimSpace(string(entries)), "\n")) != len(p.Files) {
		return fmt.Errorf("unexpected committed distribution files")
	}
	parents, err := r.git(ctx, nil, nil, "rev-list", "--parents", "-n", "1", p.Distribution)
	if err != nil {
		return err
	}
	expectedParents := []string{p.Distribution}
	if p.ExpectedDistribution != "" && p.ExpectedDistribution != p.Source {
		expectedParents = append(expectedParents, p.ExpectedDistribution)
	}
	expectedParents = append(expectedParents, p.Source)
	if strings.Join(strings.Fields(string(parents)), " ") != strings.Join(expectedParents, " ") {
		return fmt.Errorf("distribution parent differs from planned distribution")
	}
	return nil
}

// Prepare writes Git objects and an annotated local tag, without changing the
// checkout/index or contacting GitHub. Persist the commit before creating its
// tag so interruption can resume without manufacturing a different commit.
func (r *Runner) Prepare(ctx context.Context, p *Plan, path string) error {
	if err := r.Validate(ctx, p); err != nil {
		return err
	}
	if p.Distribution == "" {
		index, err := os.CreateTemp("", "setup-go-faster-release-index-*")
		if err != nil {
			return err
		}
		indexPath := index.Name()
		index.Close()
		if err = os.Remove(indexPath); err != nil {
			return err
		}
		defer os.Remove(indexPath)
		env := []string{"GIT_INDEX_FILE=" + indexPath}
		if _, err = r.git(ctx, nil, env, "read-tree", "--empty"); err != nil {
			return err
		}
		var entries strings.Builder
		for _, name := range sortedKeys(p.Files) {
			data, err := os.ReadFile(filepath.Join(p.Config.Dist, filepath.FromSlash(name)))
			if err != nil {
				return err
			}
			blob, err := r.git(ctx, data, nil, "hash-object", "-w", "--stdin")
			if err != nil {
				return err
			}
			mode := "100644"
			for _, target := range distribution.Platforms {
				if name == distribution.Binary(target) && !strings.HasPrefix(target, "windows/") {
					mode = "100755"
				}
			}
			fmt.Fprintf(&entries, "%s %s\t%s\n", mode, strings.TrimSpace(string(blob)), name)
		}
		if _, err = r.git(ctx, []byte(entries.String()), env, "update-index", "--index-info"); err != nil {
			return err
		}
		tree, err := r.git(ctx, nil, env, "write-tree")
		if err != nil {
			return err
		}
		args := []string{"commit-tree", strings.TrimSpace(string(tree))}
		if p.ExpectedDistribution != "" && p.ExpectedDistribution != p.Source {
			args = append(args, "-p", p.ExpectedDistribution)
		}
		args = append(args, "-p", p.Source)
		commit, err := r.git(ctx, []byte("Bundle action release "+p.Config.Tag+" from "+p.Source+"\n"), nil, args...)
		if err != nil {
			return err
		}
		p.Distribution = strings.TrimSpace(string(commit))
		if err = Save(path, p); err != nil {
			return err
		}
	}
	if err := r.verifyCommit(ctx, p); err != nil {
		return err
	}
	ref := "refs/tags/" + p.Config.Tag
	current, _ := r.git(ctx, nil, nil, "show-ref", "--verify", "--hash", ref)
	if len(current) == 0 {
		if p.TagObject != "" {
			return fmt.Errorf("prepared tag disappeared; inspect local refs before resuming")
		}
		if _, err := r.git(ctx, nil, nil, "tag", "-a", p.Config.Tag, p.Distribution, "-m", "Bundled release "+p.Config.Tag+" from "+p.Source); err != nil {
			return err
		}
		current, _ = r.git(ctx, nil, nil, "show-ref", "--verify", "--hash", ref)
	}
	object := strings.TrimSpace(string(current))
	if p.TagObject != "" && p.TagObject != object {
		return fmt.Errorf("local tag changed after preparation")
	}
	kind, err := r.git(ctx, nil, nil, "cat-file", "-t", object)
	if err != nil || strings.TrimSpace(string(kind)) != "tag" {
		return fmt.Errorf("release tag must be annotated")
	}
	peeled, err := r.git(ctx, nil, nil, "rev-parse", ref+"^{}")
	if err != nil || strings.TrimSpace(string(peeled)) != p.Distribution {
		return fmt.Errorf("existing local tag belongs to a different distribution")
	}
	p.TagObject = object
	return Save(path, p)
}

// CheckPush accepts only the exact initial state or the fully completed atomic
// push. Partial/mismatched states require inspection instead of force updates.
func (r *Runner) CheckPush(ctx context.Context, p *Plan) (bool, error) {
	if err := r.Validate(ctx, p); err != nil {
		return false, err
	}
	if err := r.verifyCommit(ctx, p); err != nil {
		return false, err
	}
	if !shaPattern.MatchString(p.TagObject) {
		return false, fmt.Errorf("annotated tag is not prepared")
	}
	local, err := r.git(ctx, nil, nil, "show-ref", "--verify", "--hash", "refs/tags/"+p.Config.Tag)
	if err != nil || strings.TrimSpace(string(local)) != p.TagObject {
		return false, fmt.Errorf("local release tag changed")
	}
	refs, err := r.refs(ctx, p.Config)
	if err != nil {
		return false, err
	}
	eligible, latest := publicationPolicy(p.Config, refs)
	if latest != p.MakeLatest || strings.Join(sortedKeys(eligible), ",") != strings.Join(sortedKeys(p.ExpectedAliases), ",") {
		return false, fmt.Errorf("release policy changed after planning; inspect before resuming")
	}
	for ref, expected := range p.ExpectedAliases {
		if refs[ref] != expected && refs[ref] != p.Distribution {
			return false, fmt.Errorf("release alias changed; inspect before resuming")
		}
	}
	if refs[p.Config.DistributionRef] == p.Distribution && refs["refs/tags/"+p.Config.Tag] == p.TagObject {
		return true, nil
	}
	if refs[p.Config.DistributionRef] != p.ExpectedDistribution || refs["refs/tags/"+p.Config.Tag] != "" {
		return false, fmt.Errorf("remote refs changed; do not force or re-plan over a published release")
	}
	return false, nil
}

func (r *Runner) pushPlan(ctx context.Context, p *Plan) error {
	if r.push != nil {
		return r.push(ctx, p)
	}
	// Only generated output refs are pushed; source branches are read-only.
	args := []string{"push", "--atomic", "--force-with-lease=" + p.Config.DistributionRef + ":" + p.ExpectedDistribution, "--force-with-lease=refs/tags/" + p.Config.Tag + ":", p.Config.Remote, p.Distribution + ":" + p.Config.DistributionRef, "refs/tags/" + p.Config.Tag + ":refs/tags/" + p.Config.Tag}
	_, err := r.git(ctx, nil, nil, args...)
	return err
}

// Aliases move only after the release and every remote asset have been verified.
func (r *Runner) promoteAliases(ctx context.Context, p *Plan) error {
	if len(p.ExpectedAliases) == 0 {
		return nil
	}
	refs, err := r.refs(ctx, p.Config)
	if err != nil {
		return err
	}
	args := []string{"push", "--atomic"}
	var updates []string
	for _, ref := range sortedKeys(p.ExpectedAliases) {
		if refs[ref] == p.Distribution {
			continue
		}
		if refs[ref] != p.ExpectedAliases[ref] {
			return fmt.Errorf("alias changed: %s", ref)
		}
		args = append(args, "--force-with-lease="+ref+":"+p.ExpectedAliases[ref])
		updates = append(updates, p.Distribution+":"+ref)
	}
	if len(updates) != 0 {
		args = append(args, p.Config.Remote)
		args = append(args, updates...)
		if _, err = r.git(ctx, nil, nil, args...); err != nil {
			return err
		}
	}
	refs, err = r.refs(ctx, p.Config)
	if err != nil {
		return err
	}
	for _, ref := range sortedKeys(p.ExpectedAliases) {
		if refs[ref] != p.Distribution {
			return fmt.Errorf("alias update not verified: %s", ref)
		}
	}
	return nil
}
