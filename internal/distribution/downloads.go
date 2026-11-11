package distribution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
)

const DownloadsFile = "NATIVE_DOWNLOADS.json"
const MaxNativeBytes int64 = 256 << 20

var downloadBasePattern = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*/releases/download/[A-Za-z0-9][A-Za-z0-9._-]*$`)

type NativeAsset struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type NativeDownloads struct {
	Format    string                 `json:"format"`
	BaseURL   string                 `json:"base_url"`
	Platforms map[string]NativeAsset `json:"platforms"`
}

func ValidateDownloadBase(base string) error {
	u, err := url.Parse(base)
	if err != nil || !downloadBasePattern.MatchString(base) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("native download base must be https://github.com/OWNER/REPO/releases/download/TAG with a fixed tag")
	}
	return nil
}

func WriteNativeDownloads(root, binaries, base string, platforms []string) error {
	if err := ValidateDownloadBase(base); err != nil {
		return err
	}
	manifest := NativeDownloads{Format: "sgb-native-v1", BaseURL: base, Platforms: map[string]NativeAsset{}}
	for _, platform := range platforms {
		data, err := os.ReadFile(filepath.Join(binaries, filepath.FromSlash(Binary(platform))))
		if err != nil {
			return err
		}
		if len(data) == 0 || int64(len(data)) > MaxNativeBytes {
			return fmt.Errorf("native binary exceeds size bound")
		}
		sum := sha256.Sum256(data)
		manifest.Platforms[platform] = NativeAsset{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, DownloadsFile), append(data, '\n'), 0644)
}

// ReadNativeDownloads returns nil for a traditional bundled distribution.
func ReadNativeDownloads(root string) (*NativeDownloads, error) {
	f, err := os.Open(filepath.Join(root, DownloadsFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return nil, fmt.Errorf("invalid native download manifest")
	}
	var manifest NativeDownloads
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("invalid native download manifest")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing native download manifest data")
	}
	if manifest.Format != "sgb-native-v1" || len(manifest.Platforms) == 0 || len(manifest.Platforms) > len(Platforms) {
		return nil, fmt.Errorf("invalid native download manifest")
	}
	if err := ValidateDownloadBase(manifest.BaseURL); err != nil {
		return nil, err
	}
	for platform, asset := range manifest.Platforms {
		known := false
		for _, p := range Platforms {
			known = known || platform == p
		}
		digest, err := hex.DecodeString(asset.SHA256)
		if !known || err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != asset.SHA256 || asset.Size < 1 || asset.Size > MaxNativeBytes {
			return nil, fmt.Errorf("invalid native asset metadata")
		}
	}
	return &manifest, nil
}

// CheckNativeRelease binds the mutable locator to the release being prepared.
// Pinned digests, not the locator or remote checksums, authenticate downloads.
func CheckNativeRelease(root, repository, tag string) error {
	manifest, err := ReadNativeDownloads(root)
	if err != nil || manifest == nil {
		return err
	}
	if manifest.BaseURL != "https://github.com/"+repository+"/releases/download/"+tag {
		return fmt.Errorf("native download locator must match the planned release repository and tag")
	}
	return nil
}
