# Development

Start with [CONTRIBUTING.md](../CONTRIBUTING.md) for prerequisites, checks, and change expectations. This guide covers integration and current validation boundaries. [Release procedures](RELEASES.md) cover packaging/publication; [measurement methodology](BENCHMARKS.md) covers performance evidence.

## Source layout

| Path | Responsibility |
| --- | --- |
| `cmd/setup-go-faster` | Native setup and post-job entrypoints |
| `internal/installer` | Version selection, discovery, verified download/extraction, and installation |
| `internal/cache` | Inputs/configuration; separate state/identity, backend policy, transfer, lifecycle, and report files |
| `internal/cachearchive` | Shared archive types/options; separate packing, restore/staging, merge, path-safety, and bounded-I/O files |
| `internal/cachebackend` | Cache-v2 and signed blob transfers; see [protocol](../internal/cachebackend/PROTOCOL.md) |
| `internal/actionenv`, `internal/actionlog`, `internal/httpretry` | Runner protocols, presentation, and replay-safe retries |
| `internal/distribution`, `internal/release` | Provenance, integrity, and publication |
| `cmd/package`, `cmd/verify-distribution`, `cmd/release` | Distribution/release commands |
| `cmd/check`, `cmd/check-docs` | Contributor checks and documentation consistency |
| Other `cmd/` tools | Installer, acquisition, cache, and workflow measurements |
| Root `.cjs` files | Dependency-free Node 24 runner bridge |
| `experimental/` | Node alias and installer-only Bash comparison |
| `testdata/`, `.github/` | Workloads, fixtures, and manual workflows |

