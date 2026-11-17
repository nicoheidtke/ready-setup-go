// release provides explicit plan/prepare/publish steps; it never builds on a
// consumer runner, dispatches workflows or changes repository settings.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ready-setup-go/internal/release"
)

func run() error {
	if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "-h" || os.Args[1] == "help") {
		fmt.Println("Usage: release source|plan|prepare|publish [options]\n\nsource resolves a branch and optional full SHA without remote writes.\nplan validates two reproducible builds and writes an ignored JSON plan.\nprepare creates only the local exact-byte distribution commit and tag.\npublish is dry-run by default; --execute atomically pushes and publishes.\n\nUse release <phase> --help for options.")
		return nil
	}
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: release source|plan|prepare|publish (publish is dry-run unless --execute)")
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	root := fs.String("root", ".", "source checkout root")
	planPath := fs.String("plan", ".cache/release/plan.json", "ignored reviewable plan file")
	execute := fs.Bool("execute", false, "perform external writes (publish only)")
	var c release.Config
	fs.StringVar(&c.Repository, "repository", "nicoheidtke/ready-setup-go", "GitHub owner/name")
	fs.StringVar(&c.Remote, "remote", "origin", "named Git remote")
	fs.StringVar(&c.SourceRef, "source-ref", "refs/heads/main", "source branch ref")
	fs.StringVar(&c.DistributionRef, "distribution-ref", "refs/heads/dist", "distribution branch ref")
	fs.StringVar(&c.Tag, "tag", "", "unused exact vMAJOR.MINOR.PATCH tag")
	fs.StringVar(&c.Dist, "dist", ".cache/release/dist", "verified all-six distribution")
	fs.StringVar(&c.Assets, "assets", ".cache/release/artifacts", "verified nine assets")
	fs.StringVar(&c.ReproDist, "repro-dist", ".cache/release/dist-repro", "second build distribution")
	fs.StringVar(&c.ReproAssets, "repro-assets", ".cache/release/artifacts-repro", "second build assets")
	fs.BoolVar(&c.UpdateAliases, "update-aliases", true, "advance eligible major/minor aliases without downgrades")
	fs.StringVar(&c.MakeLatest, "make-latest", "auto", "GitHub latest policy: auto, true, or false")
	fs.StringVar(&c.SourceAnchor, "source-anchor", "", "optional frozen branch anchor SHA from source resolution")
	requestPath := fs.String("request", "", "trusted request JSON to bind workflow publication inputs")
	sourceBranch := fs.String("branch", "main", "repository branch containing the selected source (source phase)")
	sourceCommit := fs.String("commit", "", "optional full source commit SHA (source phase)")
	notes := fs.String("notes", "", "release notes Markdown file (plan only)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	absPlan, err := filepath.Abs(*planPath)
	if err != nil {
		return err
	}
	r := release.New(absRoot)
	switch os.Args[1] {
	case "source":
		if *execute {
			return fmt.Errorf("source resolution performs no remote writes")
		}
		source, err := r.ResolveSource(ctx, c.Repository, c.Remote, *sourceBranch, *sourceCommit, c.Tag)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(source)
	case "plan":
		if *execute {
			return fmt.Errorf("--execute applies only to publish")
		}
		if _, err := os.Stat(absPlan); !os.IsNotExist(err) {
			return fmt.Errorf("plan file already exists; inspect/resume it rather than overwriting")
		}
		b, err := os.ReadFile(*notes)
		if err != nil {
			return fmt.Errorf("read release notes file")
		}
		c.Notes = string(b)
		p, err := r.CreatePlan(ctx, c)
		if err != nil {
			return err
		}
		if err = release.Save(absPlan, p); err != nil {
			return err
		}
		fmt.Printf("Plan saved: %s\nSource: %s\nNo Git refs or GitHub data changed. Review the plan before prepare.\n", absPlan, p.Source)
	case "prepare", "publish":
		p, err := release.Load(absPlan)
		if err != nil {
			return err
		}
		if *requestPath != "" {
			if err = release.CheckRequest(*requestPath, p); err != nil {
				return err
			}
		}
		if os.Args[1] == "prepare" {
			if *execute {
				return fmt.Errorf("--execute applies only to publish")
			}
			if err = r.Prepare(ctx, p, absPlan); err != nil {
				return err
			}
			fmt.Printf("Local distribution/tag prepared: %s\nNo remote writes.\n", p.Distribution)
			return nil
		}
		if !*execute {
			done, err := r.CheckPush(ctx, p)
			if err != nil {
				return err
			}
			fmt.Printf("Dry run valid: %s\nRemote refs already pushed: %t\nUse publish --execute with the same plan for atomic push, draft uploads and publication.\n", p.Distribution, done)
			return nil
		}
		if err = r.Publish(ctx, p, absPlan); err != nil {
			return err
		}
		fmt.Printf("Verified release: %s\nAction SHA: %s\n", p.URL, p.Distribution)
	default:
		return fmt.Errorf("unknown release phase; use plan, prepare or publish")
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}
