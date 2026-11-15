# Proposal

## Why

The current workflow can only release main and couples moving version aliases with GitHub latest. A maintainer needs ordinary main releases, backports from release branches, and historical commits through the same reviewed automation.

## What Changes

- Resolve a source branch and optional full commit SHA once; require the commit to belong to that repository branch, validate/build that exact snapshot, and never push source branches.
- Keep workflow/publisher automation on main, including when the selected source predates current release tooling.
- Replace promote with independent update-aliases and make-latest policies. Automatic latest follows version order; major/minor aliases advance independently without downgrades.
- Separate read-only candidate validation/builds from privileged notes generation and publication; require requested attestations before publication.
- Document normal, maintenance, and historical release recipes and migration of unpublished plans/flags. Runtime inputs and cache formats remain unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `distribution-publication`: immutable source selection, independent release policies, trusted workflow orchestration, and recoverable publication.

## Impact

Maintainer release CLI/tests, reusable validation and release workflows, workflow lockfile, release/development documentation. This supersedes the main-only source and coupled-promotion decisions in the active regular-stable-releases change. It prepares a PR only; hosted publication is separate.
