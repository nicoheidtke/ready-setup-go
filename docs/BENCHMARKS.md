# Benchmark methodology

The 2026-11-10 preview work adds independent `cache-download-concurrency` (default 4, range 1-32) and `cache-download-chunk-bytes` (default 32 MiB). Concurrency 1 retains the single-GET path. The first range response establishes object size; a server returning a full 200 response is consumed directly. Chunked storage retains its separate worker bound. Compare request count, bytes and elapsed download time rather than assuming more workers improve all workloads.

Split module saves now fingerprint paths, permissions and every file's bytes before packing, ignoring mtimes. Reports expose `module_check_ms` and `save_reason`; unchanged modules skip archive creation/compression and upload. Changed modules pay the fingerprint pass as well as packing. This changes the cost boundary from the older repack-and-compare implementation described in historical records.

Restore reports split `archive_ms` into `archive_stage_ms`, `archive_preflight_ms`, `archive_merge_ms` and `archive_cleanup_ms`, with published/linked file counts and copied payload bytes. These are native wall-time intervals, not independent CPU measurements. Destination-volume private staging and no-replace hardlink publication can avoid the second payload copy; unsupported filesystems retain the exclusive-copy path. Measure actual counters and timing on each runner before making a speed claim.

The [2026-11-10 Exasol investigation](investigation-2026-11-10.md) records initial observational comparisons from user-supplied cold/warm consumer runs and an existing upstream run. Different workload/dependency revisions and one sample per case prevent treating these as controlled benchmark results. Warm aggregate job time and pipeline critical-path time moved in different directions; preserve both measures in future comparisons.

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

The manually dispatched **Manual runtime benchmarks** workflow runs on a selected hosted platform. It resolves the `actions/setup-go` v7.0.0 release and `WillAbides/setup-go-faster` main commits once, checks out those exact revisions, and invokes their local actions alongside the packaged implementation. Update the official comparison release deliberately when modernizing action dependencies; retain its resolved SHA in the result metadata.

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

For cache measurements, include restore, compile/test, archive-only filtering/packing, and upload in the measured job. Extend workloads to a small module, parallel build/test/lint jobs, and a multi-module monorepo. Installer speed alone cannot establish cache efficiency.

## Primary real-world workload

