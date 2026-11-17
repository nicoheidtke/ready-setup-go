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

	"ready-setup-go/internal/distribution"
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
	refs := map[string]string{"refs/heads/main": provenance.Revision, "refs/heads/dist": provenance.Revision}
	r.remote = func(context.Context, Config) (map[string]string, error) { return refs, nil }
	c := Config{Repository: "fixture/action", Remote: "origin", SourceRef: "refs/heads/main", DistributionRef: "refs/heads/dist", Tag: "v1.0.0", Dist: dist, Assets: assets, ReproDist: reproDist, ReproAssets: reproAssets, Notes: "Fixture release"}
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
	refs[p.Config.DistributionRef] = p.Distribution
	refs["refs/tags/"+p.Config.Tag] = p.TagObject
	if done, err := r.CheckPush(ctx, p); err != nil || !done {
		t.Fatalf("pushed state: %t %v", done, err)
	}
	refs[p.Config.DistributionRef] = strings.Repeat("f", 40)
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
	for _, tag := range []string{"v1", "--delete", "v1.0.0\n"} {
		c := p.Config
		c.Tag = tag
		if _, err := r.CreatePlan(ctx, c); err == nil {
			t.Fatal("unsafe tag accepted")
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
	testGit(t, r.Root, "push", "--quiet", "origin", p.Source+":"+p.Config.SourceRef, p.ExpectedDistribution+":"+p.Config.DistributionRef)
	tree := testGit(t, r.Root, "rev-parse", p.Source+"^{tree}")
	other := testGit(t, r.Root, "commit-tree", tree, "-p", p.Source, "-m", "competing source")
	testGit(t, r.Root, "push", "--quiet", "origin", other+":"+p.Config.DistributionRef)
	if err := r.pushPlan(ctx, p); err == nil {
		t.Fatal("stale distribution lease accepted")
	}
	if got := testGit(t, r.Root, "--git-dir="+bare, "rev-parse", p.Config.DistributionRef); got != other {
		t.Fatal("failed atomic push changed the competing distribution")
	}
	cmd := exec.Command("git", "--git-dir="+bare, "show-ref", "--verify", "refs/tags/"+p.Config.Tag)
	if err := cmd.Run(); err == nil {
		t.Fatal("failed atomic push partially created tag")
	}
	testGit(t, r.Root, "--git-dir="+bare, "update-ref", p.Config.DistributionRef, p.ExpectedDistribution, other)
	if err := r.pushPlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]string{p.Config.SourceRef: p.Source, p.Config.DistributionRef: p.Distribution, "refs/tags/" + p.Config.Tag: p.TagObject} {
		if got := testGit(t, r.Root, "--git-dir="+bare, "rev-parse", ref); got != want {
			t.Fatalf("atomic pushed ref %s: %s", ref, got)
		}
	}
}

type apiOptions struct{ badDigest, lostCreate, lostUpload, finalNotFound, assetsNotFound, wrongClassification bool }

