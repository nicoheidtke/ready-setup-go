# Roadmap and performance ideas

Published ecosystem prerelease (2026-11-11): [v0.0.0-preview.20261111](https://github.com/nicoheidtke/setup-go-better/releases/tag/v0.0.0-preview.20261111) points to distribution `952566112acde233b6b762a78ebbceb743605f9a`, built from clean source `137c94d17e338b98b76adb82394e0af4dae08ad4`. All presentation/retry/community features are included. Source/preview/tag refs, 37 committed package files, Unix modes, reproducible six-platform builds and all nine remote asset digests were verified. See [publication evidence](docs/DEVELOPMENT.md). Hosted presentation/native/policy gates and attestations remain pending; GitHub release immutability was not enabled. Earlier local-only and older-preview statements below are historical.

The user also authorized smoothing publication through repository-local Git configuration and tracked tooling. The local checkout now follows the declared LF policy without changing global settings; `.editorconfig` supplies matching editor defaults. A reusable plan/prepare/publish command replaces ignored one-off scripts, with exact-byte manifests, expected-ref checks, atomic ref pushes and verified draft-resume behavior. Full tests/vet on both supported local compilers, 13 Node tests and lint/format checks passed. API recovery was tested with mocks, without another live release or hosted workflow execution.

Publication follow-up (2026-11-11): the user authorized committing/pushing this completed tranche and publishing a clean-source six-platform prerelease, `v0.0.0-preview.20261111`, replacing the existing preview branch. Final source/distribution provenance and asset verification belong in development status. This supersedes the earlier source-only publication restriction; hosted workflow execution, automatic triggers, repository settings/visibility and upstream changes remain outside authorization.

The user authorized sequential agents for the 2026-11-11 presentation and ecosystem improvements. Current local source includes grouped concise logs, status color with `NO_COLOR`, throttled real progress, opt-in debug details, optional main/post job summaries, actionable safe annotations and bounded replay-safe network retries. Cache formats, cache-off/archive/gzip defaults and existing `cache-hit` meaning are preserved. The security, lifecycle, release and community work below completes the implementation scope. Combined Go 1.27.1/1.26.4 tests/vet, 13 Node tests, lint/format checks, local all-six reproducibility and rebuilt Windows archive/chunked main/post/alias/Bash fixtures passed; [development status](docs/DEVELOPMENT.md) references the ignored frozen-source validation report. Hosted Node 24/presentation, other native platforms, service policy gates and release attestations remain unverified. No performance improvement or hosted presentation result is claimed.

Before this publication, the latest bundled preview was `4871ed7af80f1f2eede85493da35d67abbab64cf` from source `7f78716a08f3436082a3fe685ad28a3a981607a6`. It predates the 2026-11-11 source additions. README/usage examples now point to that existing release; the usage reference identifies source-only features. That implementation-stage request did not authorize publication; the later authorization above supersedes it for source/release publication only. Hosted workflow execution, automatic triggers, visibility/settings changes and upstream Exasol modifications remain outside scope.

The user selected the MIT license and GitHub private vulnerability reporting for the 2026-11-11 community tranche. Contribution/support guidance and bug/feature forms are prepared; current packaging requires the license and policy documents. SECURITY.md does not enable private reporting: maintainers must verify eligibility and enable the private form before public adoption. No repository setting or visibility changed. Previews remain best-effort without a stable support/backport promise.

Release preparation now records dirty-versus-clean source provenance, checksums every packaged file and verifies distribution/assets/ZIP modes with `cmd/verify-distribution`. The manual preparation workflow optionally creates build provenance after successful reproducibility/integrity checks; it does not publish releases or change immutability settings. [Release procedures](docs/RELEASES.md) separate local integrity, hosted attestations, immutable release publication and eventual stable aliases. These definitions are locally tested/linted; no hosted attestation or new release was created for the 2026-11-11 ecosystem tranche.

The 2026-11-11 ecosystem tranche extends local lifecycle coverage with coordinated archive/chunk writer races and runner access-policy combinations. Manual validation now prepares restore-only, failed-job, manually cancelled-job and exact seed-key/miss assertions; the six-native integration definition includes packaged archive/chunked main/post fixtures. See [lifecycle procedures](docs/LIFECYCLE_VALIDATION.md). These definitions were linted, not run on GitHub. Hosted branch/default-branch/fork/token boundaries, cancellation post-condition evaluation, the new six-native lifecycle matrix and hosted presentation remain pending explicit execution authorization. No automatic trigger was enabled.

The user authorized two additional small optimizations with separate implementation agents, followed by a bundled prerelease: remember prepared private staging directories and buffer uncompressed tar output (including raw archives used by chunked saves). Preserve cache formats, defaults, validation and publication semantics. Include the existing local publication/compression workers in this release, validate the combined source and reproducible six-platform distribution, and record provenance in docs/DEVELOPMENT.md. No hosted workflow execution or automatic-trigger change is authorized.

Completed publication: [prerelease v0.0.0-preview.20261110](https://github.com/nicoheidtke/setup-go-better/releases/tag/v0.0.0-preview.20261110), distribution `4871ed7af80f1f2eede85493da35d67abbab64cf`, source `7f78716a08f3436082a3fe685ad28a3a981607a6`. Both small optimizations and the previously local worker tranche passed the combined local checks and reproducible builds. The new distribution supersedes earlier preview/unpublished statements below. No hosted run occurred; the small staging diagnostic does not demonstrate complete restore improvement. See [release provenance](docs/DEVELOPMENT.md#published-cache-io-prerelease-2026-11-10).

The user next prioritized local Windows publication and lint packing improvements before more consumer experiments. Bounded independent file-publication and whole-archive gzip-compression workers are implemented and validated locally, with sequential settings retained, no new dependencies, unchanged storage/compression choices and compatible gzip restoration. Synthetic Windows restore median was 2.283s with one worker versus 0.862s with four over five alternating pairs; a 64 MiB full-Pack fixture under GOMAXPROCS=2 measured 384.5ms versus 203.2ms over three pairs with identical encoded bytes. These diagnose local costs, not hosted workflow performance. [Development status](docs/DEVELOPMENT.md#local-publication-and-packing-workers-2026-11-10) records resource bounds and full tests/vet, reproducible cross-builds and native Windows packaged fixtures. The tranche remains uncommitted/unpublished; no hosted workflow dispatch is authorized by this request.

The unchanged improved-preview repeat [42867214965](https://github.com/nicoheidtke/exasol-personal/actions/runs/42867214965) confirms steady module upload skipping across all four populated job caches at the same revision; the 151-byte addition did not recur. Remaining uploads are about 1.52GB of fresh build snapshots. Proceed to an independently cold-seeded and warm-repeated chunked experiment when the user chooses; Windows publication and packing remain local optimization targets. [Recorded evidence](docs/investigation-2026-11-10.md#unchanged-repeat-module-caches-converged) distinguishes transfer reductions from noisy pipeline time.

The user's improved-preview run [42862214728](https://github.com/nicoheidtke/exasol-personal/actions/runs/42862214728) verifies Linux/amd64 and Windows/amd64 archive no-copy publication and lint module skip behavior. [Follow-up observations](docs/investigation-2026-11-10.md#follow-up-improved-preview-consumer-run) prioritize identifying a new 151-byte module file, Windows per-file staging/publication and lint packing. Repeat unchanged settings before comparing a separately seeded/warmed chunked namespace; no new dispatch is authorized by this analysis request. Single-run elapsed reductions remain observational.

The user's next 2026-11-10 request authorizes subagents to implement actionable local improvements and publish a replacement bundled preview for consumer testing. The tranche targets content-based module skip detection before compression, restore filesystem work/metrics and independently bounded archive range downloads. Retain archive/gzip defaults and compatible-cache lookup. Publication is authorized; further GitHub workflow dispatches and automatic triggers are not. Hosted performance conclusions still require measurements at matching workload revisions.

Latest evidence: the [2026-11-10 Exasol investigation](docs/investigation-2026-11-10.md) confirms user-run Linux/amd64 and Windows/amd64 archive caching and useful warm test/build reuse. Initial non-controlled comparison exposes costly lint restore/post work, Windows archive filesystem work and module re-uploads despite unchanged file counts/payload sizes. Investigate these locally before further hosted measurements; no new implementation, dispatch or performance-default decision is implied.

Priority order: **runtime performance, resource efficiency, maintainability**. Measure end-to-end effects rather than assuming a language, archive format, or storage service is faster.

## Milestone 1 â€” Toolchain installation and measurement

Implementation includes a standard-library Go installer; completed-toolchain discovery; exact/minor selection; checksum-verified downloads; staged extraction; composite and Node launchers; six-platform packaging; and automated validation/benchmark definitions.

Validation began on 2026-09-22 with user authorization. Unit tests/vet on all three operating systems and native installer integration on all six platforms passed on GitHub-hosted runners. See [development status](docs/DEVELOPMENT.md) for completed checks; consumer validation and comparative benchmarks are tracked separately.

The primary real-world acceptance workload is [exasol/exasol-personal](https://github.com/exasol/exasol-personal), the project motivating this action. A small pinned dependency smoke test precedes its build, test, and lint workloads. Preserve broader workload coverage for later caching measurements.

Before a stable release (bundled preview commits may be used for consumer validation first):

- Format Go sources, run unit tests and vet, and review workflow syntax.
- Run native integration jobs for all six OS/architecture combinations; report unavailable runners or failures rather than treating cross-compilation as validation.
- Build the distribution twice with the pinned compiler and verify identical executables and archives.
- Run 20 or more paired samples for installed exact/minor versions, latest-patch checks, and cold installation on every target platform.
- Review the actual `setup-go-faster` version's Windows install path and local-cache behavior when interpreting comparisons.
- Compare action acquisition plus execution, not only the installer timer. Record inconclusive results and regressions explicitly.
- Publish a distribution commit containing binaries and preserve executable permissions. Source commits alone are not consumer-ready action distributions.

## Milestone 2 â€” Fresh and efficient CI caches

Milestone 2 source implementation and local correctness gates are complete. Initial Linux/amd64 split/gzip archive cold-save and warm-restore/post-save checks passed on 2026-11-09; the remaining hosted matrix and performance measurements are pending. The scoped implementation plan is [recorded here](docs/IMPLEMENTATION_PLAN.md). Source implementation, local correctness checks, hosted integration, and comparative measurements are separate gates.

Restore/save policy, deterministic archives, and cache-v2 networking stay in Go with standard-library dependencies. GitHub cache is the first backend to measure, not a final performance choice. Cache mode defaults to off; split/combined archives, gzip/none compression, upload concurrency/block size, size bounds, optional build trimming, and failure behavior are explicit controls.

- Give build, lint, test, and matrix variants distinct namespaces. Include workflow/job identity and explicit matrix scope; do not assume the job environment exposes the complete matrix automatically.
- Save fresh snapshots so immutable keys do not preserve the same stale build cache indefinitely. Include run attempt identity to avoid rerun collisions.
- Restore recent compatible snapshots within GitHub's branch access rules, including default-branch fallback where permitted.
- Compare independent module/build archives against a combined archive. Account for extra HTTP requests as well as avoided module uploads.
- Trim old build entries conservatively before upload. Validate Go cache mtime behavior on supported releases before choosing a default grace period.
- Define behavior for failed/cancelled jobs, missing cache credentials, network failures, and concurrent writers. Cache failures should not corrupt usable local data.
- Offer cache-off and restore-only modes. Tiny jobs must not be forced to pay more for transfer than they save in compilation.
- Benchmark small projects, parallel build/test/lint jobs, and multi-module monorepos; include cold runs, unchanged runs, source changes, dependency changes, and Go upgrades.

Lifecycle decision: the root action uses a dependency-free Node 24 launcher to receive runtime cache credentials and automatic post-job hooks, passing execution to Go. This is a functionality requirement, not a measured launcher performance win. `experimental/node` aliases the lifecycle; `experimental/composite` retains the installer-only Bash comparison. The post condition uses a per-invocation `GITHUB_ENV` flag because action inputs are unavailable when the runner evaluates post conditions. Cancelled jobs never save; failed jobs save only when explicitly enabled.

Implemented archive rules validate complete tar/gzip downloads in private staging before merging, reject links and nonportable paths, retain unrelated local entries, and give existing regular files priority. Snapshots use run/attempt/invocation freshness, dependency-matching then dependency-compatible lookup, and explicit matrix scope. Split modules skip unchanged uploads only after deterministic repacking/comparison. Build trimming is archive-only, recognized-object-only, and disabled by default. Service branch/default-branch access remains GitHub's responsibility. See [protocol boundaries](internal/cachebackend/PROTOCOL.md).

The initial real cache-v2 service and automatic post-hook success path is verified on Linux/amd64 with split/gzip archive storage. Remaining gates include native cache tests on the other five platforms, hosted chunked caching, branch/default-branch lookup, failed/cancelled jobs, concurrent snapshots, and the workload/transfer matrix above. Further runs require explicit authorization. No comparative cache or launcher results exist.

## Milestone 3 â€” Improve the measured bottleneck

The user authorized the first self-contained implementation tranche on 2026-11-08 without waiting for hosted measurements: installation/cache prefetch overlap and opt-in chunked snapshots using the existing GitHub cache service. No extra consumer actions, services or infrastructure are introduced. The next user follow-up authorized bounded parallel chunk processing, also implemented locally. Remaining candidates are not promises or predetermined decisions:

| Idea | Potential benefit | What must be measured |
| --- | --- | --- |
| External object storage near runners | Lower latency, controllable transfer layout | Authentication overhead, regional variability, throughput, operating cost |
| Content-defined chunked snapshots (implemented locally, opt-in) | Avoid retransmitting unchanged data | Index cost, compression ratio, request count, garbage collection |
| `GOCACHEPROG` backend | Fetch/store only build objects actually used | Per-object latency, batching, local hits, test-cache behavior |
| Concurrent toolchain installation and cache prefetch (implemented locally, configurable) | Shorter critical path | Whether keys and cache paths can be known early; CPU/disk contention |
| Bounded parallel chunk compression/transfer/decompression (implemented locally) | Overlap per-chunk CPU and network work | Worker memory, request limits, CPU/disk contention |
| Native or parallel archive decompression | Faster cold extraction | CPU time, disk throughput, dependency size, cross-platform consistency |
| Different extraction concurrency | Better Windows/SSD utilization | Peak disk use, filesystem overhead, tail latency |
| Download only the matching installer executable | Smaller distribution transfer | Extra request, verification, CDN behavior, failure rate |
| Platform-specific distributions | Small downloads without extra fetches | Release complexity and user workflow ergonomics |
| Compressed executable distributions | Fewer transferred bytes | Startup decompression, scanning overhead, reproducibility |
| Version metadata caching | Fewer freshness lookups | Explicit freshness contract and stale-data failure behavior |
| Toolchain archive caching | Avoid upstream download bottlenecks | GitHub restore versus official CDN; never assume a second cache helps |
| Better progress instrumentation (implemented locally) | Identify actual bottlenecks and visible long-running work | Instrumentation overhead and hosted presentation |

The implemented chunk format reuses immutable hash-addressed entries, independently compresses chunks, and publishes a fresh bounded manifest after chunks exist. Missing/evicted chunks cause a clean miss; full reassembly and archive validation precede local writes. Chunk workers (default 4, configurable 1-16) compress/transfer unique chunks concurrently and reconstruct a private archive at validated offsets; manifest publication waits for all committed chunks. The unbuffered producer bounds payloads, same-invocation duplicates transfer once, and workers cancel/join on failure. Full streaming save remains deferred because the staged module archive permits unchanged snapshots to skip all chunk requests. Packing still walks the complete local cache; chunking increases request count and temporary raw-tar work. Prefetch overlaps dependency discovery/lookup/download after exact version resolution, while selected-Go path discovery and merge wait for installation. No speedup is claimed.

Do not implement adaptive heuristics, custom toolchain archives, or remote per-object caching merely because they sound faster. Establish the baseline and keep changes only when representative workloads justify them.

## Compatibility additions

- `go-version-file` is implemented locally after the 2026-11-08 user request: explicit `go-version` wins; otherwise go.mod/go.work preferred concrete `toolchain` wins over `go`, `default` falls back to `go`, patch directives remain exact, and older-than-minimum preferred toolchains fail. No implicit file discovery or unsupported selected compiler is accepted. Hosted validation remains pending.
- Explicit self-hosted persistence, ownership, permissions, and concurrent-install guarantees.
- Custom mirrors with a defined integrity model.
- Richer cache outcomes, native annotations, debug metrics and main/post summaries are implemented locally. Optional Go diagnostic problem matchers remain separate work.

Go versions before 1.26, source builds, and arbitrary semver syntax are outside the intended scope.

## Decisions and evidence log

| Decision | Current choice | Evidence/status |
| --- | --- | --- |
| Implementation | Go; standard library installation and caching | Local correctness gates recorded in development status |
| Default launcher | Dependency-free Node 24 bridge | Required runtime credentials/post hooks; performance comparison pending |
| Alternative launcher | Installer-only Bash composite | Retained for paired launcher measurements |
| Distribution | Bundle six native executables | Provisional, acquisition comparison supplied |
| Version preference | Installed matching patch first | User-selected; exact hits make no HTTP calls by design |
| Version inputs | Explicit selector or go.mod/go.work path | File support requested 2026-11-08; explicit input wins; local correctness tests cover directive semantics |
| Cache backend | GitHub cache v2 baseline | Local protocol simulations; hosted compatibility and comparative data pending |
