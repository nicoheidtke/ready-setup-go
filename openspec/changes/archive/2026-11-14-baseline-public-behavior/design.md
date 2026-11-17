# Design

## Context

See proposal.md for motivation. OpenSpec 2.0.0 resolves this repository as the authoritative root; the initial change and durable-spec inventories were empty. The inspected source revision is `81132f2ed52717fd865c679f195613dfeb49e8ce`. The handoff plan is pre-existing untracked user work and remains preserved. Runtime behavior predates this migration.

## Goals / Non-Goals

**Goals:** Establish durable capability ownership, observable compatibility requirements, and traceable evidence without transcribing implementation. Review the complete delta set before synchronization.

**Non-Goals:** Runtime fixes, new guarantees, schema/config changes, hosted execution, releases, performance claims, and archiving. Contributor tools and measurement utilities remain procedural unless they expose a relevant maintainer distribution contract.

## Decisions

### Preserve the agreed eleven capability boundaries

| Capability | Normative owner | Cross-boundary rule |
| --- | --- | --- |
| go-version-selection | Selectors, precedence, directives, patch choice, selection failures | Installation inspects the chosen completed installation. |
| go-toolchain-installation | Supported Go installs, discovery, acquisition, verification, safe publication | Action acquisition obtains the action executable. |
| setup-execution | Root/alias/composite/standalone entrypoints, runner environment, failure/cancellation | Exposes outputs whose cache meanings diagnostics defines. |
| cache-policy | Modes, job eligibility, runner restrictions, strictness, overlap, handoff | Snapshots decides whether each eligible snapshot needs saving. |
| cache-identity | Dependency fingerprints, namespaces, matching/fallback, fresh identity | Storage controls representation; labels are not authorization. |
| cache-snapshots | Selected Go cache roots, grouping, module change detection, build saves/age | Policy determines job eligibility; storage owns encoding. |
| cache-storage | Representations/codecs, transfers, resource controls, encoded integrity, remote finalization | Restoration owns acceptance and local publication; owns cache-max-bytes input. |
| cache-restoration | Complete validation, unsafe-entry rejection, local-file preservation, bounded publication | References storage limits; does not promise transactional merge or hostile-writer defense. |
| diagnostics | Output meanings, result labels, metrics, reports, summaries, progress, privacy | Refers to domain outcomes without duplicating operational rules. |
| action-acquisition | Executable selection, bundled/lightweight acquisition, pinned verification | Installation obtains Go; publication produces distributions. |
| distribution-publication | Package/verify/plan/prepare/publish integrity, reproducibility, conflicts/recovery | Procedures and compiler/release pins remain in owning documentation. |

A single security capability was rejected because it would duplicate the protected operations. Batch names are coordination units, not capability IDs. Implementation-specific staging, serialization, hash algorithms, retries, buffers, and schedules remain outside normative contracts unless they define an exposed compatibility promise.

### Ground every contract in source and existing tests

The supporting coverage.md maps interfaces/behavior families to exactly one owning requirement, documentation, implementation, and tests. Source inspection and existing fixture coverage are separate from checks executed in this effort. Native/hosted/performance evidence retains its existing limits; no fresh such evidence is implied. Material source/document conflicts remain explicit decisions rather than silently adopted behavior or runtime edits.

New deltas include a substantive Purpose and ADDED Requirements. Each requirement has a short normative statement and representative WHEN/THEN scenarios. The representative style is explicit-version precedence: supplying an explicit selector bypasses reading even a nonexistent version-file path, grounded in ResolveVersionInput. The filename examples do not establish a filename restriction.

### Use bounded fresh drafting and independent review agents

Each drafting agent writes only its assigned capability delta files, reads relevant implementation/tests/docs, and returns compact evidence rows and questions. The coordinator owns proposal, design, tasks, and coverage. Fresh contract, boundary, and evidence reviewers inspect the completed baseline; the coordinator resolves their findings. A validator pass is structural evidence, not semantic review.

### Clarify age-filter documentation from observed behavior

The maintainer authorized reconciling source and documentation when they conflict. Save applies the build-age cutoff during packing before either storage representation is encoded; Config.Validate has no archive-storage restriction. Retain this sensible snapshot-only behavior for both archive and chunked storage, and clarify root/alias metadata, CLI help, and usage wording. This is a presentation/documentation correction with no cache-policy, format, or runtime algorithm change.

### Respect the proposal workflow checkpoint

The selected openspec-propose skill required a separate user request before apply. The complete delta set and ledger were reviewed at that checkpoint; the maintainer then explicitly requested application and synchronization of all eleven specs, leaving the change unarchived. Durable synchronization preserves the reviewed Purpose, requirements, and scenarios; contributor guidance and the superseded handoff identify the durable specs as the behavioral authority.

## Risks / Trade-offs

- [Describing accidental behavior as a promise] → Compare implementation, tests, and documented intent; record material discrepancies for a maintainer decision.
- [Overstated safety] → Preserve inspected-local-toolchain trust, partial merge I/O failure, trusted-filesystem assumptions, best-effort abrupt cancellation cleanup, and unredacted machine-data limits.
- [Drift or duplicated ownership] → Independent cross-review and an input/output/entrypoint ledger precede sync.
- [High-level wording misses exposed controls] → Inventory every root and alias metadata input and output, standalone subset, and maintainer command family.
- [Compatibility partitions overfreeze implementation] → Specify isolation and reuse behavior rather than literal keys, state formats, or algorithm constants.

## Migration Plan

After the reviewed planning checkpoint, apply the documentation-only change. Read the sync skill and current CLI context/status/specs instructions; use only returned delta paths. Populate eleven durable specs with Purpose and Requirements, preserving the reviewed ADDED content without delta headers. Reconcile CONTRIBUTING adoption text and mark the handoff plan superseded only when durable coverage exists. Strictly validate the change and durable inventories, run canonical local checks, and leave the change active. Rollback consists of reverting this documentation/specification change; there is no data or runtime migration.
