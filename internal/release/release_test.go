package release

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"setup-go-better/internal/distribution"
)

func write(t *testing.T, root, name string, data []byte) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0755); err != nil {
		t.Fatal(err)
	}
}
func testGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture Git failed: %v %s", err, b)
	}
	return strings.TrimSpace(string(b))
}
func copyFiles(t *testing.T, source, target string) {
	t.Helper()
	m, err := hashes(source)
	if err != nil {
		t.Fatal(err)
	}
	for name := range m {
		b, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		write(t, target, name, b)
	}
}

func fixture(t *testing.T) (*Runner, *Plan, string, map[string]string) {
	t.Helper()
	root := t.TempDir()
	testGit(t, root, "init", "--quiet", "--initial-branch=main")
	testGit(t, root, "config", "user.name", "Fixture")
	testGit(t, root, "config", "user.email", "fixture@example.test")
	testGit(t, root, "config", "core.autocrlf", "true")
	write(t, root, ".gitignore", []byte("/out/\n"))
	write(t, root, "source.go", []byte("package source\n"))
	testGit(t, root, "add", ".")
	testGit(t, root, "commit", "--quiet", "-m", "source")
	provenance, err := distribution.Source(root, distribution.Platforms, true)
	if err != nil {
		t.Fatal(err)
	}
	dist, assets := filepath.Join(root, "out", "dist"), filepath.Join(root, "out", "assets")
	for _, name := range []string{"action.yml", "launch.cjs", "post.cjs", "launcher.cjs", "launch.sh", "experimental/node/action.yml", "experimental/node/launch.cjs", "experimental/node/post.cjs", "experimental/composite/action.yml", "README.md"} {
		write(t, dist, name, []byte("fixture "+name+"\r\n"))
	}
	write(t, dist, ".gitattributes", []byte(distribution.CheckoutAttributes))
	for _, target := range distribution.Platforms {
		b := []byte("binary " + target)
		write(t, dist, distribution.Binary(target), b)
		write(t, assets, distribution.Asset(target), b)
	}
	if err = distribution.WriteProvenance(dist, provenance); err != nil {
		t.Fatal(err)
	}
	if err = distribution.WriteChecksums(dist); err != nil {
		t.Fatal(err)
	}
	files, err := hashes(dist)
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"bundled", "launcher"} {
		f, err := os.Create(filepath.Join(assets, variant+".zip"))
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		for _, name := range sortedKeys(files) {
			if variant == "launcher" && strings.HasPrefix(name, "bin/") {
				continue
			}
			mode := os.FileMode(0644)
			if strings.HasPrefix(name, "bin/") && !strings.HasSuffix(name, ".exe") {
				mode = 0755
			}
			h := zip.FileHeader{Name: name, Method: zip.Deflate}
			h.SetMode(mode)
			w, err := z.CreateHeader(&h)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(dist, filepath.FromSlash(name)))
			if _, err = w.Write(b); err != nil {
				t.Fatal(err)
			}
		}
		if err = z.Close(); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err = distribution.WriteChecksums(assets); err != nil {
		t.Fatal(err)
	}
	reproDist, reproAssets := filepath.Join(root, "out", "dist-repro"), filepath.Join(root, "out", "assets-repro")
	copyFiles(t, dist, reproDist)
	copyFiles(t, assets, reproAssets)
	r := New(root)
	refs := map[string]string{"refs/heads/main": provenance.Revision, "refs/heads/codex/action-preview": provenance.Revision}
	r.remote = func(context.Context, Config) (map[string]string, error) { return refs, nil }
	c := Config{Repository: "fixture/action", Remote: "origin", SourceRef: "refs/heads/main", PreviewRef: "refs/heads/codex/action-preview", Tag: "v0.0.0-preview.20261111", Dist: dist, Assets: assets, ReproDist: reproDist, ReproAssets: reproAssets, Notes: "Fixture prerelease"}
	p, err := r.CreatePlan(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "out", "plan.json")
	if err = Save(path, p); err != nil {
		t.Fatal(err)
	}
	return r, p, path, refs
}

