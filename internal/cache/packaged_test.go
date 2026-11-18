package cache

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"ready-setup-go/internal/distribution"
)

// TestPackagedLifecycle is opt-in: it consumes a prebuilt distribution and an
// already installed toolchain, never compiling the action or fetching Go. This
// checks the actual subprocess/file-protocol boundary which unit APIs cannot.
func TestPackagedLifecycle(t *testing.T) {
	dist := os.Getenv("READY_SETUP_GO_TEST_DIST")
	if dist == "" {
		t.Skip("set READY_SETUP_GO_TEST_DIST to test an existing native distribution")
	}
	var err error
	dist, err = filepath.Abs(dist)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	runnerTemp := os.Getenv("READY_SETUP_GO_TEST_RUNNER_TEMP")
	if runnerTemp == "" {
		runnerTemp = filepath.Join(repo, ".cache", "installer smoke with spaces")
	}
	runnerTemp, err = filepath.Abs(runnerTemp)
	if err != nil {
		t.Fatal(err)
	}
	version := os.Getenv("READY_SETUP_GO_TEST_VERSION")
	if version == "" {
		version = "1.26.4"
	}
	storage := os.Getenv("READY_SETUP_GO_TEST_STORAGE")
	if storage == "" {
		storage = "archive"
	}
	compression := os.Getenv("READY_SETUP_GO_TEST_COMPRESSION")
	if compression == "" {
		compression = "gzip"
	}
	gzipLevel := os.Getenv("READY_SETUP_GO_TEST_GZIP_LEVEL")
	if gzipLevel == "" {
		gzipLevel = "default"
	}
	config := DefaultConfig()
	config.Storage, config.Compression = storage, compression
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	toolchain := filepath.Join(runnerTemp, "ready-setup-go", "toolchains", version, runtime.GOOS+"-"+runtime.GOARCH)
	goName := "go"
	if runtime.GOOS == "windows" {
		goName += ".exe"
	}
	goBinary := filepath.Join(toolchain, "bin", goName)
	if _, err = os.Stat(goBinary); err != nil {
		t.Fatalf("preinstall toolchain before opt-in test: %v", err)
	}
	node := os.Getenv("READY_SETUP_GO_TEST_NODE")
	if node == "" {
		node = "node"
	}
	// Keep dependency mutations private and the workspace path stable through history.
	workspace := t.TempDir()
	for _, name := range []string{"go.mod", "main.go", "main_test.go"} {
		data, err := os.ReadFile(filepath.Join(repo, "testdata", "smoke", name))
		if err != nil {
			t.Fatal(err)
		}
		put(t, filepath.Join(workspace, name), string(data))
	}
	nativeManifest, err := distribution.ReadNativeDownloads(dist)
	if err != nil {
		t.Fatal(err)
	}
	nativeShim, nativeCount := "", ""
	if nativeManifest != nil {
		assets := os.Getenv("READY_SETUP_GO_TEST_NATIVE_ASSETS")
		if assets == "" {
			t.Fatal("download distributions require READY_SETUP_GO_TEST_NATIVE_ASSETS for the local fixture")
		}
		assets, err = filepath.Abs(assets)
		if err != nil {
			t.Fatal(err)
		}
		platform := runtime.GOOS + "/" + runtime.GOARCH
		asset := filepath.Join(assets, distribution.Asset(platform))
		shimDir := t.TempDir()
		nativeShim, nativeCount = filepath.Join(shimDir, "native-fixture.cjs"), filepath.Join(shimDir, "downloads")
		// Inject the transport only in the Node subprocess. The production bootstrap
		// still validates its pinned URL/size/digest and runs the packaged executable.
		// Isolate its reuse directory without copying the already installed compiler.
		quoted := func(v string) string {
			b, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			return string(b)
		}
		shim := fmt.Sprintf(`const fs=require('node:fs');const {Readable}=require('node:stream');
const bootstrap=require(%s);const original=bootstrap.ensureNative;
bootstrap.ensureNative=(root,platform,options)=>original(root,platform,{...options,env:{...process.env,RUNNER_TEMP:%s}});
globalThis.fetch=async(url,options)=>{if(url!==%s || Object.keys(options.headers).some(k=>k.toLowerCase()==='authorization'))throw Error('unexpected native request');fs.appendFileSync(%s,'download\\n');return new Response(Readable.toWeb(fs.createReadStream(%s)),{headers:{'content-length':String(fs.statSync(%s).size)}})};
`, quoted(filepath.Join(dist, "native-download.cjs")), quoted(filepath.Join(shimDir, "native temp")), quoted(nativeManifest.BaseURL+"/"+distribution.Asset(platform)), quoted(nativeCount), quoted(asset), quoted(asset))
		put(t, nativeShim, shim)
	}

	type remoteEntry struct {
		key, version, id string
		data             []byte
		finalized        bool
	}
	var mu sync.Mutex
	var stored []*remoteEntry
	var rangeRequests int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/blob/") {
			if r.Header.Get("Authorization") != "" {
				t.Error("runtime token reached blob endpoint")
			}
			id := strings.TrimPrefix(r.URL.Path, "/blob/")
			for _, entry := range stored {
				if entry.id == id {
					switch r.Method {
					case http.MethodPut:
						entry.data, _ = io.ReadAll(r.Body)
						w.WriteHeader(http.StatusCreated)
					case http.MethodGet:
						if r.Header.Get("Range") != "" {
							rangeRequests++
						}
						http.ServeContent(w, r, "cache", time.Time{}, bytes.NewReader(entry.data))
					default:
						w.WriteHeader(405)
					}
					return
				}
			}
			w.WriteHeader(404)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer local-test-") {
			t.Error("runtime authorization absent")
		}
		var request struct {
			Key, Version string
			RestoreKeys  []string `json:"restore_keys"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		versionBytes, versionErr := hex.DecodeString(request.Version)
		if versionErr != nil || len(versionBytes) != 32 {
			t.Error("cache request version is not a full SHA-256 digest")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch filepath.Base(r.URL.Path) {
		case "GetCacheEntryDownloadURL":
			for _, prefix := range append([]string{request.Key}, request.RestoreKeys...) {
				for i := len(stored) - 1; i >= 0; i-- {
					entry := stored[i]
					if entry.finalized && entry.version == request.Version && strings.HasPrefix(entry.key, prefix) {
						json.NewEncoder(w).Encode(map[string]any{"ok": true, "matched_key": entry.key, "signed_download_url": server.URL + "/blob/" + entry.id})
						return
					}
				}
			}
			fmt.Fprint(w, `{"ok":false}`)
		case "CreateCacheEntry":
			entry := &remoteEntry{key: request.Key, version: request.Version, id: fmt.Sprint(len(stored))}
			stored = append(stored, entry)
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "signed_upload_url": server.URL + "/blob/" + entry.id})
		case "FinalizeCacheEntryUpload":
			for _, entry := range stored {
				if entry.key == request.Key && entry.version == request.Version {
					entry.finalized = true
					fmt.Fprint(w, `{"ok":true}`)
					return
				}
			}
			w.WriteHeader(404)
		default:
			t.Error("unknown service method", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	// Cold baseline, unchanged baseline, restore-only branch, dependency change,
	// and two explicit always/all runs exercise both policies across main/alias.
	for iteration := 0; iteration < 6; iteration++ {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cacheRoot := filepath.Join(root, "cache roots with spaces")
		eventPath := filepath.Join(root, "event.json")
		put(t, eventPath, `{"repository":{"full_name":"test/local","default_branch":"main"}}`)
		if iteration == 3 {
			data, err := os.ReadFile(filepath.Join(workspace, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(workspace, "go.mod"), string(data)+"\n// changed dependency fingerprint\n")
		}
		build, modules := filepath.Join(cacheRoot, "build"), filepath.Join(cacheRoot, "modules")
		fields := map[string]string{}
		for _, key := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE", "GITHUB_STEP_SUMMARY"} {
			fields[key] = filepath.Join(root, key)
			put(t, fields[key], "")
		}
		env := map[string]string{
			"GITHUB_REF": "refs/heads/main", "GITHUB_EVENT_PATH": eventPath,
			"GITHUB_ACTIONS": "true", "RUNNER_DEBUG": "1", "NO_COLOR": "1", "INPUT_SUMMARY": "true",
			"RUNNER_TEMP": runnerTemp, "RUNNER_TOOL_CACHE": "", "GITHUB_WORKSPACE": workspace, "GITHUB_REPOSITORY": "test/local", "GITHUB_WORKFLOW_REF": "test/local/.github/workflows/packaged.yml@refs/heads/main", "GITHUB_JOB": "packaged-smoke", "GITHUB_ACTION": fmt.Sprintf("packaged_smoke_%d", iteration), "GITHUB_RUN_ID": "local", "GITHUB_RUN_ATTEMPT": "1",
			"INPUT_GO-VERSION": version, "INPUT_CHECK-LATEST": "false", "INPUT_CACHE": "restore-save", "INPUT_CACHE-LAYOUT": "combined", "INPUT_CACHE-COMPRESSION": compression, "INPUT_CACHE-STRICT": "true", "INPUT_CACHE-SCOPE": "local-packaged-smoke", "INPUT_CACHE-DEPENDENCY-PATH": ".", "INPUT_CACHE-REPORT": filepath.Join(root, "cache-report.json"),
			"INPUT_CACHE-UPLOAD-CHUNK-BYTES":   "4194304000",
			"INPUT_CACHE-DOWNLOAD-CONCURRENCY": "4",
			"INPUT_CACHE-DOWNLOAD-CHUNK-BYTES": "1048576",
			"INPUT_CACHE-STORAGE":              storage,
			"INPUT_CACHE-GZIP-LEVEL":           gzipLevel,
			"GOCACHE":                          build, "GOMODCACHE": modules, "GOPATH": filepath.Join(root, "gopath"), "GOROOT": toolchain, "GOTOOLCHAIN": "local", "GOTELEMETRY": "off", "ACTIONS_RESULTS_URL": server.URL, "ACTIONS_RUNTIME_TOKEN": "local-test-main", "ACTIONS_CACHE_MODE": "", "STATE_cache_state": "", "STATE_cache_strict": "",
			"HTTP_PROXY": "http://127.0.0.1:1", "HTTPS_PROXY": "http://127.0.0.1:1", "NO_PROXY": "127.0.0.1,localhost",
		}
		if iteration == 2 || iteration >= 4 {
			env["GITHUB_REF"] = "refs/heads/feature"
		}
		if iteration >= 4 {
			env["INPUT_CACHE-SAVE-POLICY"], env["INPUT_CACHE-SAVE-BRANCHES"] = "always", "all"
		}
		if nativeShim != "" {
			env["NODE_OPTIONS"] = "--require=\"" + filepath.ToSlash(nativeShim) + "\""
		}
		for k, v := range fields {
			env[k] = v
		}
		execute := func(binary string, args ...string) {
			t.Helper()
			cmd := exec.Command(binary, args...)
			cmd.Dir = workspace
			for _, v := range os.Environ() {
				k, _, _ := strings.Cut(v, "=")
				overridden := false
				for key := range env {
					if strings.EqualFold(k, key) {
						overridden = true
						break
					}
				}
				if strings.HasPrefix(strings.ToUpper(k), "INPUT_CACHE") {
					overridden = true
				}
				if !overridden {
					cmd.Env = append(cmd.Env, v)
				}
			}
			for k, v := range env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s %v: %v\n%s", binary, args, err, out)
			}
			t.Logf("%s %v\n%s", filepath.Base(binary), args, out)
			if bytes.Contains(out, []byte("local-test-main")) || bytes.Contains(out, []byte("local-test-post")) || bytes.Contains(out, []byte(server.URL)) {
				t.Fatal("subprocess logs exposed runtime credentials or a signed URL")
			}
		}
		launcherRoot := dist
		if iteration%2 == 1 {
			launcherRoot = filepath.Join(dist, "experimental", "node")
		}
		execute(node, filepath.Join(launcherRoot, "launch.cjs"))
		mainSummary, err := os.ReadFile(fields["GITHUB_STEP_SUMMARY"])
		if err != nil || !bytes.Contains(mainSummary, []byte("Go toolchain")) || !bytes.Contains(mainSummary, []byte("Go cache restore")) {
			t.Fatalf("missing packaged main summary: %s %v", mainSummary, err)
		}
		outputs, err := os.ReadFile(fields["GITHUB_OUTPUT"])
		if err != nil {
			t.Fatal(err)
		}
		want := "false"
		if iteration > 0 {
			want = "true"
		}
		if !strings.Contains(string(outputs), "cache-hit="+want+"\n") {
			t.Fatalf("iteration %d cache hit: %s", iteration, outputs)
		}
		// The action must select this offline installation and expose real paths.
		if !strings.Contains(string(outputs), "goroot="+toolchain+"\n") {
			t.Fatalf("unexpected toolchain output %s", outputs)
		}
		execute(goBinary, "test", "./...")
		files := 0
		if err := filepath.WalkDir(build, func(_ string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				files++
			}
			return err
		}); err != nil || files == 0 {
			t.Fatal("real workload created no build cache", err)
		}
		stateData, err := os.ReadFile(fields["GITHUB_STATE"])
		if err != nil {
			t.Fatal(err)
		}
		if iteration == 2 {
			if strings.Contains(string(stateData), "cache_state=") {
				t.Fatal("feature branch registered a default-policy save")
			}
			execute(node, filepath.Join(launcherRoot, "post.cjs"))
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(stateData)), "\n") {
			key, value, _ := strings.Cut(line, "=")
			env["STATE_"+key] = value
		}
		if env["STATE_cache_state"] == "" {
			t.Fatal("main did not save post state")
		}
		env["ACTIONS_RUNTIME_TOKEN"] = "local-test-post"
		env["GITHUB_STEP_SUMMARY"] = filepath.Join(root, "POST_STEP_SUMMARY")
		put(t, env["GITHUB_STEP_SUMMARY"], "")
		execute(node, filepath.Join(launcherRoot, "post.cjs"))
		postSummary, err := os.ReadFile(env["GITHUB_STEP_SUMMARY"])
		if err != nil || !bytes.Contains(postSummary, []byte("Go cache save")) {
			t.Fatalf("missing packaged post summary: %s %v", postSummary, err)
		}
		if after, err := os.ReadFile(fields["GITHUB_STEP_SUMMARY"]); err != nil || !bytes.Equal(after, mainSummary) {
			t.Fatal("post modified the main step summary", err)
		}
		if _, err := os.Stat(env["STATE_cache_state"]); !os.IsNotExist(err) {
			t.Fatal("post did not remove private state", err)
		}
		data, err := os.ReadFile(env["INPUT_CACHE-REPORT"] + ".post.json")
		var postReport Report
		if err != nil || json.Unmarshal(data, &postReport) != nil || len(postReport.Snapshots) != 1 {
			t.Fatalf("missing packaged post report: %s %v", data, err)
		}
		if m := postReport.Snapshots[0]; iteration == 1 {
			if !m.Skipped || m.SaveReason != "dependency-match" || m.Bytes != 0 || m.ArchiveMS != 0 {
				t.Fatalf("warm baseline did save work: %+v", m)
			}
		} else if m.Skipped || m.Bytes == 0 {
			t.Fatalf("eligible snapshot did not save: %+v", m)
		}
		if bash := os.Getenv("READY_SETUP_GO_TEST_BASH"); iteration == 1 && bash != "" && nativeManifest == nil {
			env["READY_SETUP_GO_ROOT"] = dist
			env["READY_SETUP_GO_VERSION"] = version
			env["READY_SETUP_GO_LATEST"] = "false"
			env["RUNNER_OS"] = map[string]string{"windows": "Windows", "linux": "Linux", "darwin": "macOS"}[runtime.GOOS]
			env["RUNNER_ARCH"] = map[string]string{"amd64": "X64", "arm64": "ARM64"}[runtime.GOARCH]
			execute(bash, "--noprofile", "--norc", filepath.Join(dist, "launch.sh"), "--cache=off")
		}
	}
	if nativeManifest != nil {
		data, err := os.ReadFile(nativeCount)
		if err != nil || string(data) != "download\\n" {
			t.Fatalf("main/post/alias should download exactly one native asset: %q %v", data, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if storage == "archive" && rangeRequests < 2 {
		t.Fatalf("packaged archive restore did not exercise multiple ranges: %d", rangeRequests)
	}
	if storage == "chunked" && rangeRequests != 0 {
		t.Fatalf("chunk workers unexpectedly used nested ranges: %d", rangeRequests)
	}
	var snapshots []string
	for _, entry := range stored {
		if !entry.finalized {
			t.Fatal("unfinalized cache entry", entry.key)
		}
		if !strings.Contains(entry.key, "-chunk-") {
			snapshots = append(snapshots, entry.key)
		}
	}
	if len(snapshots) != 4 {
		t.Fatal("main/post failed baseline and refresh lifecycle", len(stored), len(snapshots))
	}
	seen := map[string]bool{}
	for _, key := range snapshots {
		if seen[key] {
			t.Fatal("reused immutable snapshot key")
		}
		seen[key] = true
	}
}
