# Development and release preparation

The consumer README describes the action interface. Development status and future ideas belong in [ROADMAP.md](../ROADMAP.md), not the README.

## Current verification status

The user authorized validation on 2026-09-22, superseding the earlier implementation-only restriction.

Verified locally on Windows/amd64 with Go 1.26.4:

- Go sources formatted; `go test ./...` and `go vet ./...` passed.
- Windows installer packaged and used to download, verify, and install Go 1.26.4 into a path containing spaces.
- Minor selector reused that installation with HTTP(S) proxies pointing to a closed local port.
- The installed toolchain passed the pinned `google/uuid` and `golang.org/x/sync` smoke test in `testdata/packages`.
- Node and Git Bash launchers passed offline exact-version reuse. Git Bash required execution outside the local sandbox to create its signal pipe.
- All six native executables cross-compiled. Two builds produced identical SHA-256 manifests for the six executables and both ZIP archives. This is packaging verification, not native validation of the other five platforms.
- All workflow files passed actionlint v1.7.12 with optional shellcheck/pyflakes integrations disabled. An initial v1.7.7 run was too old to recognize Node 24 and newer hosted runner labels.
- Exasol Personal generation and Windows/amd64 application compilation passed with the installed Go 1.26.4 toolchain. Its full Windows Go unit suite failed; see [validation record](validation-2026-09-22.md).

Hosted integration across all six platforms and comparative performance measurements remain pending. The GitHub remote is now configured as `origin` at `https://github.com/nicoheidtke/setup-go-better.git` and connectivity was verified. Both repositories still have no commits; hosted workflows require committing and pushing the sources first. Local downloads and module verification used workspace caches; initial attempts to write the user's module checksum cache were denied and rerun with a workspace GOPATH. Go also emitted telemetry upload-token permission warnings without failing the completed checks.

The manual Exasol Personal workflow pins commit `54b7e1a7454f49d821102bcab5bfce2c7747f8c6`, uses Go 1.26.4 (above its declared 1.26.3 minimum), and runs separate Linux build/test/lint jobs plus a Windows build. Generation uses upstream's `SKIP_EMBED=true` mode and `containers_image_openpgp` build tag. These are Go workload checks, not release builds or full Python integration/deployment tests. See [workload methodology](BENCHMARKS.md#primary-real-world-workload).

## Source layout

- `cmd/setup-go-better`: native installer entrypoint.
- `internal/installer`: version selection, installed-toolchain discovery, release metadata, verified extraction, publication.
- `internal/actionenv`: GitHub environment and output files.
- `cmd/package`: reproducible native executables and action archives.
- `cmd/benchmark`, `cmd/acquisition`, `cmd/workflow-report`: measurement tools.
- `experimental/node`: dependency-free comparison launcher.
- `.github/workflows`: validation, packaging, and manually dispatched benchmarks.

No Go modules beyond the standard library are required. No npm toolchain is required.

## Local validation

Use Go 1.26.4 to match the packaging workflow. Disable telemetry if desired with `GOTELEMETRY=off`.

```text
gofmt -w cmd internal testdata/smoke
go test ./...
go vet ./...
go run ./cmd/package --platform windows/amd64
```

Substitute `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, or `windows/arm64` as appropriate. Unit tests serve fixture metadata and archives locally; they do not require public Go downloads. Integration jobs deliberately download official toolchains and execute a real module in a subsequent workflow step.

`dist` is generated and ignored. Local workflow tests use `uses: ./dist`; the source-tree action requires the generated `bin` directory and must not be advertised as a runnable distribution by itself.

## Reproducible distribution

```text
go run ./cmd/package --out dist --archives artifacts
```

The packager uses `CGO_ENABLED=0`, fixed architecture baselines, `-trimpath`, no VCS stamping, no build ID, and stripped symbols. ZIP entry times and modes are deterministic. Reproducibility requires identical sources, compiler, and build settings; the manual packaging workflow compares two builds.

Outputs:

- `dist`: action metadata, launchers, documentation, six executables, and executable checksums.
- `artifacts/bundled.zip`: the complete action distribution.
- `artifacts/launcher.zip`: the same distribution without native executables, for acquisition experiments only.
- `artifacts/setup-go-better-<os>-<arch>[.exe]`: individual native executables.
- `artifacts/SHA256SUMS`: checksums of experiment/release assets.

The launcher-only ZIP is not a runnable action by itself. The acquisition experiment measures fetching it followed by the matching binary; it does not add a runtime-downloading production launcher.

The packaging workflow prepares downloadable artifacts and does **not** create tags, releases, commits, or push to a repository. To publish, place the complete distribution on a release commit, preserve executable modes for Unix binaries, and point the chosen action tag at that commit. A future release process can automate that once repository ownership, versioning, and publication policy are decided.

## Implementation boundaries

Completion is based on a marker, a matching `VERSION` file, and a regular Go executable. This trusts the runner's local filesystem; it is not a revalidation of every installed compiler file. Failed fresh installs are cleaned up. An incomplete pre-existing destination produces an actionable error instead of being overwritten.

Archive links and duplicate file entries are rejected. Extraction writes only into a newly created staging directory and validates archive paths before writing. The action does not promise robust shared writable state across untrusted processes on a self-hosted runner.

Networking uses a bounded client and operation context, honors the standard Go proxy environment, and surfaces errors rather than installing an unverified fallback. There is no automatic retry/backoff policy yet; evaluate availability and latency together before adding one.

The benchmark tools and packaging commands execute subprocesses only when explicitly run. Merely writing these files has not executed them.
