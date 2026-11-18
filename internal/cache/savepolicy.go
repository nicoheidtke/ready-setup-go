package cache

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

func snapshotSaveSkip(c Config, snap Snapshot) string {
	if snap.Kind == "build" && c.DisableBuildSave {
		return "build-save-disabled"
	}
	if c.SavePolicy == "on-miss" && snap.DependencyMatch {
		return "dependency-match"
	}
	return ""
}

// Empty policies belong to compatible old state and retain all-writer behavior.
// Unknown default-branch context intentionally denies writes, even in strict mode.
// Never include event contents or parser/filesystem errors in presentation.
func saveBranchSkip(c Config) string {
	if c.SaveBranches != "default" {
		return ""
	}
	ref := os.Getenv("GITHUB_REF")
	if !strings.HasPrefix(ref, "refs/heads/") {
		return "save policy permits only the repository default branch"
	}
	const unknown = "save policy could not establish the repository default branch"
	f, err := os.Open(os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return unknown
	}
	defer f.Close()
	const maxEventBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxEventBytes+1))
	if err != nil || len(data) > maxEventBytes {
		return unknown
	}
	var event struct {
		Repository struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
		} `json:"repository"`
	}
	if json.Unmarshal(data, &event) != nil || event.Repository.DefaultBranch == "" ||
		os.Getenv("GITHUB_REPOSITORY") == "" ||
		!strings.EqualFold(event.Repository.FullName, os.Getenv("GITHUB_REPOSITORY")) {
		return unknown
	}
	if ref != "refs/heads/"+event.Repository.DefaultBranch {
		return "save policy permits only the repository default branch"
	}
	return ""
}
