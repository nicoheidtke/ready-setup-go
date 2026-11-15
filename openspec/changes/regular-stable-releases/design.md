# Design

## Context

See proposal.md. The current publisher already validates two builds, exact Git bytes/modes, leases, and nine remote asset digests. The repository has no public releases, so private preview plans and flags need no compatibility workaround.

## Goals / Non-Goals

**Goals:** One main-only source process, one manual release workflow, and usable action tags with fixed full-version content.

**Non-Goals:** Automatic PR execution, source release branches, merging binaries back into main, prerelease automation, repository-setting changes, or unsupported performance/service claims.

## Decisions

- Accept stable vMAJOR.MINOR.PATCH only, without leading zeroes. Replace preview names/flags with distribution names and default `refs/heads/dist`. An absent branch has an empty expected ref. Distribution commits retain the prior distribution when present and the exact source as parents. This links provenance and gives generated PR notes a source merge base without extra source tags.
- Capture major/minor alias refs in the plan. After verifying the published release, atomically move both aliases using exact leases, then verify them. Resume accepts exact completed targets; conflicting writers fail. Normal promotion rejects versions older than an existing version. `--promote=false` permits maintenance publication while leaving aliases/latest unchanged.
- Reuse the existing validation workflow through workflow_call, keeping its manual entrypoint. A manual release dispatch on main selects the candidate source SHA and gates packaging/publication on all six native targets. Serialize releases without cancellation. Keep credentials out of checkout and ordinary checks.
- Generate notes from merged PRs when no override is supplied. Include source/distribution provenance and the workflow link with each release. Persist plan, assets, distribution, and a Git bundle before remote writes for recovery.
- Keep examples on the usage guide's recommended major alias. Permit full-version/SHA examples and reject source refs. Source docs are updated in normal PRs before release; provenance is release-owned, removing circular self-SHA updates and post-release README PRs.

## Risks / Trade-offs

- API/Git writes cannot be one transaction → preserve exact leases, verified resumability, and recovery artifacts before publication.
- Aliases are mutable → publish fixed full versions and record full distribution SHAs for consumers needing fixed bytes.
- Six-platform validation costs runner minutes → run it once automatically within the release workflow; preserve manual control.
- Hosted native fixtures do not establish every cache-service policy or speedup → retain explicit support limits and roadmap investigations.

## Hosted fixture corrections

The first hosted validation exposed pre-existing fixture assumptions: GitHub ignores workflow overrides of reserved RUNNER_* variables, and a read-only archive test prevented Unix temporary-directory cleanup. Provision fixture toolchains in explicit child processes with runner-output channels disabled so the packaging compiler stays active. Native fixture paths use the actual runner directory; the Linux release fixture additionally exercises a path containing spaces. Restore test-only read-only directory permissions during cleanup after all metadata assertions. Use the existing canonical temporary-directory helper in archive identity/policy tests so macOS /var symlinks are resolved before exercising symlink-rejecting archive APIs. Runtime behavior remains unchanged.

## Migration Plan

Merge the readiness PR into main, run Release action for v1.0.0, and inspect the release summary. Thereafter merge ordinary source/documentation PRs and dispatch the next version. Never merge dist back into main or rebuild an existing published version to retry.
