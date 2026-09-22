# Local validation — 2026-09-22

Host: Windows/amd64. Bootstrap and installed compiler: Go 1.26.4. These checks establish local correctness evidence; they are not hosted runner benchmarks or evidence of a speedup.

## Setup Go Better

Passed:

- `gofmt -w cmd internal testdata/smoke` and formatting the new package fixture.
- `go test ./...` and `go vet ./...`.
- Native Windows packaging, official checksum-verified cold installation into a directory containing spaces, and offline minor-version reuse.
- Offline exact-version reuse through both packaged Node and Bash launchers. The local Bash test supplied the GitHub runner environment variables required by the launcher.
- `go test -count=1 ./...` in `testdata/packages`, using the freshly installed compiler, pinned modules, and Go's normal module checksum verification.
- Six-platform packaging twice, comparing SHA-256 manifests containing all six executables, `bundled.zip`, and `launcher.zip`.
- `actionlint` v1.7.12 on all workflows, with `-shellcheck= -pyflakes=`.

Generated packages, compiler installations, checkout, and logs remain under ignored `.cache/` and `dist/`. No release commit, tag, or remote workflow was created. The local repository has no configured Git remote.

## Exasol Personal

Repository: <https://github.com/exasol/exasol-personal>

Revision: `54b7e1a7454f49d821102bcab5bfce2c7747f8c6`.

The isolated checkout's dependency files remained unchanged. Generation used `GOFLAGS=-tags=containers_image_openpgp` and `SKIP_EMBED=true`. Local Go module/build caches and application-data directories were redirected into the workspace after initial permission errors. Generation and this compilation succeeded:

```text
go build -mod=readonly -tags=containers_image_openpgp -trimpath -gcflags=all=-l -o ../exasol-validation.exe ./cmd/exasol
```

Compilation used `CGO_ENABLED=0`. This is a Go application compilation check with test-mode resources, not a release artifact or deployment test.

The full Windows test attempt used:

```text
go test -mod=readonly -tags=containers_image_openpgp -count=1 -timeout=5m ./...
```

It failed. Observed failures include:

- `internal/localruntime/mac_data_migration_test.go:271` references `syscall.Stat_t`, which is unavailable on Windows. A separate compile-only test with the pre-existing Go 1.26.4 compiler reproduced this failure, independent of the installer.
- Unix file-mode expectations and a non-writable-directory test do not match Windows filesystem behavior.
- Symlink fixture creation/extraction lacks the required Windows privilege.
- Tests assume Unix `echo` and `true` executables are on PATH.
- Path escaping/separators and CRLF template output differ from test expectations.
- Task-runner ordering/log assertions also failed; their cause has not been isolated.

Raw local output is in `.cache/exasol-test.log`; generation and build logs are alongside it. These failures were retained rather than skipped or counted as passes. No upstream source fixes were made.

## Remaining acceptance

Run the manual Exasol workflow on hosted runners once this action has a GitHub remote and a committed runnable workflow. It mirrors the upstream CI's Linux unit-test/lint placement and Windows/Linux builds. Linux tests include `-race`; the local Windows attempt did not. Python CLI integration tests and any deployment tests remain separate work.

Run native installer integration on all six target platforms and then paired performance measurements. Caching is still unimplemented; Exasol's current shared-cache warmup costs must be included when evaluating the later cache milestone.
