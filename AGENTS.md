# Project handoff

Current authorization (2026-11-10 follow-up): the user explicitly requested subagents to implement the actionable investigation improvements and publish a new bundled preview for the user to try. This supersedes the investigation-only implementation/publication restriction below. Local implementation, correctness checks, source publication and a replacement six-binary preview are authorized. Do not dispatch/rerun GitHub workflows, restore automatic triggers, change visibility or modify the upstream Exasol repository. Keep compression/storage choices configurable and record local versus hosted evidence separately.

The delegated improvements implement full-content module checks before packing (ignoring mtimes), destination-volume private restore staging and no-replace hardlink publication with copy fallback, phase/copy metrics and bounded configurable whole-archive ranges (default four 32 MiB workers; concurrency 1 retains single GET). Cache namespaces and archive/gzip defaults remain compatible. Full tests/vet passed with local Go 1.27.1 and 1.26.4; nine Node tests and actionlint passed. The six-platform candidate cross-built and Windows archive/chunked packaged main/post/alias/Bash fixtures passed, including multi-range restore. New hosted behavior is unverified; final reproducibility/publication provenance belongs in docs/DEVELOPMENT.md. Prefetch transfer/merge pipelining and compression/storage default changes remain deferred.

Published improvement preview: `e6e119a8b0b524a6ac1606d660c48603c11753df`, source `232c3fe2d2d729a67acb08e4cc725181215bdf1c`, on existing `codex/action-preview` and `main` branches respectively. All six binaries and both ZIP variants reproduced identically across two builds; committed binary bytes and Unix executable modes were verified. This replaces the older preview as the user-facing candidate. No new hosted run occurred. The user additionally requested a concise visitor-focused README with links to complete usage, architecture, comparison and development/evidence documents; keep the detailed reference material outside the README.

Latest evidence (2026-11-10): the user supplied independently executed Exasol cold/warm consumer runs using preview `13f611572c059e38af11315888f7add1566bbb2e`. Existing logs confirm Linux/amd64 and Windows/amd64 split/gzip archive restore/post-save and useful build/test reuse. See [the investigation](docs/investigation-2026-11-10.md) for actual timings, redundant module uploads and restore bottlenecks. The upstream comparison uses a different workload/dependency revision and is observational, not a controlled benchmark. Earlier statements below about unverified Windows hosted caching and absent comparisons predate this evidence; chunked storage and other policy/platform gates remain unverified. This request authorizes investigation of existing runs, not further dispatches, implementation/publication or upstream changes.

Read [ROADMAP.md](ROADMAP.md), [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md), and [docs/BENCHMARKS.md](docs/BENCHMARKS.md) before changing the implementation. The README is consumer documentation, not a development-status log.

## Goal and priorities

Build `setup-go-better`, a fast Go setup action for GitHub Actions, combining efficient toolchain installation with fresh and efficient build/module caching.

Priorities, in order:

1. Runtime performance: minimize total workflow cost, including action acquisition, setup, and cache restore/save.
2. Resource efficiency: transferred bytes, storage, CPU, and disk work.
3. Maintainability.

Ease of implementation must not override runtime performance. Prefer measured results to assumptions about languages or backends. Do not claim speedups without evidence.

## Accepted decisions

- Finish local implementation before further hosted testing. On 2026-11-08 the user authorized the first self-contained performance tranche: overlap toolchain installation with cache prefetch and optional content-defined chunk reuse on the existing GitHub cache service. No third-party consumer actions or extra infrastructure. The next user follow-up authorized bounded parallel chunk processing inside the same action. Other milestone 3 experiments and compatibility additions remain separate work.
- Target GitHub-hosted Linux, macOS, and Windows, each on amd64 and arm64. All three operating systems are in initial scope.
- Support Go 1.x starting at 1.26, with exact versions and minor selectors. Prefer installed matching patches; `check-latest` opts into a freshness lookup. The user requested `go-version-file` support on 2026-11-08: accept go.mod/go.work, explicit version precedence, preferred concrete toolchain then go directive, with exact patches preserved. Selected compilers must be 1.26+; older language minima are allowed with supported preferred toolchains.
- Keep implementation in Go with minimal dependencies. The user dislikes substantial Bash/Perl and JS/TypeScript logic. Minimal launcher glue is acceptable as an experiment, with runtime costs measured.
- Use the dependency-free Node root launcher for runtime credentials and automatic post-job cache hooks. Keep `experimental/composite` installer-only for Bash/Node comparisons and `experimental/node` as an alias. Neither launcher has won on performance.
- Bundle executables initially, while measuring bundled acquisition against downloading only the matching binary. Do not compile the installer on consumer runners.
- Treat launcher and storage choices independently. External storage remains a future performance candidate, not an initial requirement. GitHub cache is not a final backend decision.
- Cover small projects, parallel build/test/lint jobs, and monorepos in eventual cache benchmarks; do not arbitrarily narrow the audience to one workload size.
- Use `exasol/exasol-personal` as the primary real-world acceptance workload: this action is being developed for that project. Start with a small smoke test using pinned well-known packages, then validate its build/test/lint workloads at a recorded commit.

## Current state and execution constraint