func TestPlanPrepareRawBytesAndResume(t *testing.T) {
	r, p, path, refs := fixture(t)
	ctx := context.Background()
	if p.Distribution != "" || testGit(t, r.Root, "tag") != "" {
		t.Fatal("planning changed refs")
	}
	if err := r.Prepare(ctx, p, path); err != nil {
		t.Fatal(err)
	}
	first := p.Distribution
	if err := r.Prepare(ctx, p, path); err != nil || p.Distribution != first {
		t.Fatalf("local prepare did not resume: %v", err)
	}
	if err := r.verifyCommit(ctx, p); err != nil {
		t.Fatal(err)
	}
	if done, err := r.CheckPush(ctx, p); err != nil || done {
		t.Fatalf("initial state: %t %v", done, err)
	}
	refs[p.Config.SourceRef] = p.Source
	refs[p.Config.PreviewRef] = p.Distribution
	refs["refs/tags/"+p.Config.Tag] = p.TagObject
	if done, err := r.CheckPush(ctx, p); err != nil || !done {
		t.Fatalf("pushed state: %t %v", done, err)
	}
	refs[p.Config.PreviewRef] = strings.Repeat("f", 40)
	if _, err := r.CheckPush(ctx, p); err == nil {
		t.Fatal("changed remote refs accepted")
	}
	if loaded, err := Load(path); err != nil || loaded.Distribution != first {
		t.Fatalf("saved plan did not survive resume: %v", err)
	}
	if err := Save(filepath.Join(r.Root, "tracked-plan.json"), p); err == nil {
		t.Fatal("nonignored plan accepted")
	}
	write(t, p.Config.Dist, "README.md", []byte("tamper"))
	if err := r.Validate(ctx, p); err == nil {
		t.Fatal("tampered package accepted")
	}
}

func TestPlanRejectsChangedReproAndUnsafeConfig(t *testing.T) {
	r, p, _, _ := fixture(t)
	ctx := context.Background()
	for _, tag := range []string{"v1", "--delete", "v0.0.0-preview.20261111\n"} {
		c := p.Config
		c.Tag = tag
		if _, err := r.CreatePlan(ctx, c); err == nil {
			t.Fatal("unsafe or stable tag accepted")
		}
	}
	write(t, p.Config.ReproDist, "README.md", []byte("different"))
	if _, err := r.CreatePlan(ctx, p.Config); err == nil {
		t.Fatal("changed reproducibility input accepted")
	}
}

func TestAtomicPushLeasesUseOnlyTemporaryBareRemote(t *testing.T) {
	r, p, path, _ := fixture(t)
	ctx := context.Background()
	if err := r.Prepare(ctx, p, path); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "remote.git")
	testGit(t, r.Root, "init", "--bare", "--quiet", bare)
	testGit(t, r.Root, "remote", "add", "origin", bare)
	testGit(t, r.Root, "push", "--quiet", "origin", p.Source+":"+p.Config.SourceRef, p.ExpectedPreview+":"+p.Config.PreviewRef)
	tree := testGit(t, r.Root, "rev-parse", p.Source+"^{tree}")
	other := testGit(t, r.Root, "commit-tree", tree, "-p", p.Source, "-m", "competing source")
	testGit(t, r.Root, "push", "--quiet", "origin", other+":"+p.Config.SourceRef)
	if err := r.pushPlan(ctx, p); err == nil {
		t.Fatal("stale source lease accepted")
	}
	if got := testGit(t, r.Root, "--git-dir="+bare, "rev-parse", p.Config.PreviewRef); got != p.ExpectedPreview {
		t.Fatal("failed atomic push partially updated preview")
	}
	cmd := exec.Command("git", "--git-dir="+bare, "show-ref", "--verify", "refs/tags/"+p.Config.Tag)
	if err := cmd.Run(); err == nil {
		t.Fatal("failed atomic push partially created tag")
	}
	testGit(t, r.Root, "--git-dir="+bare, "update-ref", p.Config.SourceRef, p.ExpectedSource, other)
	if err := r.pushPlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]string{p.Config.SourceRef: p.Source, p.Config.PreviewRef: p.Distribution, "refs/tags/" + p.Config.Tag: p.TagObject} {
		if got := testGit(t, r.Root, "--git-dir="+bare, "rev-parse", ref); got != want {
			t.Fatalf("atomic pushed ref %s: %s", ref, got)
		}
	}
}

