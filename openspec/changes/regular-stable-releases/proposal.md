# Proposal

## Why

The private-era dated preview publisher cannot publish a first public stable release. Maintaining a public action needs one main-only source workflow with ordinary versioned releases and no per-patch documentation chore.

## What Changes

- Replace dated preview publication with exact stable semantic versions and a neutral generated `dist` branch; bootstrap its first release atomically.
- Publish fixed version tags, mark normal releases latest, and promote guarded major/minor aliases only after remote asset verification; support maintenance releases without promotion.
- Add a single manual release workflow that runs reusable six-platform validation, reproducible packaging, optional generated notes, explicit publication, and retained recovery inputs.
- Merge code and documentation into main before release. Use major-version consumer examples and record exact provenance in each release, avoiding a follow-up README commit.
- Preserve runtime inputs, cache compatibility, nine release assets, executable modes, explicit writes, and conflict-safe recovery.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `distribution-publication`: public stable releases, aliases, first publication, hosted release gating, and main-only documentation ownership.

## Impact

Maintainer CLI/tests, reusable validation and release workflows/lockfile, documentation consistency checks, release/development/consumer guides. No runtime behavior changes. Unpublished private-era plans and preview command flags are replaced deliberately; no public release exists to migrate.
