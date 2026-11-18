# Tasks

## 1. Baseline policy

- [x] 1.1 Implement independent save-policy and branch-writer inputs, validated restore outcomes and legacy handoff compatibility; cover cold/exact/fallback/partial/failed restores, branch eligibility and post rechecks with focused tests, update action metadata and owning usage/architecture/migration documentation, and pass input/document contracts.

## 2. Integration verification

- [x] 2.1 Extend native packaged fixtures to verify default-branch baseline seeding, warm skip, dependency fallback and branch restore-only behavior while retaining explicit always/all coverage; run canonical checks, Go 1.26 compatibility, applicable Windows packaged lifecycle cases and distribution verification, plus strict OpenSpec validation. Record unavailable hosted/native-platform/performance coverage accurately.

## Validation

- Canonical checks passed with Go 1.27.1 and Node 24, including all Go tests/vet, documentation contracts and 34 Node tests. The initial sandbox run failed in Git/MSYS release fixtures; the same checks passed outside the sandbox.
- Changed cache/setup/benchmark packages passed tests and vet with Go 1.26.8.
- A local Windows amd64 distribution built with Go 1.27.1 passed integrity/provenance verification with the dirty-source diagnostic allowance. Packaged lifecycle history passed for archive and chunked storage with gzip, zstd and none, covering the root action and Node alias.
- Strict OpenSpec validation and whitespace checks passed. Linux/macOS/arm64 native execution, hosted cache-service visibility, hosted workflows, publication and performance measurements were not run. No workflow or repository settings changed.

## Workflow follow-up

- Review the breaking defaults before selecting a release; publish a runnable distribution only on a separate explicit maintainer request.
- Measure paired source/dependency histories on hosted runners before claiming improved workflow performance.
