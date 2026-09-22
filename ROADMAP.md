# Roadmap and performance ideas

Priority order: **runtime performance, resource efficiency, maintainability**. Measure end-to-end effects rather than assuming a language, archive format, or storage service is faster.

## Milestone 1 — Toolchain installation and measurement

Implementation includes a standard-library Go installer; completed-toolchain discovery; exact/minor selection; checksum-verified downloads; staged extraction; composite and Node launchers; six-platform packaging; and automated validation/benchmark definitions.

Validation began on 2026-09-22 with user authorization. See [development status](docs/DEVELOPMENT.md) for completed checks; hosted validation and comparative benchmarks remain pending.

The primary real-world acceptance workload is [exasol/exasol-personal](https://github.com/exasol/exasol-personal), the project motivating this action. A small pinned dependency smoke test precedes its build, test, and lint workloads. Preserve broader workload coverage for later caching measurements.

Before publishing a usable action revision:

- Format Go sources, run unit tests and vet, and review workflow syntax.
- Run native integration jobs for all six OS/architecture combinations; report unavailable runners or failures rather than treating cross-compilation as validation.
- Build the distribution twice with the pinned compiler and verify identical executables and archives.
- Run 20 or more paired samples for installed exact/minor versions, latest-patch checks, and cold installation on every target platform.
- Review the actual `setup-go-faster` version's Windows install path and local-cache behavior when interpreting comparisons.
- Compare action acquisition plus execution, not only the installer timer. Record inconclusive results and regressions explicitly.
- Publish a distribution commit containing binaries and preserve executable permissions. Source commits alone are not consumer-ready action distributions.

## Milestone 2 — Fresh and efficient CI caches

Implement restore and save in Go, independently from launcher choice. Use GitHub cache as the first backend to measure; do not assume it will remain the fastest backend.

- Give build, lint, test, and matrix variants distinct namespaces. Include workflow/job identity and explicit matrix scope; do not assume the job environment exposes the complete matrix automatically.
- Save fresh snapshots so immutable keys do not preserve the same stale build cache indefinitely. Include run attempt identity to avoid rerun collisions.
- Restore recent compatible snapshots within GitHub's branch access rules, including default-branch fallback where permitted.
- Compare independent module/build archives against a combined archive. Account for extra HTTP requests as well as avoided module uploads.
- Trim old build entries conservatively before upload. Validate Go cache mtime behavior on supported releases before choosing a default grace period.
- Define behavior for failed/cancelled jobs, missing cache credentials, network failures, and concurrent writers. Cache failures should not corrupt usable local data.
- Offer cache-off and restore-only modes. Tiny jobs must not be forced to pay more for transfer than they save in compilation.
- Benchmark small projects, parallel build/test/lint jobs, and multi-module monorepos; include cold runs, unchanged runs, source changes, dependency changes, and Go upgrades.

Lifecycle decision: the GitHub runner exposes automatic post-job hooks and cache credentials to JavaScript actions. A dependency-free launcher can pass them to the Go process. Composite shell steps do not receive the same credential injection. Explicit restore/save steps alone do not solve that distinction. Recheck runner behavior before implementing this milestone.

## Milestone 3 — Improve the measured bottleneck

Candidate experiments, not promises or predetermined decisions:

| Idea | Potential benefit | What must be measured |
| --- | --- | --- |
| External object storage near runners | Lower latency, controllable transfer layout | Authentication overhead, regional variability, throughput, operating cost |
| Chunked or incremental snapshots | Avoid retransmitting unchanged data | Index cost, compression ratio, request count, garbage collection |
| `GOCACHEPROG` backend | Fetch/store only build objects actually used | Per-object latency, batching, local hits, test-cache behavior |
| Concurrent toolchain installation and cache restore | Shorter critical path | Whether keys and cache paths can be known early; CPU/disk contention |
| Native or parallel archive decompression | Faster cold extraction | CPU time, disk throughput, dependency size, cross-platform consistency |
| Different extraction concurrency | Better Windows/SSD utilization | Peak disk use, filesystem overhead, tail latency |
| Download only the matching installer executable | Smaller distribution transfer | Extra request, verification, CDN behavior, failure rate |
| Platform-specific distributions | Small downloads without extra fetches | Release complexity and user workflow ergonomics |
| Compressed executable distributions | Fewer transferred bytes | Startup decompression, scanning overhead, reproducibility |
| Version metadata caching | Fewer freshness lookups | Explicit freshness contract and stale-data failure behavior |
| Toolchain archive caching | Avoid upstream download bottlenecks | GitHub restore versus official CDN; never assume a second cache helps |
| Better progress instrumentation | Identify actual bottlenecks | Instrumentation overhead itself |

Do not implement adaptive heuristics, custom toolchain archives, or remote per-object caching merely because they sound faster. Establish the baseline and keep changes only when representative workloads justify them.

## Compatibility additions

- `go-version-file` with deliberate `go`/`toolchain` directive semantics.
- Explicit self-hosted persistence, ownership, permissions, and concurrent-install guarantees.
- Custom mirrors with a defined integrity model.
- Optional problem matchers and richer diagnostic outputs.

Go versions before 1.26, source builds, and arbitrary semver syntax are outside the intended scope.

## Decisions and evidence log

| Decision | Current choice | Evidence/status |
| --- | --- | --- |
| Implementation | Go; standard library installer | Local Windows validation started; see development status |
| Default launcher | Composite with minimal Bash | Provisional, pending paired measurements |
| Alternative launcher | Node with built-in process/path APIs only | Performance comparison pending |
| Distribution | Bundle six native executables | Provisional, acquisition comparison supplied |
| Version preference | Installed matching patch first | User-selected; exact hits make no HTTP calls by design |
| First public interface | Explicit version only | User-selected |
| Cache backend | Deferred | No comparative data yet |
