# Design

## Context

See proposal.md for motivation. Existing snapshots have fresh immutable keys with a dependency prefix and a broader compatible prefix. Restore already distinguishes matching and compatible candidates, but only successful publication can establish a usable baseline. Main/post exchange JSON configuration. Historical handoffs omit the new fields.

## Goals / Non-Goals

Goals: cheap baseline decisions, conservative writer eligibility, independent customization, and compatible archive reuse. Keep caching opt-in and preserve job/workflow isolation and the meaning of cache-hit (all requested snapshots successfully restored, including fallback).

Non-goals: automatic elapsed-time refresh, content-change detection for build caches, codec changes, cross-job build sharing, hosted measurements, or publication.

## Decisions

- Add string inputs `cache-save-policy` (`on-miss`, `always`) and `cache-save-branches` (`default`, `all`). DefaultConfig and action metadata set explicit new defaults. Empty serialized fields retain legacy always/all behavior; validation accepts empty only for this compatibility path. The input parser rejects explicitly empty/invalid command-line choices through resolved validation where applicable.
- Keep fresh immutable keys and existing prefixes/formats. A successful restored dependency prefix is an exact dependency baseline even when the stored entry has a run-specific suffix. Persist that outcome separately from lookup success. This reuses historical snapshots and avoids introducing another key scheme or changing cache-hit.
- On-miss skips before root access and bypasses restored module fingerprint generation/checks. A fallback must seed a new dependency baseline, even if the old module contents equal the current tree. Always retains the existing unchanged-module check and fresh build saves.
- Restrict writes in main and recheck in post. Default requires a heads ref, matching `GITHUB_REPOSITORY`, and `repository.default_branch` from a valid event object limited to 1 MiB. Unknown context skips intentionally, including strict mode. All bypasses this event requirement while retaining runner write permissions and successful-job defaults. Branch names do not enter keys; GitHub controls ref visibility.
- Avoid backend construction in post when every snapshot is already skipped. Reports use a fixed save reason for dependency matches; policy strings are safe configuration diagnostics. Do not add event payloads, branch names, credentials or URLs to reports.
- Keep cache-build-save as an additional opt-out, including on cold misses. Always/all is the explicit migration recipe for previous behavior. Concurrent initial misses can still publish separate immutable baselines; this retains the existing namespace and race behavior rather than introducing a reservation protocol.

## Risks / Trade-offs

- Source-only changes never refresh the baseline under on-miss. Document repeated application compilation/test work and show always refresh with workflow expressions for selective refresh.
- An incomplete first baseline can remain incomplete. Retain workload isolation and document deliberate seeding and manifest selection for shared modules.
- Missing or oversized runner metadata prevents default saves. Restore still works and logs explain the skip; standalone callers can opt into all.
- New defaults change enabled-cache behavior. Mark the change breaking and require an appropriate release/migration decision; source edits alone do not publish a runnable action.
- Skipping uploads does not establish a workflow speedup. Test policy correctness locally and leave hosted performance claims to paired source/dependency-history measurements.

## Migration Plan

Existing archives remain readable and existing compatible main/post state keeps legacy behavior. New invocations use baseline policies. Users needing prior behavior set always/all; long-lived branches can enable all independently. Restore-only and cache-off remain unchanged. Rollback uses the previous distribution or the explicit legacy recipe; no cache deletion is required.