Runtime Go code uses the standard library plus the pinned `github.com/klauspost/compress/zstd` codec. Consumers use bundled executables without building the action, installing npm packages, or installing compression tools. Source-only revisions cannot run as actions. Optional lightweight distribution packaging adds a verified public-asset bootstrap; see [release procedures](RELEASES.md#lightweight-distributions).

## Maintained interfaces

The root `action.yml` is the recommended consumer entrypoint. `experimental/node` remains a compatibility alias for the same main/post lifecycle. `experimental/composite` and `launch.sh` are installer-only comparison paths; they do not provide the cache lifecycle. Integration fixtures and launcher benchmarks still exercise these paths, so changing or removing them requires updating that coverage and assessing compatibility.

Setup input parsing lives in `cmd/setup-go-faster/options.go`, separate from installation and runner protocols. Contract tests compare parsed defaults and every action input with root/Node-alias metadata, including explicit flag overrides. Keep these tests current when adding an input; documentation defaults are independently checked by `cmd/check-docs`. The launcher continues translating `check-latest` and standalone version fallback variables into flags.

Measurement commands are maintainer tools, separate from the action's public inputs and outputs. Their purposes and measurement limits are listed in [performance measurement](BENCHMARKS.md#tools). Keep generated distributions and raw reports in ignored output directories.

## Validation

Release builds use Go 1.27.1. Also check the Go 1.26 line and use Node 24 to verify the action runtime. Run contribution checks after changes; native integration and hosted service checks cover additional boundaries.

### Bounded fuzzing

Ordinary tests execute the seed corpora for `FuzzPortablePath` and `FuzzVersionFile`. They check native path confinement/canonicalization, stable Unicode case folding, and supported version-selector extraction/round trips. Mutation fuzzing is opt-in; run one target at a time with a fixed duration and worker bound:

```text
go test ./internal/cachearchive -run='^$' -fuzz='^FuzzPortablePath$' -fuzztime=30s -parallel=2
go test ./internal/installer -run='^$' -fuzz='^FuzzVersionFile$' -fuzztime=30s -parallel=2
```

Targets skip oversized mutations (4 KiB paths, 16 KiB version-file content) to bound per-input work. These are test limits, not changes to runtime acceptance. Fuzzing uses the local Go fuzz cache and may write a minimized failure under the package's `testdata/fuzz` directory; review and retain reproducible regression inputs. Short local runs cover only their stated corpus, compiler, and native platform; they do not establish archive-stream, hosted-service, or cross-platform coverage. Use `go run ./cmd/check --race` separately for concurrent runtime packages; report missing race-toolchain prerequisites as skipped coverage, not a pass.

### Local packaged lifecycle

Package the native platform into an ignored directory (substitute the host OS/architecture):

```text
go run ./cmd/package --platform windows/amd64 --out .cache/native-dist
```

`TestPackagedLifecycle` consumes a prebuilt distribution and an installed toolchain; it never downloads Go or compiles the action. Set these environment variables:

| Variable | Value |
| --- | --- |
| `SETUP_GO_FASTER_TEST_DIST` | Absolute bundled or lightweight distribution directory |
| `SETUP_GO_FASTER_TEST_NATIVE_ASSETS` | For lightweight distributions, the matching local packaged asset directory; injects a file-stream transport and isolates bootstrap reuse without contacting GitHub |
| `SETUP_GO_FASTER_TEST_RUNNER_TEMP` | Parent of `setup-go-faster/toolchains/<version>/<os>-<arch>/bin/go[.exe]` |
| `SETUP_GO_FASTER_TEST_VERSION` | Installed exact version; default `1.26.4` |
| `SETUP_GO_FASTER_TEST_STORAGE` | Run once with `archive`, once with `chunked` |
| `SETUP_GO_FASTER_TEST_COMPRESSION` | `gzip` (default), `zstd`, or `none`; repeat for the codecs affected by a change |
| `SETUP_GO_FASTER_TEST_GZIP_LEVEL` | `default` (default) or `1` through `9`; repeat gzip tests with `1` for the fast profile |
| `SETUP_GO_FASTER_TEST_NODE` | Node 24 executable; default `node` |
| `SETUP_GO_FASTER_TEST_BASH` | Optional Bash executable for installer-only reuse |

```text
go test ./internal/cache -run '^TestPackagedLifecycle$' -count=1 -v
```

The test exercises root main/post, the Node alias, reports/state/summaries, real compilation, and offline Bash reuse against a loopback cache-v2 fixture. Without the distribution variable it is skipped. For lightweight distributions, the native-assets variable is required; the fixture verifies one bootstrap acquisition across main/post and the Node alias using actual packaged executable bytes. This injected local transport does not validate public GitHub redirects, private repository access, or hosted performance. Unit fixtures cover archive safety, existing-file priority, read-only modules, cancellation/worker cleanup, access-policy changes, and forced immutable-writer races.

### Packaged process cancellation

With a newly packaged bundled distribution and the same installed-toolchain variables above, run:

```text
go test ./internal/cache -run '^TestPackagedCancellation$' -count=1 -v
```

On Linux/macOS this fixture starts the actual Node launcher and native executable against a stalled loopback cache download, then sends SIGINT and SIGTERM only to Node. It requires a failed launcher exit after the native request is cancelled and private transfer files are removed, without cache publication or post-job state. Fixture timeouts kill its private process group on failure. The native executable handles both POSIX signals; the asynchronous launcher forwards them and waits for child cleanup. Mock launcher tests also check forwarding and joining without sending signals to the test runner.

Windows skips this fixture explicitly: Node child termination uses forceful Windows process termination and cannot guarantee deferred native cleanup. Native console interrupts remain supported. Local POSIX signal tests do not establish GitHub runner cancellation behavior, force-kill cleanup, or hosted post-job policy; retain the separate hosted cancellation probe.

### Hosted lifecycle

Workflows are manual; maintainers control execution and costs. Native integration checks all six targets without multiplying matrix jobs: each runs bundled main/post fixtures for gzip/zstd/none with archive/chunked storage, one platform-appropriate cancellation fixture, and lightweight archive/zstd plus chunked/gzip bootstrap/reuse cases. The lightweight fixtures use local packaged assets, not public release downloads. Distribution preparation independently gates artifact upload on the same cases on Linux amd64 and complete contributor/workflow checks; it does not substitute for six-native validation. The consumer workflow takes a full distribution SHA and checks the published bundle.

Use `cache-validation.yml` with a fresh scope for each independent probe. Fix source/toolchain, runner, dependencies, job identity, and settings within each pair. Inspect completed post logs before treating `probe.json` intended `fresh_keys` as saved entries. `expect-hit` requires a complete restore; `expect-miss` requires no match or payload; `expected-match` requires exact reviewed seed keys.

| Case | Required observation |
| --- | --- |
| Cold/warm | Successful seed post-save, then restore-only hit on exact seed keys |
| Restore-only | No save; later restore in the fresh scope remains a clean miss |
| Failed job | Default policy does not save; opted-in save requires successful post and later restore |
| Cancelled job | Actual cancelled conclusion, no save-post execution, later clean miss; wait expiry is inconclusive |
| Concurrent snapshots | Distinct keys, reviewed post results, and valid later restore; reservation conflict alone proves no committed chunk |
| Branch access | Default-branch fallback works; sibling-only seeds remain inaccessible where service rules deny them |
| Restricted access | Actual runner token policy enforces restrictions; environment overrides only test local handling |

Fork/merge-ref behavior requires its real event/token context. Manual dispatch cannot establish it. Never broaden permissions or use `pull_request_target` to bypass a probe. Intentionally failed policy probes retain their failed conclusion and need independent post inspection.

Retain revisions, runner image, Go version, inputs, event/ref, seed/matched keys, job/post conclusions, warnings, and logs. List incomplete/unavailable cases. Ordinary artifacts precede post hooks and cannot collect save reports; use debug logs or a subsequent collector. Synchronous benchmarks capture both phases but exclude runner post dispatch.

## Support and validation status

The source targets Go 1.26+ on Linux/macOS/Windows, amd64/arm64. Caching defaults off; enabled caching defaults to archive/gzip. Self-hosted runners have no guarantee against hostile concurrent filesystem writers.

Contributor checks, native packaged lifecycle fixtures, cross-compilation, hosted service behavior, and measured performance are separate evidence. Run the applicable checks above for each candidate. No controlled end-to-end speedup is established. The [roadmap](../ROADMAP.md) lists remaining release gates.

## Latest published distribution

No distribution is published yet. Consumer examples use `DISTRIBUTION_COMMIT_SHA` as an explicit placeholder. Before recommending a distribution, complete the [release procedure](RELEASES.md), record its release link, distribution commit, source revision, asset digests, compiler, and validation evidence here, and update all example pins together.

| Provenance | Value |
| --- | --- |
| Release | Not published |
| Distribution commit | Not published |
| Source revision | Not published |
