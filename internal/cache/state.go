package cache

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"ready-setup-go/internal/cachearchive"
)

type Snapshot struct {
	Kind             string              `json:"kind"`
	Key              string              `json:"key"`
	Prefix           string              `json:"prefix"`
	CompatiblePrefix string              `json:"compatible_prefix"`
	Version          string              `json:"version"`
	Roots            []cachearchive.Root `json:"roots"`
	// RestoredDigest identifies validated archive contents, excluding mtimes.
	RestoredDigest string `json:"restored_digest,omitempty"`
	// RestoredChunks are bounded content-verified hints, never remote locators.
	RestoredChunks []chunkHint `json:"restored_chunks,omitempty"`
}

type State struct {
	Format    string     `json:"format"`
	Config    Config     `json:"config"`
	Snapshots []Snapshot `json:"snapshots"`
}

func NewState(c Config, version, dependencies string, roots []cachearchive.Root) (State, error) {
	if err := c.Validate(); err != nil {
		return State{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return State{}, err
	}
	id := digest(os.Getenv("GITHUB_RUN_ID")+"\x00"+os.Getenv("GITHUB_RUN_ATTEMPT")) + "-" + hex.EncodeToString(nonce[:])
	storage := ""
	if c.Storage == "chunked" {
		storage = "chunks-"
	}
	base := storage + fmt.Sprintf("%s-%s-%s-go%s-%s-%s-%s-", Format, runtime.GOOS, runtime.GOARCH, version, namespace(c.Scope), c.Layout, c.Compression)
	s := State{Format: Format, Config: c}
	groups := [][]cachearchive.Root{roots}
	if c.Layout == "split" {
		groups = nil
		for _, r := range roots {
			groups = append(groups, []cachearchive.Root{r})
		}
	}
	for _, rs := range groups {
		kind := "combined"
		if len(rs) == 1 {
			kind = rs[0].Name
		}
		kindBase := base
		if kind == "modules" && c.ModuleScope != "" {
			// This explicit opt-in namespace shares module contents across jobs
			// and compilers. Build snapshots retain the ordinary namespace.
			shared := digest(os.Getenv("GITHUB_REPOSITORY") + "\x00" + c.ModuleScope)
			kindBase = storage + fmt.Sprintf("%s-%s-%s-shared-modules-%s-split-%s-", Format, runtime.GOOS, runtime.GOARCH, shared, c.Compression)
		}
		compatible := kindBase + kind + "-"
		prefix := compatible + dependencies + "-"
		s.Snapshots = append(s.Snapshots, Snapshot{Kind: kind, Roots: rs, CompatiblePrefix: compatible, Prefix: prefix, Key: prefix + id, Version: versionDigest(Format + "\x00" + c.Compression + "\x00" + kind)})
	}
	return s, nil
}

func persist(s State) (string, error) {
	dir, err := os.MkdirTemp(os.Getenv("RUNNER_TEMP"), "ready-setup-go-state-*")
	if err != nil {
		return "", err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		os.Remove(dir)
		return "", err
	}
	b, err := json.Marshal(s)
	if err != nil {
		os.Remove(dir)
		return "", err
	}
	path := filepath.Join(dir, "state.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		os.Remove(dir)
		return "", err
	}
	return path, nil
}
