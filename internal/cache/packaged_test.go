package cache

import (
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
)

// TestPackagedLifecycle is opt-in: it consumes a prebuilt distribution and an
// already installed toolchain, never compiling the action or fetching Go. This
// checks the actual subprocess/file-protocol boundary which unit APIs cannot.
func TestPackagedLifecycle(t *testing.T) {
	dist := os.Getenv("SETUP_GO_BETTER_TEST_DIST")
	if dist == "" {
		t.Skip("set SETUP_GO_BETTER_TEST_DIST to test an existing native distribution")
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
	runnerTemp := os.Getenv("SETUP_GO_BETTER_TEST_RUNNER_TEMP")
	if runnerTemp == "" {
		runnerTemp = filepath.Join(repo, ".cache", "installer smoke with spaces")
	}
	runnerTemp, err = filepath.Abs(runnerTemp)
	if err != nil {
		t.Fatal(err)
	}
	version := os.Getenv("SETUP_GO_BETTER_TEST_VERSION")
	if version == "" {
		version = "1.26.4"
	}
	storage := os.Getenv("SETUP_GO_BETTER_TEST_STORAGE")
	if storage == "" {
		storage = "archive"
	}
	toolchain := filepath.Join(runnerTemp, "setup-go-better", "toolchains", version, runtime.GOOS+"-"+runtime.GOARCH)
	goName := "go"
	if runtime.GOOS == "windows" {
		goName += ".exe"
	}
	goBinary := filepath.Join(toolchain, "bin", goName)
	if _, err = os.Stat(goBinary); err != nil {
		t.Fatalf("preinstall toolchain before opt-in test: %v", err)
	}
	node := os.Getenv("SETUP_GO_BETTER_TEST_NODE")
	if node == "" {
		node = "node"
	}
	workspace := filepath.Join(repo, "testdata", "smoke")
	type remoteEntry struct {
		key, version, id string
		data             []byte
		finalized        bool
	}
	var mu sync.Mutex
	var stored []*remoteEntry
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
						w.Write(entry.data)
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

	for iteration := 0; iteration < 2; iteration++ {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cacheRoot := filepath.Join(root, "cache roots with spaces")
		build, modules := filepath.Join(cacheRoot, "build"), filepath.Join(cacheRoot, "modules")
		fields := map[string]string{}
		for _, key := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE"} {
			fields[key] = filepath.Join(root, key)
			put(t, fields[key], "")
		}
		env := map[string]string{
			"RUNNER_TEMP": runnerTemp, "RUNNER_TOOL_CACHE": "", "GITHUB_WORKSPACE": workspace, "GITHUB_REPOSITORY": "test/local", "GITHUB_WORKFLOW_REF": "test/local/.github/workflows/packaged.yml@refs/heads/main", "GITHUB_JOB": "packaged-smoke", "GITHUB_ACTION": fmt.Sprintf("packaged_smoke_%d", iteration), "GITHUB_RUN_ID": "local", "GITHUB_RUN_ATTEMPT": "1",
			"INPUT_GO-VERSION": version, "INPUT_CHECK-LATEST": "false", "INPUT_CACHE": "restore-save", "INPUT_CACHE-LAYOUT": "combined", "INPUT_CACHE-COMPRESSION": "gzip", "INPUT_CACHE-STRICT": "true", "INPUT_CACHE-SCOPE": "local-packaged-smoke", "INPUT_CACHE-DEPENDENCY-PATH": ".", "INPUT_CACHE-REPORT": filepath.Join(root, "cache-report.json"),
			"INPUT_CACHE-UPLOAD-CHUNK-BYTES": "4194304000",
			"INPUT_CACHE-STORAGE":            storage,
			"GOCACHE":                        build, "GOMODCACHE": modules, "GOPATH": filepath.Join(root, "gopath"), "GOROOT": toolchain, "GOTOOLCHAIN": "local", "GOTELEMETRY": "off", "ACTIONS_RESULTS_URL": server.URL, "ACTIONS_RUNTIME_TOKEN": "local-test-main", "ACTIONS_CACHE_MODE": "", "STATE_cache_state": "", "STATE_cache_strict": "",
			"HTTP_PROXY": "http://127.0.0.1:1", "HTTPS_PROXY": "http://127.0.0.1:1", "NO_PROXY": "127.0.0.1,localhost",
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
		}
		launcherRoot := dist
		if iteration == 1 {
			launcherRoot = filepath.Join(dist, "experimental", "node")
		}
		execute(node, filepath.Join(launcherRoot, "launch.cjs"))
		outputs, err := os.ReadFile(fields["GITHUB_OUTPUT"])
		if err != nil {
			t.Fatal(err)
		}
		want := "false"
		if iteration == 1 {
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
		for _, line := range strings.Split(strings.TrimSpace(string(stateData)), "\n") {
			key, value, _ := strings.Cut(line, "=")
			env["STATE_"+key] = value
		}
		if env["STATE_cache_state"] == "" {
			t.Fatal("main did not save post state")
		}
		env["ACTIONS_RUNTIME_TOKEN"] = "local-test-post"
		execute(node, filepath.Join(launcherRoot, "post.cjs"))
		if _, err := os.Stat(env["STATE_cache_state"]); !os.IsNotExist(err) {
			t.Fatal("post did not remove private state", err)
		}
		if _, err := os.Stat(env["INPUT_CACHE-REPORT"] + ".post.json"); err != nil {
			t.Fatal(err)
		}
		if bash := os.Getenv("SETUP_GO_BETTER_TEST_BASH"); iteration == 1 && bash != "" {
			env["SETUP_GO_BETTER_ROOT"] = dist
			env["SETUP_GO_BETTER_VERSION"] = version
			env["SETUP_GO_BETTER_LATEST"] = "false"
			env["RUNNER_OS"] = map[string]string{"windows": "Windows", "linux": "Linux", "darwin": "macOS"}[runtime.GOOS]
			env["RUNNER_ARCH"] = map[string]string{"amd64": "X64", "arm64": "ARM64"}[runtime.GOARCH]
			execute(bash, "--noprofile", "--norc", filepath.Join(dist, "launch.sh"), "--cache=off")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	var snapshots []string
	for _, entry := range stored {
		if !entry.finalized {
			t.Fatal("unfinalized cache entry", entry.key)
		}
		if !strings.Contains(entry.key, "-chunk-") {
			snapshots = append(snapshots, entry.key)
		}
	}
	if len(snapshots) != 2 || snapshots[0] == snapshots[1] {
		t.Fatal("main/post failed fresh immutable snapshot lifecycle", len(stored))
	}
}