Use [exasol/exasol-personal](https://github.com/exasol/exasol-personal) as the primary acceptance project. The initial pinned revision is `54b7e1a7454f49d821102bcab5bfce2c7747f8c6`; record any revision change alongside results. First run the small `testdata/packages` workload using pinned `github.com/google/uuid` and `golang.org/x/sync` versions, with normal Go checksum verification.

`.github/workflows/workload.yml` validates separate Exasol Go build, unit-test, and lint jobs using the packaged installer. It follows the upstream generation mode for tests (`SKIP_EMBED=true`), retains the required `containers_image_openpgp` tag, and runs Linux unit tests with the race detector. Build jobs use CGO disabled and the upstream no-inlining setting. Generated placeholder-resource binaries are compilation checks, not distributable Exasol releases. Python integration tests and deployment tests are separate acceptance stages; cloud/VM provisioning is not part of this Go workload.

The original installer-only workload is a correctness gate, not a performance comparison: packaging uses a bootstrap Go installation and there is only one observation per job. Cache correctness and cache measurements use separate workflow definitions. Its module/build cache directories are isolated from the bootstrap and package smoke test.

For the later cache comparison, preserve Exasol's actual parallel build/test/lint split and account for its current setup action's module and tool warmup (`go mod download`, golangci-lint, tflint and plugin initialization). Measure the complete job before removing or changing that warmup. Include tool caches and resource-generation costs explicitly. Compare cold, unchanged, source-change, dependency-change, and Go-upgrade runs at fixed revisions; avoid drawing cache conclusions from the installer timer or a single local run.


## Cache correctness and experiment matrix

The local cache implementation and workflow definitions are preparation for hosted measurements. The user authorized source commits/pushes on 2026-11-08 after all workflows were made manual-only to avoid PoC costs. Do not dispatch/rerun workflows or restore automatic triggers without authorization. Publishing a replacement bundled distribution remains a separate step. No cache, launcher or acquisition speedup has been established.

Correctness must precede performance collection. The gate covers the real root Node action and its automatic post hook, subsequent restoration of saved files, isolated module/build roots, exact selected Go version and dependency compatibility, distinct job/matrix scopes, failed/cancelled jobs, missing/denied credentials, malformed downloads, immutable-key races and branch/default-branch access. Unit fixtures exercise protocol and archive failures locally; they do not prove service compatibility or native Unix behavior.

Use the same selected Go toolchain, checkout, cache paths and workload commands for each paired comparison. Capture exact action/compiler/workload revisions, runner image, OS/architecture, compression/layout and transfer settings. Restore-only, cache-off and restore-save are explicit modes. Baselines must retain their own cache inputs/settings in the record; an installer-only run cannot demonstrate cache efficiency.

| Dimension | Required cases |
| --- | --- |
| Workload | Small pinned dependency smoke module; actual parallel build/test/lint jobs; multi-module monorepo; Exasol build/test/lint at the recorded revision |
| Cache state | Cold, unchanged, source change, dependency change, exact Go upgrade |
| Storage layout | Split module/build archives and combined archive |
| Compression | gzip and none |
| Transfer | Explicit upload worker and chunk size values; record requests and payload where instrumented |
| Retention | No build filtering baseline; explicit conservative age cutoffs only after Go mtime behavior is validated |
| Lifecycle | Success, restore-only, missing credentials/write denial, failed/cancelled job behavior |
| Platform | Native hosted Linux/macOS/Windows, amd64/arm64; unsupported or unavailable runner cases remain visible |

Collect at least 20 successful paired samples for each comparison and reverse pair order on alternate samples. A single success is a correctness observation. Include failures rather than silently discarding them; groups below the required sample count remain incomplete. Isolate pair/sample namespaces so earlier measurements cannot warm later cold cases. Seed warm/change scenarios outside the measured interval, recording seed settings and selected Go version. Go-upgrade cases must actually seed with a different exact version; changing only a label is invalid.

The initial cache backend is GitHub cache v2; it is a measurement candidate, not an architectural performance conclusion. Split snapshots add lookup/upload operations but can avoid unchanged module uploads. Unchanged-module detection still packs and hashes the local module tree, so include its CPU/disk time even when transferred bytes are zero. Compression similarly trades transfer against local work; no default should be called optimal before representative measurements.

Dependency changes first try the exact fingerprint and then a dependency-compatible prefix within the same scope, OS/architecture, Go version, layout and compression. Source changes need fresh build snapshots even when dependency files do not change. Record the returned key and archive kind; `cache-hit` means every requested archive restored and is not a promise that the entire workload avoids compilation.

## Cache metric boundaries

Cache reports distinguish `preparation_ms` (selected-Go cache path discovery and dependency fingerprinting), phase `total_ms`, per-archive `lookup_ms`, `transfer_ms`, `archive_ms`, `transferred_bytes`, `archive_files` and `archive_payload_bytes`. Restore total excludes preparation; compute the full setup cache contribution by including both. Transfer time includes the backend operation rather than a pure network-throughput estimate. Archive processing includes validation, decompression/staging/merge on restore and walking/filtering/packing/hash work on save.

Transferred bytes count HTTP payload rather than HTTP/TLS overhead. Archive payload counts regular-file content, including entries skipped because a local file already exists. The action's maximum independently bounds compressed transfer and expanded tar; it does not measure peak disk allocation. Restore temporarily retains a downloaded archive plus staged payload, while save temporarily retains a packed archive. Filesystem block rounding, metadata, signed-service storage and full peak temporary bytes need separate instrumentation before publishing resource totals.

Report paired total workload cost as restore preparation + restore + workload + post save, with medians/p95 and individual observations. Action acquisition and runner scheduling remain additional costs; the acquisition experiment and job-step API timestamps have the limitations described above. Do not subtract coarse API timings to manufacture precise cache throughput.

The real action saves in its automatic post hook. A regular upload-artifact step runs before post and can collect the restore report only. The save report is written later at `<cache-report>.post.json` and printed in post logs. A later collector may inspect completed jobs/logs; an explicit-lifecycle benchmark harness can instead write restore and save metrics before its artifact upload, but must identify that it does not measure GitHub post dispatch overhead. Never describe a restore-only artifact as complete cache lifecycle evidence.

## Prepared cache tools and workflows

`cache-validation.yml` defines **Manual cache lifecycle validation**. After hosted execution is authorized, run it once to exercise restore, the pinned package workload and the real automatic post-save. Run a second dispatch with the same runner, Go version, layout, compression and scope, setting `expect-hit` to true. Job identity remains the same across dispatches, so the second run can validate the previous snapshot without weakening workload isolation. This two-dispatch gate validates the normal success path; failed/cancelled/branch/concurrency cases remain additional integration gates.

`cache-benchmark.yml` defines **Manual cache benchmarks**, with runner, workload, scenario, samples, exact target/seed Go versions, layout, compression, candidate mode, exact dependency mutation, maximum bytes, upload concurrency, upload chunk bytes and optional build-age cutoff inputs. On Linux, workload `exasol` creates separate parallel build/test/lint jobs; on Windows it selects build only. Exasol is rejected on macOS, where `all` selects the small module and generated multi-module workspace. On supported platforms, `all` also includes the available Exasol jobs. The default minimum is 20 pairs. For `go-upgrade`, explicitly select different exact seed and target versions; the helper rejects equal versions.

The workflow invokes `.github/actions/cache-benchmark`, a dependency-free Node credential bridge, to run `cmd/cache-benchmark`. It uses synchronous Go restore/save so its JSON artifact includes both operations before artifact upload. It does not measure automatic runner post dispatch. The helper accepts:

```text
--workload small|monorepo|exasol-build|exasol-test|exasol-lint
--scenario cold|unchanged|source|dependency|go-upgrade
--source PATH --go GO_EXECUTABLE --seed-go SEED_GO_EXECUTABLE
--samples 20 --mode restore-save|restore --layout split|combined
--compression gzip|none --max-bytes BYTES
--upload-concurrency WORKERS --upload-chunk-bytes BYTES
--build-max-age DURATION --out cache-reports/observations.json
```

The helper requires the injected runtime cache credentials; an ordinary unauthenticated local shell is not a live-cache benchmark. `--validation` permits fewer observations for correctness only and suppresses a performance-complete summary. `--dependency MODULE@VERSION` (or separate `--dependency-module` and `--dependency-version`) configures the exact dependency mutation outside measurement. The mutation must change the dependency fingerprint. Each pair has a distinct remote namespace and an unmeasured seed where appropriate; local caches reset before each variant. This consumes remote cache storage and the helper does not delete repository caches.

Reports retain seeds, all commands and exit codes, failed samples, individual cache phases, selected exact Go versions, workload/source/action revisions, runner image, configuration, pair order, and paired delta summaries. Workload output logs are stored beside JSON. OS page caches, upstream module proxies and CPU scheduling are uncontrolled and must be considered when interpreting close results.

Both manual workflows define a later report job to retrieve completed job/post-step timing through `cmd/workflow-report`. These timestamps are coarse and do not recover native per-phase JSON from an already finished machine; post logs remain the source for full save-report JSON.

The Exasol harness measures `go mod download`, generation, the selected build/test/lint task and available tool warmup. Optional `golangci-lint version`, `tflint --version` and initialization outcomes remain in raw command records, including optional failures. Lint/plugin cache directories are isolated and cold; they are not included in the Go module/build archives. Task installation, upstream's separate tool-cache restore/save, Python/integration/deploy work and runner post dispatch are excluded. This is an explicit Go workload/cache experiment, not proof of complete upstream-job equivalence. Preserve those exclusions when comparing to Exasol's current setup/warmup.

## Overlap and chunked-storage boundaries

The 2026-11-08 self-contained tranche adds `cache-overlap` (default true) and `cache-storage` (`archive` default, `chunked` opt-in). Disable overlap for sequential comparisons. Restore `total_ms` counts prefetch lookup/download work plus later archive validation/merge; `preparation_ms` adds dependency preparation and selected-Go path discovery but excludes time waiting to join the prefetch worker. `prefetch_wait_ms` records that join wait after path discovery. These cache-work times may overlap installer work: adding installer time and cache totals does not represent end-to-end setup elapsed time. The synchronous cache benchmark helper accepts `--storage archive|chunked` and does not exercise installation overlap.

Chunked `transferred_bytes` includes uploaded/downloaded manifests and chunks; `reused_chunks` and `reused_bytes` count existing encoded chunk content skipped on save (not HTTP overhead or actual server storage usage). Chunk transfer time includes content-boundary/hash/compression work and chunk control requests. Local packing is still complete and save/restore retains a full uncompressed tar plus bounded chunk buffers. Overlapped split prefetch can retain both archives before merge. Chunk requests/encoding, independent chunk eviction and cache quota/entry pressure must remain visible in later comparisons. No performance result is established by implementation or reuse correctness tests.

## Bounded chunk workers

`cache-chunk-concurrency` (and the synchronous helper's `--chunk-concurrency`) independently selects 1-16 chunk workers, default 4. Chunk blob operations use one block worker each so this does not multiply `cache-upload-concurrency`. Save compression/hash/lookup/upload and restore lookup/download/decompression can overlap. Transfer phase time is elapsed wall time for the pipeline, not a sum of worker durations. Metrics are reduced only after workers join. Invocation-local duplicate chunks count once as payload transfer and as reused occurrences thereafter.

Complete local packing and a raw save archive remain; this preserves unchanged-module saves that skip every chunk lookup. Restore retains one private raw archive with bounded concurrent buffers and validated offset writes, not a growing in-memory reorder queue. A conservative chunk-buffer allowance is approximately 32 MiB per worker plus 16 MiB for the save producer, in addition to gzip/runtime/filesystem overhead. This is an allocation planning bound, not measured total resident memory. Worker settings do not affect cache compatibility or content-defined boundaries. Correctness tests exercise concurrency/ordering/cancellation; no performance result is implied.