Milestone 1 and milestone 2 installation/cache source implementation, tests, launchers, packaging, and workflow definitions are complete locally. Final frozen-source tests/vet passed with pinned Go 1.27.1; all six binaries cross-built reproducibly, and native Windows packaged main/post caching passed against a local protocol fixture. Initial hosted Linux/amd64 split-gzip archive save/restore and automatic post hooks passed on 2026-11-09. Other platforms, chunked hosted caching, branch/failure/concurrency cases and comparative measurements remain unverified. See [the scoped implementation plan](docs/IMPLEMENTATION_PLAN.md). Local validation started on 2026-09-22; see docs/DEVELOPMENT.md for verified results and remaining checks. No comparative performance results exist.

On 2026-09-22 the user authorized proceeding with validation, identifying `exasol/exasol-personal` as the ultimate test after small package smoke tests. This supersedes the earlier implementation-only restriction. The user subsequently authorized the initial commit and push to `nicoheidtke/setup-go-better` and wants to consume the action in a workflow. That historical authorization produced the existing bundled preview. On 2026-11-08 the user authorized committing and pushing the current source after making every workflow manual-only; a replacement bundled preview remains a separate publication step. A stable release still requires the roadmap's measurements. Changes to the upstream Exasol repository remain outside this task.

The latest user direction additionally authorizes self-contained performance implementation, beginning with overlap and chunked snapshots, while keeping choices configurable. This supersedes the earlier milestone 3 implementation deferral for those features and the subsequently requested bounded parallel chunk processing. The prior explicit constraint remains: **"dont run any further workflows on github yet. just finished the implementation side first"**. All repository workflows now use only `workflow_dispatch` to avoid GitHub Actions costs during the PoC. The user authorized committing and pushing source with these manual-only triggers on 2026-11-08. Do not dispatch or rerun workflows, or restore automatic triggers, until explicitly authorized. Already-started runs may finish. The initial bundled preview remains historical installer-only code. The latest source publication includes workflow modernization, cache implementation, self-contained performance changes and manual correctness/benchmark definitions; source revisions do not include generated executables.

Chunked storage now has independently bounded `cache-chunk-concurrency` (default 4, range 1-16), duplicate suppression, ordered private-archive assembly and cancellation/join cleanup. It still retains complete local packing and an uncompressed save archive to preserve unchanged-module skipping. The parallel-chunk source passed pinned-Go full tests/vet, Node tests, workflow lint and packaged Windows archive/chunked lifecycle checks; refreshed all-six local candidates are recorded in development status. Cache prefetch overlap defaults to enabled when caching is requested; `cache-overlap: false` retains sequential execution. `cache-storage: chunked` opts into content-defined immutable chunk reuse and independently compressed chunks, with full validation before merge. Whole-archive storage remains the default. The latest performance tranche passed local tests/vet, Node checks, actionlint, and packaged Windows archive/chunked lifecycles; regenerated local distributions include it. Earlier frozen-source results remain historical. Cache defaults to off; restore-only and restore-save are explicit. GitHub cache-v2 is the first measurement backend, not a permanent backend decision. Snapshot namespaces include explicit matrix scope, keys are fresh per attempt/invocation, and module/build split versus combined and gzip versus none remain configurable. Archive-only build trimming is disabled by default. Existing local cache files win; corrupt restores never replace usable local data. See internal/cachebackend/PROTOCOL.md for protocol evidence and limitations.

Continue with correctness checks before hosted integration or performance measurements. Do not skip correctness checks because the README is written as product documentation. Source revisions lack tracked generated binaries; release distributions must include them.

## Documentation upkeep

- Keep README.md focused on what the action does and how users use it, written as product documentation. Keep ongoing efforts and unverified development status out of it.
- Keep goals, decisions, deferred ideas, and remaining work in ROADMAP.md.
- Keep build/release instructions and validation status in docs/DEVELOPMENT.md.
- Keep measurement definitions and limitations in docs/BENCHMARKS.md.
- Update this handoff and the relevant documents when decisions, execution constraints, or validation status change. Distinguish implemented code from verified behavior.

## Original references

- Discussion: https://news.ycombinator.com/item?id=49726350
- Installation ideas: https://github.com/WillAbides/setup-go-faster
- Cache freshness/isolation ideas: https://github.com/cloudx-io/setup-go

The upstream projects optimize different parts of CI. Their historical benchmark claims are not evidence for this project's performance on current runners.

## Current preview/smoke authorization (2026-11-09)

The user explicitly authorized publishing a fresh bundled preview and running one Linux pinned-package consumer smoke test against its immutable SHA. All workflows remain manual-only; the consumer workflow selects one runner per dispatch and defaults to ubuntu-24.04. The user then explicitly authorized step 3: one warm-cache repeat on the same Linux runner and distribution SHA after a successful save. This exception does not authorize the full platform matrix, benchmarks, Exasol runs, automatic triggers, or visibility changes. Follow the authorized run to completion and record its actual result; troubleshoot and fix correctness failures within that run scope.

The authorized 2026-11-09 smoke cycle is complete. Current bundled preview is `13f611572c059e38af11315888f7add1566bbb2e`, built from source `d1187a834d030b7e53cb8e8b270831da4aaa1a9c`. An initial failed run exposed truncated cache-service version hashes; the corrected preview passed cold save run 42534290287 and warm restore/save run 42534400908 on ubuntu-24.04 with Go 1.26.0 from go.mod, default split/gzip archive storage and strict cache errors. Both archives restored; unchanged modules skipped re-upload and a fresh build snapshot saved. This does not validate other platforms, chunked hosted behavior or performance. All workflows remain manual-only; no further dispatches are authorized by the completed smoke request.
