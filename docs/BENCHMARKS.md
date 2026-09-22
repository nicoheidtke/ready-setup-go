# Benchmark methodology

Measure total cost: action acquisition, process launch, version resolution, toolchain transfer, extraction, and the workload that follows. Report cold and warm cases separately. Never equate cross-compilation with platform validation.

## Installer and launcher comparison

`cmd/benchmark` launches the identical packaged installer through Bash and the dependency-free Node adapter. It requires at least 20 samples **per launcher**, reverses the pair order on alternate samples, and writes all observations plus median and nearest-rank p95.

```text
go run ./cmd/benchmark --dist dist --version 1.26.0 --scenario installed-exact --samples 20 --out installed-exact.json
```

Scenarios:

| Scenario | Preparation | Measured operation |
| --- | --- | --- |
| `installed-exact` | Seed the exact version outside measurement | Reuse exact version with HTTP(S) routed to a closed local proxy |
| `installed-minor` | Seed the exact version outside measurement | Resolve its minor selector locally with the same network block |
| `latest-minor` | Seed the exact version outside measurement | Check official release metadata for every sample |
| `cold` | Give every sample a fresh installation directory and empty runner-cache setting | Resolve, download, verify, extract, and publish |

Warm cases are additionally checked by unit tests with a transport that rejects and counts every request. Proxy blocking in a workflow is an extra integration check, not an OS-level network sandbox.

Supply `--node` with the runner's Node 24 executable for a representative comparison; the default `node` on PATH may have a different version. `--shell` selects Bash. The JSON report records runtime versions, OS/architecture, runner image variables, source revision, and per-sample selected Go versions.

The Bash process models the composite step shell and uses `exec` for the installer. The Node process launches the same executable. Full GitHub runner dispatch overhead is outside this subprocess comparison.

## Metric definitions

- `resolution_ms`: local discovery plus any official metadata request and parsing.
- `download_ms`: archive request/header latency plus elapsed time spent reading the response body.
- `extraction_ms`: the archive processing interval excluding those response-body reads; ZIP spooling, hashing, and filesystem work are included.
- `total_ms`: native setup call, including publication and cleanup, excluding CLI startup and GitHub environment export.
- `launcher_and_export_ms`: subprocess wall time minus native setup time. This also includes CLI initialization, JSON serialization and writing, and environment export; it is not a pure Node/Bash startup measurement.
- `metadata_bytes` / `archive_bytes`: received payload bytes, excluding HTTP/TLS overhead.
- `peak_temporary_bytes`: peak logical file payload for staging/extraction, the completion marker, and ZIP spooling. It excludes filesystem metadata, block rounding, the installer executable, and already installed toolchains.

Tar download and extraction overlap. The reported partition attributes body-read time to download; it does not pretend there are two independent sequential phases. Instrumentation itself has overhead and should be considered when interpreting small differences.

## Current action baselines

The manually dispatched **Manual runtime benchmarks** workflow runs on a selected hosted platform. It resolves current `actions/setup-go` and `WillAbides/setup-go-faster` main commits once, checks out those exact revisions, and invokes their local actions alongside the packaged implementation.

Each scenario has 20 fresh jobs. Execution order reverses between samples. Caching is disabled in `actions/setup-go`; `setup-go-faster` does not manage build/module cache snapshots. Minor selectors and latest checks are mapped to each action's corresponding inputs.

Cold jobs give each action an isolated tool-cache and temporary path. Warm jobs seed equivalent private tool caches outside measured steps. Review the selected `setup-go-faster` implementation before interpreting Windows data: it may select a different installation volume or local discovery path. Record differences rather than silently treating unequal paths as equivalent.

Raw GitHub job-step observations are retrieved using the jobs API. Median and p95 summaries label groups with fewer than 20 successful observations as incomplete. Failed samples remain visible in the report; do not discard them to improve a result.

API step timestamps have coarse resolution. The runner's **Set up job** step combines action acquisition and other preparation, so it is recorded separately but cannot be attributed to a specific action. The local-action baseline primarily compares execution, not each upstream repository's acquisition cost.

Metadata artifacts record exact action revisions and runner image details. No measurement workflow runs automatically on a normal push or pull request.

## Bundled versus matching-binary acquisition

The packager creates a complete action ZIP, a launcher-only ZIP, and each native executable. Host these as immutable public HTTPS assets, then supply their URLs and SHA-256 values to **Manual action acquisition experiment** or `cmd/acquisition`.

For each of at least 20 paired samples, compare:

1. Download, verify, and unpack the complete bundle.
2. Download, verify, and unpack the launcher-only ZIP, then download and verify the matching executable.

Alternate ordering. Record received bytes, transfer/checksum time, ZIP extraction time, combined time, and peak logical file bytes. Both paths include checksum verification. The second path includes both network requests.

This is a controlled acquisition experiment, not an exact emulation of GitHub's repository archive CDN or preparation service. CDN cache state, latency, and hosting location affect results; preserve URLs/checksums and runner metadata in the report. Repeat on each target platform.

## Interpreting results

Compare paired differences, medians, and p95 together. Investigate regressions; record overlapping or coarse results as inconclusive. Repeat suspicious outcomes before changing the default launcher or distribution format. Latest-patch experiments must disclose if the selected version changes during the run.

Do not publish a single speedup number averaged across cold installations, installed exact versions, and latest-version checks. They represent different user workloads.

When caching is added, include restore, compile/test, pruning, and upload in the measured job. Extend workloads to a small module, parallel build/test/lint jobs, and a multi-module monorepo. Installer speed alone cannot establish cache efficiency.

## Primary real-world workload

Use [exasol/exasol-personal](https://github.com/exasol/exasol-personal) as the primary acceptance project. The initial pinned revision is `54b7e1a7454f49d821102bcab5bfce2c7747f8c6`; record any revision change alongside results. First run the small `testdata/packages` workload using pinned `github.com/google/uuid` and `golang.org/x/sync` versions, with normal Go checksum verification.

`.github/workflows/workload.yml` validates separate Exasol Go build, unit-test, and lint jobs using the packaged installer. It follows the upstream generation mode for tests (`SKIP_EMBED=true`), retains the required `containers_image_openpgp` tag, and runs Linux unit tests with the race detector. Build jobs use CGO disabled and the upstream no-inlining setting. Generated placeholder-resource binaries are compilation checks, not distributable Exasol releases. Python integration tests and deployment tests are separate acceptance stages; cloud/VM provisioning is not part of this Go workload.

This workflow is a correctness gate, not a performance comparison: packaging uses a bootstrap Go installation, there is only one observation per job, and caching is not implemented. Its module/build cache directories are isolated from the bootstrap and package smoke test.

For the later cache comparison, preserve Exasol's actual parallel build/test/lint split and account for its current setup action's module and tool warmup (`go mod download`, golangci-lint, tflint and plugin initialization). Measure the complete job before removing or changing that warmup. Include tool caches and resource-generation costs explicitly. Compare cold, unchanged, source-change, dependency-change, and Go-upgrade runs at fixed revisions; avoid drawing cache conclusions from the installer timer or a single local run.
