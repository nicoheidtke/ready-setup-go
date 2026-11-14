# Proposal

## Why

The runtime predates OpenSpec, leaving future changes without durable behavioral requirements against which to assess compatibility. Establish the agreed high-level baseline from current implementation, tests, and owning documentation.

## What Changes

- Add eleven capability delta specs describing existing public behavior, with representative success and boundary scenarios.
- Record ownership, evidence, review findings, and the documentation-only integration checklist.
- Clarify age-filter descriptions/help to match snapshot packing for both storage modes, under the maintainer's follow-up authorization; preserve the implemented behavior.
- Populate durable specs and reconcile contributor adoption guidance. Preserve all runtime behavior, defaults, cache identities/formats, and main/post compatibility.

## Capabilities

### New Capabilities

- `go-version-selection`: Select supported Go releases from explicit or file inputs.
- `go-toolchain-installation`: Discover, verify, and safely publish usable Go installations.
- `setup-execution`: Entrypoints, runner environment integration, failure, and cancellation.
- `cache-policy`: Cache modes, eligibility, strictness, and compatible main/post handoff.
- `cache-identity`: Dependency fingerprints, namespaces, sharing, and immutable snapshot identity.
- `cache-snapshots`: Cache roots, grouping, selective saves, and build age filtering.
- `cache-storage`: Representations, codecs, bounded transfers, and encoded integrity.
- `cache-restoration`: Validate complete snapshots and preserve local files during publication.
- `diagnostics`: Output meanings, summaries, progress, reports, and presentation privacy limits.
- `action-acquisition`: Select and verify bundled or downloaded action executables.
- `distribution-publication`: Maintainer packaging, verification, preparation, and publication contracts.

### Modified Capabilities

None; the durable inventory was empty when this baseline change was created.

## Impact

Specification and documentation artifacts only. No runtime algorithm, dependency, workflow trigger, configuration, public-interface semantics, or cache migration changes. Local validation is separate from six-platform native execution, hosted cache behavior, and performance measurement. Hosted operations and publication are excluded; the change remains unarchived.
