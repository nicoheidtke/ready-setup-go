# Proposal

## Why

Enabled caching currently refreshes every eligible build snapshot, even for repeated unchanged workloads. Simple workflows need an economical dependency baseline and explicit controls for workloads that benefit from fresher results or branch-local writes.

## What Changes

- **BREAKING**: enabled caching defaults to `cache-save-policy: on-miss` and `cache-save-branches: default`. Caching itself remains opt-in.
- Restore dependency-matching snapshots before compatible fallback on every branch. Only successfully validated dependency matches suppress saves; compatible hits still seed a new baseline on eligible writers.
- Skip all snapshot scanning, fingerprinting, packing and uploading after a successful dependency match under `on-miss`.
- Permit `always` refresh and `all` branch writers independently. Preserve the existing build-save opt-out, unchanged-module optimization in always mode, job isolation, archive identities and cache-hit output meaning.
- Establish the default branch from bounded runner event metadata and the actual branch ref; missing, invalid or mismatched context disables default-policy saves. Recheck write eligibility in post.
- Preserve legacy serialized main/post configurations without the new fields as always/all; document migration and source-only release limits.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cache-policy`: default-branch writers, independent policy inputs and backward-compatible main/post state.
- `cache-snapshots`: dependency-baseline saves and an explicit refresh policy.

## Impact

Go cache configuration, input parsing, restore state and post-save decisions; root and Node-alias action metadata; lifecycle and input contract fixtures; README, usage, architecture, comparison and release migration guidance. No dependency, codec, archive format, namespace, hosted workflow or repository setting changes. Local checks and packaged native fixtures validate behavior; hosted performance and branch-service enforcement remain separate evidence.
