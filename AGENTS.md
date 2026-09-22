# Project handoff

Read [ROADMAP.md](ROADMAP.md), [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md), and [docs/BENCHMARKS.md](docs/BENCHMARKS.md) before changing the implementation. The README is consumer documentation, not a development-status log.

## Goal and priorities

Build `setup-go-better`, a fast Go setup action for GitHub Actions, combining efficient toolchain installation with, eventually, fresh and efficient build/module caching.

Priorities, in order:

1. Runtime performance: minimize total workflow cost, including action acquisition, setup, and eventual cache restore/save.
2. Resource efficiency: transferred bytes, storage, CPU, and disk work.
3. Maintainability.

Ease of implementation must not override runtime performance. Prefer measured results to assumptions about languages or backends. Do not claim speedups without evidence.

## Accepted decisions

- Start with installation and benchmarks; caching is a later milestone.
- Target GitHub-hosted Linux, macOS, and Windows, each on amd64 and arm64. All three operating systems are in initial scope.
- Support Go 1.x starting at 1.26, with exact versions and minor selectors only. Prefer installed matching patches; `check-latest` opts into a freshness lookup. Version files are deferred.
- Keep implementation in Go with minimal dependencies. The user dislikes substantial Bash/Perl and JS/TypeScript logic. Minimal launcher glue is acceptable as an experiment, with runtime costs measured.
- Compare a composite/Bash launcher and a dependency-free Node launcher using the same native executable. Neither launcher has won on performance yet.
- Bundle executables initially, while measuring bundled acquisition against downloading only the matching binary. Do not compile the installer on consumer runners.
- Treat launcher and storage choices independently. External storage remains a future performance candidate, not an initial requirement. GitHub cache is not a final backend decision.
- Cover small projects, parallel build/test/lint jobs, and monorepos in eventual cache benchmarks; do not arbitrarily narrow the audience to one workload size.
- Use `exasol/exasol-personal` as the primary real-world acceptance workload: this action is being developed for that project. Start with a small smoke test using pinned well-known packages, then validate its build/test/lint workloads at a recorded commit.

## Current state and execution constraint

Milestone 1 sources, tests, launchers, packaging code, and workflow definitions have been written. Local validation started on 2026-09-22; see docs/DEVELOPMENT.md for verified results and remaining checks. No comparative performance results exist.

On 2026-09-22 the user authorized proceeding with validation, identifying `exasol/exasol-personal` as the ultimate test after small package smoke tests. This supersedes the earlier implementation-only restriction. The user subsequently authorized the initial commit and push to `nicoheidtke/setup-go-better` and wants to consume the action in a workflow. Publish a bundled preview revision for that validation; a stable release still requires the roadmap's measurements. Changes to the upstream Exasol repository remain outside this task.

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