type apiOptions struct{ badDigest, lostCreate, lostUpload, finalNotFound, assetsNotFound bool }

func apiFixture(t *testing.T, options apiOptions) (*github, *Plan, *int, *bool) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "one.bin", []byte("payload"))
	write(t, root, "two.bin", []byte("second"))
	m, err := hashes(root)
	if err != nil {
		t.Fatal(err)
	}
	p := &Plan{Config: Config{Repository: "fixture/action", Tag: "v0.0.0-preview.20261111", Assets: root, Notes: "Reviewed notes"}, Source: strings.Repeat("a", 40), Distribution: strings.Repeat("b", 40), Assets: m}
	var release *remoteRelease
	uploadCount := 0
	published := false
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("authentication missing")
		}
		w.Header().Set("Content-Type", "application/json")
		base := "/repos/fixture/action/releases"
		switch {
		case r.Method == "GET" && r.URL.Path == base:
			items := []remoteRelease{}
			if release != nil {
				items = append(items, *release)
			}
			json.NewEncoder(w).Encode(items)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, base+"/tags/"):
			if release == nil || release.Draft || options.finalNotFound {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(release)
		case r.Method == "POST" && r.URL.Path == base:
			if release != nil {
				t.Error("duplicate create")
			}
			release = &remoteRelease{ID: 1, Tag: p.Config.Tag, Target: p.Distribution, Draft: true, Prerelease: true, Body: releaseBody(p), UploadURL: server.URL + "/upload{?name}", URL: server.URL + "/published"}
			if options.lostCreate {
				options.lostCreate = false
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(release)
		case r.Method == "GET" && r.URL.Path == base+"/1/assets":
			if options.assetsNotFound {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(release.Assets)
		case r.Method == "POST" && r.URL.Path == "/upload":
			b, _ := io.ReadAll(r.Body)
			sum := sha256.Sum256(b)
			a := asset{Name: r.URL.Query().Get("name"), Size: int64(len(b)), Digest: "sha256:" + hex.EncodeToString(sum[:]), State: "uploaded"}
			if options.badDigest {
				a.Digest = "sha256:wrong"
			}
			release.Assets = append(release.Assets, a)
			uploadCount++
			if options.lostUpload {
				options.lostUpload = false
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(a)
		case r.Method == "PATCH" && r.URL.Path == base+"/1":
			if len(release.Assets) != len(p.Assets) {
				t.Error("published before all uploads")
			}
			release.Draft = false
			published = true
			json.NewEncoder(w).Encode(release)
		default:
			t.Errorf("unexpected API operation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	return &github{token: "private-token", base: server.URL, client: server.Client(), localTest: true}, p, &uploadCount, &published
}

func TestDraftUploadVerificationAndPublishedResume(t *testing.T) {
	g, p, count, published := apiFixture(t, apiOptions{})
	ctx := context.Background()
	if _, err := g.publish(ctx, p); err != nil {
		t.Fatal(err)
	}
	if *count != 2 || !*published {
		t.Fatal("not all assets published")
	}
	if _, err := g.publish(ctx, p); err != nil || *count != 2 {
		t.Fatalf("published resume changed assets: %v", err)
	}
	p.Config.Notes = "changed notes"
	if _, err := g.publish(ctx, p); err == nil {
		t.Fatal("published release overwrite accepted")
	}
}

func TestMismatchedDigestNeverPublishes(t *testing.T) {
	g, p, _, published := apiFixture(t, apiOptions{badDigest: true})
	if _, err := g.publish(context.Background(), p); err == nil || *published {
		t.Fatalf("bad digest published: %v", err)
	}
}

func TestDraftDiscoveryResumesAmbiguousCreateAndUpload(t *testing.T) {
	for _, options := range []apiOptions{{lostCreate: true}, {lostUpload: true}} {
		g, p, count, published := apiFixture(t, options)
		if _, err := g.publish(context.Background(), p); err == nil || *published {
			t.Fatal("ambiguous write was blindly retried")
		}
		if _, err := g.publish(context.Background(), p); err != nil || !*published || *count != 2 {
			t.Fatalf("existing draft was not resumed without duplicate assets: %v", err)
		}
	}
}

func TestMissingVerificationReadsNeverReportSuccess(t *testing.T) {
	for _, options := range []apiOptions{{finalNotFound: true}, {assetsNotFound: true}} {
		g, p, _, _ := apiFixture(t, options)
		if _, err := g.publish(context.Background(), p); err == nil {
			t.Fatal("HTTP404 accepted as successful verification")
		}
	}
}

func TestMismatchedOrMultiplePushURLsRejectedBeforeNetwork(t *testing.T) {
	r, p, _, _ := fixture(t)
	r.remote = nil
	testGit(t, r.Root, "remote", "add", "origin", "https://github.com/fixture/action.git")
	testGit(t, r.Root, "config", "remote.origin.pushurl", "https://github.com/upstream/other.git")
	if _, err := r.refs(context.Background(), p.Config); err == nil || !strings.Contains(err.Error(), "push URL") {
		t.Fatal("mismatched push destination accepted", err)
	}
	testGit(t, r.Root, "config", "remote.origin.pushurl", "https://github.com/fixture/action.git")
	testGit(t, r.Root, "config", "--add", "remote.origin.pushurl", "https://github.com/fixture/action.git")
	if _, err := r.refs(context.Background(), p.Config); err == nil || !strings.Contains(err.Error(), "push URL") {
		t.Fatal("multiple push destinations accepted", err)
	}
}

func TestDraftLookupPaginationAndDuplicateRefusal(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			items := []remoteRelease{}
			if r.URL.Query().Get("page") == "1" {
				for range 100 {
					items = append(items, remoteRelease{Tag: "other"})
				}
				if duplicate {
					items[0].Tag = "wanted"
				}
			} else {
				items = append(items, remoteRelease{ID: 2, Tag: "wanted", Draft: true})
			}
			json.NewEncoder(w).Encode(items)
		}))
		g := github{token: "private-token", client: server.Client(), localTest: true}
		found, err := g.findRelease(context.Background(), server.URL, "wanted")
		server.Close()
		if duplicate && err == nil || !duplicate && (err != nil || found == nil || found.ID != 2) {
			t.Fatalf("pagination/duplicate result: %+v %v", found, err)
		}
	}
}

func TestHTTPBoundarySanitizesRedirectsAndTransportFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://secret.test/?token=private-token", 302)
	}))
	defer server.Close()
	g := github{token: "private-token", client: server.Client(), localTest: true}
	_, err := g.request(context.Background(), "POST", server.URL, nil, 0, "", nil)
	if err == nil || strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "secret.test") {
		t.Fatal("redirect leaked or followed", err)
	}
	g.localTest = false
	if _, err = g.request(context.Background(), "GET", "https://evil.test", nil, 0, "", nil); err == nil {
		t.Fatal("foreign API host accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g.localTest = true
	g.client = &http.Client{Timeout: time.Second}
	if _, err = g.request(ctx, "POST", server.URL, nil, 0, "", nil); err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("cancelled request leaked", err)
	}
}
