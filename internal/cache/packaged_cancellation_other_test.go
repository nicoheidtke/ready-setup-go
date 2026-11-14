//go:build !windows

package cache

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// This opt-in fixture sends a signal only to the actual Node launcher, not its
// process group. The packaged Go child must cancel its active HTTP request and
// remove private staging before Node exits. It proves local POSIX process
// behavior, not GitHub runner cancellation or forceful Windows termination.
func TestPackagedCancellation(t *testing.T) {
	dist := os.Getenv("SETUP_GO_FASTER_TEST_DIST")
	if dist == "" {
		t.Skip("set SETUP_GO_FASTER_TEST_DIST to test packaged POSIX cancellation")
	}
	dist, err := filepath.Abs(dist)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dist, "NATIVE_DOWNLOADS.json")); err == nil {
		t.Skip("real-process cancellation fixture requires a bundled distribution")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	runnerTemp := os.Getenv("SETUP_GO_FASTER_TEST_RUNNER_TEMP")
	if runnerTemp == "" {
		runnerTemp = filepath.Join(repo, ".cache", "installer smoke with spaces")
	}
	runnerTemp, err = filepath.Abs(runnerTemp)
	if err != nil {
		t.Fatal(err)
	}
	version := os.Getenv("SETUP_GO_FASTER_TEST_VERSION")
	if version == "" {
		version = "1.26.4"
	}
	toolchain := filepath.Join(runnerTemp, "setup-go-faster", "toolchains", version, runtime.GOOS+"-"+runtime.GOARCH)
	if _, err := os.Stat(filepath.Join(toolchain, "bin", "go")); err != nil {
		t.Fatalf("preinstall toolchain before opt-in cancellation test: %v", err)
	}
	node := os.Getenv("SETUP_GO_FASTER_TEST_NODE")
	if node == "" {
		node = "node"
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			root := t.TempDir()
			temp := filepath.Join(root, "private temp")
			if err := os.Mkdir(temp, 0700); err != nil {
				t.Fatal(err)
			}
			started, cancelled := make(chan struct{}), make(chan struct{})
			var writes atomic.Int32
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/blob" {
					w.Header().Set("Content-Length", "65536")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(make([]byte, 512))
					w.(http.Flusher).Flush()
					close(started)
					<-r.Context().Done()
					close(cancelled)
					return
				}
				if filepath.Base(r.URL.Path) != "GetCacheEntryDownloadURL" {
					writes.Add(1)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				var request struct{ Key string }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "matched_key": request.Key, "signed_download_url": server.URL + "/blob"})
			}))
			defer server.Close()
			env := map[string]string{
				"RUNNER_TEMP": runnerTemp, "RUNNER_TOOL_CACHE": "", "TMPDIR": temp, "TMP": temp, "TEMP": temp,
				"GITHUB_ACTIONS": "true", "GITHUB_WORKSPACE": filepath.Join(repo, "testdata", "smoke"),
				"GITHUB_REPOSITORY": "test/local", "GITHUB_JOB": "cancel", "GITHUB_ACTION": "cancel", "GITHUB_WORKFLOW_REF": "test/local/.github/workflows/test.yml@refs/heads/main",
				"INPUT_GO-VERSION": version, "INPUT_CHECK-LATEST": "false", "INPUT_CACHE": "restore-save", "INPUT_CACHE-LAYOUT": "combined", "INPUT_CACHE-STRICT": "true", "INPUT_SUMMARY": "false",
				"GOCACHE": filepath.Join(root, "build"), "GOMODCACHE": filepath.Join(root, "modules"), "GOROOT": toolchain, "GOTOOLCHAIN": "local", "GOTELEMETRY": "off",
				"ACTIONS_RESULTS_URL": server.URL, "ACTIONS_RUNTIME_TOKEN": "local-test-cancel", "ACTIONS_CACHE_MODE": "", "STATE_cache_state": "", "NODE_OPTIONS": "",
				"HTTP_PROXY": "http://127.0.0.1:1", "HTTPS_PROXY": "http://127.0.0.1:1", "NO_PROXY": "127.0.0.1,localhost",
			}
			for _, name := range []string{"GITHUB_ENV", "GITHUB_PATH", "GITHUB_OUTPUT", "GITHUB_STATE"} {
				env[name] = filepath.Join(root, name)
				put(t, env[name], "")
			}
			cmd := exec.Command(node, filepath.Join(dist, "launch.cjs"))
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			for _, value := range os.Environ() {
				name, _, _ := strings.Cut(value, "=")
				if _, replaced := env[name]; !replaced && !strings.HasPrefix(name, "INPUT_") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			for name, value := range env {
				cmd.Env = append(cmd.Env, name+"="+value)
			}
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			joined := false
			defer func() {
				if !joined || t.Failed() {
					// Bound fixture failures, including a launcher that leaves its
					// native child alive. Kill only this fixture's private group.
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					if joined {
						return
					}
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("fixture processes did not terminate")
					}
				}
			}()
			select {
			case <-started:
			case err := <-done:
				joined = true
				t.Fatalf("launcher exited before transfer: %v\n%s", err, output.String())
			case <-time.After(10 * time.Second):
				t.Fatal("native transfer did not start")
			}
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				joined = true
				if err == nil {
					t.Fatal("cancelled launcher reported success")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("launcher did not join cancelled native child")
			}
			select {
			case <-cancelled:
			case <-time.After(2 * time.Second):
				t.Fatal("native transfer remained active after launcher exit")
			}
			if writes.Load() != 0 {
				t.Fatal("cancelled restore attempted remote publication")
			}
			for _, path := range []string{env["GOCACHE"], env["GOMODCACHE"]} {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("cancelled transfer published destination %s: %v", filepath.Base(path), err)
				}
			}
			partials, err := filepath.Glob(filepath.Join(temp, "setup-go-faster-prefetch-*"))
			if err != nil || len(partials) != 0 {
				t.Fatalf("private transfer files survived cleanup: %d (%v)", len(partials), err)
			}
			state, err := os.ReadFile(env["GITHUB_STATE"])
			if err != nil || len(state) != 0 {
				t.Fatal("cancelled main published post-job state", err)
			}
		})
	}
}
