# Design

## Context

See proposal.md. Current source checks require HEAD to equal a remote branch tip, push that source ref, and use Promote for both latest and two aliases. Historical snapshots can lack the current publisher. The active regular-stable-releases change supplies the preceding stable-release implementation; this change supersedes its source/promotion restrictions.

## Goals / Non-Goals

**Goals:** One workflow from main with explicit source selection, frozen source SHA, safe backport defaults, and ordinary defaults requiring only a version and publish choice.

**Non-Goals:** Prerelease channels, automatic release dispatch, merging/publishing this PR, changing repository settings, or bypassing failing candidate checks.

## Decisions

- Add source_branch (main by default) and optional full source_commit. Resolve via approved origin, fetch the branch, reject detached/unreachable commits, and emit a fixed SHA. Reusable validation receives this SHA explicitly for every candidate checkout.
- Build the publisher from the workflow revision before candidate code executes. Read-only package jobs use candidate checks and packaging commands, while the trusted publisher operates on that candidate from a separate write-enabled job. Historical sources must satisfy current packaging/validation prerequisites; failing old tests are not bypassed.
- Require the selected HEAD to be an ancestor of the recorded remote source anchor. Source branches are never pushed, and later branch advances cannot retarget a saved plan. The recovery bundle retains the source anchor as well as the prepared version tag.
- Replace Promote with UpdateAliases and MakeLatest (auto/true/false). Resolve auto once against existing stable version tags. For each major/minor alias, promote only when no newer version in that series exists. A v1 patch after v2 can advance v1 without taking latest; an older minor only advances its own minor alias.
- Use the previous lower release within the same minor, falling back to the same major, as generated-notes baseline. Notes generation needs contents write, so it runs in the trusted resolver job. Validate, package, and attest use separate jobs; requested attestations gate publication.
- Pass controller and preparation artifacts between jobs using their exact immutable artifact IDs. Save selected policy, source, artifacts, trusted publisher, and Git objects before remote writes. Publication restores the exact prepared objects and paths, verifies again, and retains conflict-safe recovery.

## Risks / Trade-offs

- The controller SHA and released source SHA can differ → record both and explain the distinction for hosted attestation verification.
- An old source can contain incompatible packaging commands or failing tests → fail visibly and require a backport branch rather than weakening release gates.
- Remote writers can race output refs → retain exact leases, atomic output pushes, and alias preflight/resume checks.
- GitHub generated notes require a write-capable token → expose it only to the trusted workflow job, never candidate tests/builds.

## Migration Plan

Merge this PR before selecting source branches/commits in the UI. Replace --promote with --update-aliases and --make-latest; recreate only unpublished plans with the new schema. Never re-plan an already-pushed release; recover it using the original matching tool/plan. Review/archive the preceding regular-stable-releases change before this successor to preserve specification order.