func apiFixture(t *testing.T, options apiOptions) (*github, *Plan, *int, *bool) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "one.bin", []byte("payload"))
	write(t, root, "two.bin", []byte("second"))
	m, err := hashes(root)
	if err != nil {
		t.Fatal(err)
	}
	p := &Plan{Config: Config{Repository: "fixture/action", Tag: "v1.0.0", Assets: root, Notes: "Reviewed notes"}, Source: strings.Repeat("a", 40), Distribution: strings.Repeat("b", 40), Assets: m}
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
			release = &remoteRelease{ID: 1, Tag: p.Config.Tag, Target: p.Distribution, Draft: true, Prerelease: false, Body: releaseBody(p), UploadURL: server.URL + "/upload{?name}", URL: server.URL + "/published"}
			if options.wrongClassification {
				release.Prerelease = !release.Prerelease
			}
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
			var request struct {
				Draft      bool   `json:"draft"`
				Prerelease bool   `json:"prerelease"`
				Latest     string `json:"make_latest"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			wantPreview := false
			wantLatest := "false"
			if p.MakeLatest {
				wantLatest = "true"
			}
			if request.Draft || request.Prerelease != wantPreview || request.Latest != wantLatest {
				t.Errorf("incorrect publication options: %+v", request)
			}
			release.Draft = false
			release.Prerelease = request.Prerelease
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

// A thin candidate retains all native assets but commits only the pinned manifest
// and runtime files. Exercise the actual planning/prepare paths, not just parsing.
func TestLightweightReleasePlanAndPrepare(t *testing.T) {
	r, p, planPath, _ := fixture(t)
	base := "https://github.com/" + p.Config.Repository + "/releases/download/" + p.Config.Tag
	for _, pair := range [][2]string{{p.Config.Dist, p.Config.Assets}, {p.Config.ReproDist, p.Config.ReproAssets}} {
		if err := distribution.WriteNativeDownloads(pair[0], pair[0], base, distribution.Platforms); err != nil {
			t.Fatal(err)
		}
		for _, platform := range distribution.Platforms {
			if err := os.Remove(filepath.Join(pair[0], filepath.FromSlash(distribution.Binary(platform)))); err != nil {
				t.Fatal(err)
			}
		}
		write(t, pair[0], "native-download.cjs", []byte("bootstrap"))
		if err := distribution.WriteChecksums(pair[0]); err != nil {
			t.Fatal(err)
		}
		files, err := hashes(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, variant := range []string{"bundled", "launcher"} {
			f, err := os.Create(filepath.Join(pair[1], variant+".zip"))
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			for _, name := range sortedKeys(files) {
				h := zip.FileHeader{Name: name, Method: zip.Deflate}
				h.SetMode(0644)
				w, err := z.CreateHeader(&h)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(filepath.Join(pair[0], filepath.FromSlash(name)))
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.Write(data); err != nil {
					t.Fatal(err)
				}
			}
			if err := z.Close(); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if err := distribution.WriteChecksums(pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	planned, err := r.CreatePlan(context.Background(), p.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Prepare(context.Background(), planned, planPath); err != nil {
		t.Fatal(err)
	}
	if err := r.verifyCommit(context.Background(), planned); err != nil {
		t.Fatal(err)
	}
	wrong := p.Config
	wrong.Tag = "v1.0.1"
	if _, err := r.CreatePlan(context.Background(), wrong); err == nil || !strings.Contains(err.Error(), "locator") {
		t.Fatalf("wrong release tag accepted: %v", err)
	}
	planned.Config.Repository = "other/action"
	if err := r.Validate(context.Background(), planned); err == nil || !strings.Contains(err.Error(), "locator") {
		t.Fatalf("mismatched release accepted: %v", err)
	}
}

func TestReleaseVersions(t *testing.T) {
	r, p, _, _ := fixture(t)
	for _, tag := range []string{"v1.0.0", "v0.1.0", "v12.34.567", "v2.0.0"} {
		c := p.Config
		c.Tag = tag
		if _, err := r.CreatePlan(context.Background(), c); err != nil {
			t.Errorf("%s: %v", tag, err)
		}
	}
	for _, tag := range []string{"v1", "v1.2", "1.2.3", "v01.2.3", "v1.02.3", "v1.2.03", "v0.0.0-preview.20261115", "v1.2.3-rc.1", "v1.2.3+build", "v1.2.3\n"} {
		c := p.Config
		c.Tag = tag
		if _, err := r.CreatePlan(context.Background(), c); err == nil {
			t.Errorf("accepted %q", tag)
		}
	}
}

func TestFirstReleaseAtomicPushAndResume(t *testing.T) {
	r, initial, path, refs := fixture(t)
	delete(refs, initial.Config.DistributionRef)
	c := initial.Config
	c.Tag = "v1.0.0"
	ctx := context.Background()
	p, err := r.CreatePlan(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if p.ExpectedDistribution != "" {
		t.Fatal("first release has a parent")
	}
	if err = r.Prepare(ctx, p, path); err != nil {
		t.Fatal(err)
	}
	testGit(t, r.Root, "merge-base", "--is-ancestor", p.Source, p.Distribution)
	if done, err := r.CheckPush(ctx, p); err != nil || done {
		t.Fatalf("first preflight: %t %v", done, err)
	}
	bare := filepath.Join(t.TempDir(), "remote.git")
	testGit(t, r.Root, "init", "--bare", "--quiet", bare)
	testGit(t, r.Root, "remote", "add", "origin", bare)
	testGit(t, r.Root, "push", "--quiet", "origin", p.Source+":"+p.Config.SourceRef)
	// A competing initial branch must invalidate the absent-ref lease atomically.
	testGit(t, r.Root, "--git-dir="+bare, "update-ref", p.Config.DistributionRef, p.Source)
	if err = r.pushPlan(ctx, p); err == nil {
		t.Fatal("competing first branch accepted")
	}
	if got := testGit(t, r.Root, "--git-dir="+bare, "rev-parse", p.Config.DistributionRef); got != p.Source {
		t.Fatal("competing branch changed")
	}
	cmd := exec.Command("git", "--git-dir="+bare, "show-ref", "--verify", "refs/tags/"+p.Config.Tag)
	if err := cmd.Run(); err == nil {
		t.Fatal("failed push created tag")
	}
	testGit(t, r.Root, "--git-dir="+bare, "update-ref", "-d", p.Config.DistributionRef, p.Source)
	if err = r.pushPlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	refs[p.Config.DistributionRef] = p.Distribution
	refs["refs/tags/"+p.Config.Tag] = p.TagObject
	if done, err := r.CheckPush(ctx, p); err != nil || !done {
		t.Fatalf("resume: %t %v", done, err)
	}
	if err = r.Prepare(ctx, p, path); err != nil {
		t.Fatal(err)
	}
}

func TestStablePublicationAndClassificationConflict(t *testing.T) {
	g, p, count, published := apiFixture(t, apiOptions{})
	p.Config.Tag = "v1.0.0"
	p.MakeLatest = true
	if _, err := g.publish(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if !*published || *count != 2 {
		t.Fatal("stable release not published")
	}
	if _, err := g.publish(context.Background(), p); err != nil || *count != 2 {
		t.Fatalf("stable resume: %v", err)
	}
	g, p, _, published = apiFixture(t, apiOptions{wrongClassification: true})
	p.Config.Tag = "v1.0.0"
	if _, err := g.publish(context.Background(), p); err == nil || *published {
		t.Fatal("classification conflict accepted")
	}
}

func TestAliasPlanAndPromotion(t *testing.T) {
	r, initial, path, refs := fixture(t)
	c := initial.Config
	c.Tag = "v1.2.3"
	c.UpdateAliases = true
	ctx := context.Background()
	refs["refs/tags/v1.1.0"] = initial.Source
	p, err := r.CreatePlan(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Prepare(ctx, p, path); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "remote.git")
	testGit(t, r.Root, "init", "--bare", "--quiet", bare)
	testGit(t, r.Root, "remote", "add", "origin", bare)
	testGit(t, r.Root, "push", "--quiet", "origin", p.Distribution+":"+c.DistributionRef)
	// Simulate the remote reader while exercising actual guarded Git pushes.
	raceNext := false
	r.remote = func(context.Context, Config) (map[string]string, error) {
		result := map[string]string{}
		output := testGit(t, r.Root, "ls-remote", "--refs", "origin")
		for _, line := range strings.Split(output, "\n") {
			fields := strings.Fields(line)
			result[fields[1]] = fields[0]
		}
		if raceNext {
			raceNext = false
			testGit(t, r.Root, "--git-dir="+bare, "update-ref", "refs/tags/v1", p.Source)
		}
		return result, nil
	}
	// A race must not create either alias partially.
	testGit(t, r.Root, "push", "--quiet", "origin", p.Source+":refs/tags/v1")
	if err = r.promoteAliases(ctx, p); err == nil {
		t.Fatal("alias conflict accepted")
	}
	if got := testGit(t, r.Root, "--git-dir="+bare, "tag", "-l", "v1.2"); got != "" {
		t.Fatal("partial alias created")
	}
	testGit(t, r.Root, "--git-dir="+bare, "update-ref", "-d", "refs/tags/v1", p.Source)
	raceNext = true
	if err = r.promoteAliases(ctx, p); err == nil {
		t.Fatal("stale alias push lease accepted")
	}
	if got := testGit(t, r.Root, "--git-dir="+bare, "tag", "-l", "v1.2"); got != "" {
		t.Fatal("failed atomic push partially created alias")
	}
	testGit(t, r.Root, "--git-dir="+bare, "update-ref", "-d", "refs/tags/v1", p.Source)
	if err = r.promoteAliases(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err = r.promoteAliases(ctx, p); err != nil {
		t.Fatal("alias resume:", err)
	}
	for _, alias := range releaseAliases(c.Tag) {
		if got := testGit(t, r.Root, "--git-dir="+bare, "rev-parse", alias); got != p.Distribution {
			t.Fatal("wrong alias target", got)
		}
	}
}

func TestIndependentPublicationPolicies(t *testing.T) {
	for _, tc := range []struct {
		name, tag, latest     string
		aliases               bool
		versions, wantAliases []string
		wantLatest            bool
	}{
		{"first", "v1.0.0", "auto", true, nil, []string{"refs/tags/v1", "refs/tags/v1.0"}, true},
		{"backport", "v1.4.2", "auto", true, []string{"v1.4.1", "v2.0.0"}, []string{"refs/tags/v1", "refs/tags/v1.4"}, false},
		{"old-minor", "v1.4.2", "auto", true, []string{"v1.4.1", "v1.5.0"}, []string{"refs/tags/v1.4"}, false},
		{"older-patch", "v1.4.0", "auto", true, []string{"v1.4.1"}, nil, false},
		{"fixed-only", "v1.4.2", "false", false, []string{"v2.0.0"}, nil, false},
		{"latest-only", "v3.0.0", "true", false, []string{"v2.0.0"}, nil, true},
		{"explicit-latest", "v1.0.0", "true", true, []string{"v2.0.0"}, []string{"refs/tags/v1", "refs/tags/v1.0"}, true},
		{"no-latest", "v3.0.0", "false", true, nil, []string{"refs/tags/v3", "refs/tags/v3.0"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refs := map[string]string{}
			for _, tag := range tc.versions {
				refs["refs/tags/"+tag] = strings.Repeat("a", 40)
			}
			aliases, latest := publicationPolicy(Config{Tag: tc.tag, UpdateAliases: tc.aliases, MakeLatest: tc.latest}, refs)
			if latest != tc.wantLatest || strings.Join(sortedKeys(aliases), ",") != strings.Join(tc.wantAliases, ",") {
				t.Fatalf("aliases=%v latest=%t", aliases, latest)
			}
		})
	}
	refs := map[string]string{"refs/tags/v1.3.0": "", "refs/tags/v1.4.1": "", "refs/tags/v1.5.0": "", "refs/tags/v2.0.0": ""}
	if got := PreviousVersion("v1.4.2", refs); got != "v1.4.1" {
		t.Fatal(got)
	}
	for _, pair := range [][2]string{{"v1.9.9", "v1.10.0"}, {"v2.0.0", "v10.0.0"}, {"v1.0.9", "v1.0.10"}} {
		if !versionLess(pair[0], pair[1]) || versionLess(pair[1], pair[0]) {
			t.Fatal(pair)
		}
	}
}

func TestPlanRequiresMergedSource(t *testing.T) {
	r, p, _, refs := fixture(t)
	refs[p.Config.SourceRef] = strings.Repeat("f", 40)
	if _, err := r.CreatePlan(context.Background(), p.Config); err == nil || !strings.Contains(err.Error(), "belong to the remote source branch") {
		t.Fatalf("unmerged source accepted: %v", err)
	}
}

func TestHistoricalPlanNeverPushesSource(t *testing.T) {
	r, initial, path, refs := fixture(t)
	ctx := context.Background()
	tree := testGit(t, r.Root, "rev-parse", initial.Source+"^{tree}")
	newer := testGit(t, r.Root, "commit-tree", tree, "-p", initial.Source, "-m", "newer main")
	refs[initial.Config.SourceRef] = newer
	testGit(t, r.Root, "checkout", "--detach", initial.Source)
	c := initial.Config
	c.SourceAnchor = ""
	p, err := r.CreatePlan(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if p.Source != initial.Source || p.ExpectedSource != newer {
		t.Fatal("source snapshot changed")
	}
	if err = r.Prepare(ctx, p, path); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(t.TempDir(), "remote.git")
	testGit(t, r.Root, "init", "--bare", "--quiet", bare)
	testGit(t, r.Root, "remote", "add", "origin", bare)
	testGit(t, r.Root, "push", "--quiet", "origin", newer+":"+p.Config.SourceRef, p.ExpectedDistribution+":"+p.Config.DistributionRef)
	if err = r.pushPlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	if got := testGit(t, r.Root, "--git-dir="+bare, "rev-parse", p.Config.SourceRef); got != newer {
		t.Fatal("historical release moved source branch", got)
	}
	refs[p.Config.DistributionRef], refs["refs/tags/"+p.Config.Tag] = p.Distribution, p.TagObject
	refs[p.Config.SourceRef] = testGit(t, r.Root, "commit-tree", tree, "-p", newer, "-m", "later main")
	if done, err := r.CheckPush(ctx, p); err != nil || !done {
		t.Fatalf("source advance retargeted saved plan: %t %v", done, err)
	}
}

func TestSourceResolutionRejectsUnrelatedAndMalformedCommits(t *testing.T) {
	r, p, _, refs := fixture(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	testGit(t, r.Root, "init", "--bare", "--quiet", bare)
	testGit(t, r.Root, "remote", "add", "origin", bare)
	tree := testGit(t, r.Root, "rev-parse", p.Source+"^{tree}")
	newer := testGit(t, r.Root, "commit-tree", tree, "-p", p.Source, "-m", "newer main")
	testGit(t, r.Root, "push", "--quiet", "origin", newer+":"+p.Config.SourceRef)
	refs[p.Config.SourceRef] = newer
	selected, err := r.ResolveSource(context.Background(), "fixture/action", "origin", "main", p.Source, "v1.0.1")
	if err != nil || selected.Source != p.Source || selected.Anchor != newer {
		t.Fatalf("history selection: %+v %v", selected, err)
	}
	tip, err := r.ResolveSource(context.Background(), "fixture/action", "origin", "main", "", "v1.0.1")
	if err != nil || tip.Source != newer {
		t.Fatalf("branch tip selection: %+v %v", tip, err)
	}
	unrelated := testGit(t, r.Root, "commit-tree", tree, "-m", "unrelated")
	for _, revision := range []string{unrelated, "HEAD~1", p.Source[:10], "--help"} {
		if _, err := r.ResolveSource(context.Background(), "fixture/action", "origin", "main", revision, "v1.0.1"); err == nil {
			t.Fatal("invalid source accepted", revision)
		}
	}
	for _, branch := range []string{"main~1", "main.lock", "../main", "missing"} {
		if _, err := r.ResolveSource(context.Background(), "fixture/action", "origin", branch, "", "v1.0.1"); err == nil {
			t.Fatal("invalid branch accepted", branch)
		}
	}
}

func TestTrustedRequestBindsAllPublicationOptions(t *testing.T) {
	_, p, _, _ := fixture(t)
	request := Request{Config: p.Config, Source: p.Source}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "request.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = CheckRequest(path, p); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Config.DistributionRef = "refs/heads/main" },
		func(p *Plan) { p.Config.Repository = "other/repository" },
		func(p *Plan) { p.Config.Notes = "changed notes" },
		func(p *Plan) { p.Config.MakeLatest = "true" },
		func(p *Plan) { p.Config.UpdateAliases = !p.Config.UpdateAliases },
		func(p *Plan) { p.Config.Dist = filepath.Dir(p.Config.Dist) },
		func(p *Plan) { p.Source = strings.Repeat("f", 40) },
	} {
		altered := *p
		mutate(&altered)
		if err = CheckRequest(path, &altered); err == nil {
			t.Fatal("untrusted plan change accepted")
		}
	}
}

func TestChangedVersionPolicyInvalidatesPreparedPlan(t *testing.T) {
	r, initial, path, refs := fixture(t)
	c := initial.Config
	c.Tag = "v1.4.2"
	c.UpdateAliases = true
	c.MakeLatest = "auto"
	refs["refs/tags/v2.0.0"] = initial.Source
	p, err := r.CreatePlan(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if p.MakeLatest || len(p.ExpectedAliases) != 2 {
		t.Fatal("backport decisions not frozen")
	}
	if err = r.Prepare(context.Background(), p, path); err != nil {
		t.Fatal(err)
	}
	refs["refs/tags/v1.5.0"] = initial.Source
	if _, err = r.CheckPush(context.Background(), p); err == nil || !strings.Contains(err.Error(), "policy changed") {
		t.Fatalf("stale alias policy accepted: %v", err)
	}
}

func TestPublicationLatestDoesNotFollowAliasSwitch(t *testing.T) {
	for _, latest := range []bool{false, true} {
		g, p, _, _ := apiFixture(t, apiOptions{})
		p.Config.UpdateAliases = !latest
		p.MakeLatest = latest
		if _, err := g.publish(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecoveryBundleRestoresPreparedGitObjects(t *testing.T) {
	r, p, path, _ := fixture(t)
	ctx := context.Background()
	if err := r.Prepare(ctx, p, path); err != nil {
		t.Fatal(err)
	}
	testGit(t, r.Root, "update-ref", "refs/ready-setup-go/source-anchor", p.ExpectedSource)
	bundle := filepath.Join(r.Root, "out", "recovery.bundle")
	testGit(t, r.Root, "bundle", "create", bundle, "HEAD", "refs/ready-setup-go/source-anchor", "refs/tags/"+p.Config.Tag)
	testGit(t, r.Root, "tag", "-d", p.Config.Tag)
	testGit(t, r.Root, "fetch", bundle, "+refs/tags/*:refs/tags/*", "+refs/ready-setup-go/source-anchor:refs/ready-setup-go/source-anchor")
	if done, err := r.CheckPush(ctx, p); err != nil || done {
		t.Fatalf("restored plan: %t %v", done, err)
	}
	if got := testGit(t, r.Root, "rev-parse", "refs/tags/"+p.Config.Tag); got != p.TagObject {
		t.Fatal("restoration changed annotated tag")
	}
}
