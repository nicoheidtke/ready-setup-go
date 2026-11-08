# Roadmap and performance ideas

Priority order: **runtime performance, resource efficiency, maintainability**. Measure end-to-end effects rather than assuming a language, archive format, or storage service is faster.

## Milestone 1 — Toolchain installation and measurement

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

## Milestone 2 — Fresh and efficient CI caches

Milestone 2 source implementation and local correctness gates are complete; hosted integration and performance measurements remain pending. The scoped implementation plan is [recorded here](docs/IMPLEMENTATION_PLAN.md). Source implementation, local correctness checks, hosted integration, and comparative measurements are separate gates.

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

Remaining validation gates after local implementation: hosted cache-v2 compatibility, actual post-hook behavior, native tests on all six platforms, branch/default-branch lookup, failed/cancelled jobs, concurrent snapshots, and the workload/transfer matrix above. Do not run them until the user authorizes hosted execution. No comparative cache or launcher results exist.

## Milestone 3 — Improve the measured bottleneck

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
| Better progress instrumentation | Identify actual bottlenecks | Instrumentation overhead itself |

The implemented chunk format reuses immutable hash-addressed entries, independently compresses chunks, and publishes a fresh bounded manifest after chunks exist. Missing/evicted chunks cause a clean miss; full reassembly and archive validation precede local writes. Chunk workers (default 4, configurable 1-16) compress/transfer unique chunks concurrently and reconstruct a private archive at validated offsets; manifest publication waits for all committed chunks. The unbuffered producer bounds payloads, same-invocation duplicates transfer once, and workers cancel/join on failure. Full streaming save remains deferred because the staged module archive permits unchanged snapshots to skip all chunk requests. Packing still walks the complete local cache; chunking increases request count and temporary raw-tar work. Prefetch overlaps dependency discovery/lookup/download after exact version resolution, while selected-Go path discovery and merge wait for installation. No speedup is claimed.

Do not implement adaptive heuristics, custom toolchain archives, or remote per-object caching merely because they sound faster. Establish the baseline and keep changes only when representative workloads justify them.

## Compatibility additions

- `go-version-file` is implemented locally after the 2026-11-08 user request: explicit `go-version` wins; otherwise go.mod/go.work preferred concrete `toolchain` wins over `go`, `default` falls back to `go`, patch directives remain exact, and older-than-minimum preferred toolchains fail. No implicit file discovery or unsupported selected compiler is accepted. Hosted validation remains pending.
- Explicit self-hosted persistence, ownership, permissions, and concurrent-install guarantees.
- Custom mirrors with a defined integrity model.
- Optional problem matchers and richer diagnostic outputs.

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
