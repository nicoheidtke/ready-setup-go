# Development

Start with [CONTRIBUTING.md](../CONTRIBUTING.md) for prerequisites, checks, and change expectations. This guide covers integration and current validation boundaries. [Release procedures](RELEASES.md) cover packaging/publication; [measurement methodology](BENCHMARKS.md) covers performance evidence.

## Source layout

| Path | Responsibility |
| --- | --- |
| `cmd/setup-go-better` | Native setup and post-job entrypoints |
| `internal/installer` | Version selection, discovery, verified download/extraction, and installation |
| `internal/cache` | Inputs, identity, restore/save policy, private post state, and reports |
| `internal/cachearchive` | Deterministic tar/gzip, staging, and additive publication |
| `internal/cachebackend` | Cache-v2 and signed blob transfers; see [protocol](../internal/cachebackend/PROTOCOL.md) |
| `internal/actionenv`, `internal/actionlog`, `internal/httpretry` | Runner protocols, presentation, and replay-safe retries |
| `internal/distribution`, `internal/release` | Provenance, integrity, and publication |
| `cmd/package`, `cmd/verify-distribution`, `cmd/release` | Distribution/release commands |
| `cmd/check`, `cmd/check-docs` | Contributor checks and documentation consistency |
| Other `cmd/` tools | Installer, acquisition, cache, and workflow measurements |
| Root `.cjs` files | Dependency-free Node 24 runner bridge |
| `experimental/` | Node alias and installer-only Bash comparison |
| `testdata/`, `.github/` | Workloads, fixtures, and manual workflows |

Runtime Go code uses the standard library. Consumers use bundled executables without building the action or installing npm packages. Source-only revisions cannot run as actions.

## Maintained interfaces

The root `action.yml` is the recommended consumer entrypoint. `experimental/node` remains a compatibility alias for the same main/post lifecycle. `experimental/composite` and `launch.sh` are installer-only comparison paths; they do not provide the cache lifecycle. Integration fixtures and launcher benchmarks still exercise these paths, so changing or removing them requires updating that coverage and assessing compatibility.

Measurement commands are maintainer tools, separate from the action's public inputs and outputs. Their purposes and measurement limits are listed in [performance measurement](BENCHMARKS.md#tools). Keep generated distributions and raw reports in ignored output directories.

## Validation

Release builds use Go 1.27.1. Also check the Go 1.26 line and use Node 24 to verify the action runtime. Run contribution checks after changes; native integration and hosted service checks cover additional boundaries.

### Local packaged lifecycle

Package the native platform into an ignored directory (substitute the host OS/architecture):

```text
go run ./cmd/package --platform windows/amd64 --out .cache/native-dist
```

`TestPackagedLifecycle` consumes a prebuilt distribution and an installed toolchain; it never downloads Go or compiles the action. Set these environment variables:

| Variable | Value |
| --- | --- |
| `SETUP_GO_BETTER_TEST_DIST` | Absolute distribution directory |
| `SETUP_GO_BETTER_TEST_RUNNER_TEMP` | Parent of `setup-go-better/toolchains/<version>/<os>-<arch>/bin/go[.exe]` |
| `SETUP_GO_BETTER_TEST_VERSION` | Installed exact version; default `1.26.4` |
| `SETUP_GO_BETTER_TEST_STORAGE` | Run once with `archive`, once with `chunked` |
| `SETUP_GO_BETTER_TEST_NODE` | Node 24 executable; default `node` |
| `SETUP_GO_BETTER_TEST_BASH` | Optional Bash executable for installer-only reuse |

```text
go test ./internal/cache -run '^TestPackagedLifecycle$' -count=1 -v
```

The test exercises root main/post, the Node alias, reports/state/summaries, real compilation, and offline Bash reuse against a loopback cache-v2 fixture. Without the distribution variable it is skipped. Unit fixtures cover archive safety, existing-file priority, read-only modules, cancellation/worker cleanup, access-policy changes, and forced immutable-writer races.

### Hosted lifecycle

Workflows are manual; maintainers control execution and costs. Native integration checks all six targets. The consumer workflow takes a full distribution SHA and checks the published bundle.

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

This prerelease targets Go 1.26+ on hosted Linux/macOS/Windows, amd64/arm64. Caching defaults off; enabled caching defaults to archive/gzip. Self-hosted runners have no guarantee against hostile concurrent filesystem writers.

| Area | Recorded evidence and limits |
| --- | --- |
| Latest release source | Tests/vet on Go 1.27.1/1.26.4, 13 Node tests on Node 26.3.0, formatting, actionlint 1.7.12 passed |
| Integrity | All six binaries and both ZIPs reproduced; committed bytes, Unix modes, and nine uploaded asset digests verified |
| Native packaged caching | Windows archive/chunked main/post/alias/Bash passed locally; cross-compilation proves no other native behavior |
| Hosted service | Earlier Linux archive cold/warm smoke and Linux/Windows amd64 consumer archive runs passed on earlier distributions |
| Remaining gates | Current Node 24/presentation, six-native lifecycle, hosted chunked, branch/fork/token policies, failure/cancellation, service concurrency |
| Performance | No controlled end-to-end speedup established |

Evidence belongs to its revision. Rerun relevant checks after changes; earlier hosted runs cannot verify new behavior. The [roadmap](../ROADMAP.md) lists remaining release gates.

## Latest published distribution

[v0.0.0-preview.20261111](https://github.com/nicoheidtke/setup-go-better/releases/tag/v0.0.0-preview.20261111) identifies distribution `952566112acde233b6b762a78ebbceb743605f9a`, reproducibly built from clean source `137c94d17e338b98b76adb82394e0af4dae08ad4` with Go 1.27.1. Later source changes do not change that bundle.

| Asset | SHA-256 |
| --- | --- |
| Bundled ZIP | `2441cd157659791e6533ec292a0179059d7645d48db1bced953ea98cfd7d671f` |
| Launcher ZIP | `fda6af11196ca5af98a8cdfa3bfb9e9df628941b7856153ef060015763e5026c` |

Source, preview branch, and annotated tag were published atomically. Commit IDs identify immutable content; the release reported `immutable: false`, so tag/assets are not claimed locked. No hosted build attestation was generated. Release-tool API recovery was tested with mocks rather than a second live publication.
