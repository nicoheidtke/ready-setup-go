# Design

## Context

See proposal.md for benchmark motivation. Existing restoration validates a complete tar/codec stream privately, preflights destinations, and publishes without replacing local regular files. Windows can move missing trees, but materialization and metadata work still involve per-file filesystem calls. Prefetch currently downloads every snapshot before a separate ordered merge pass.

## Goals / Non-Goals

Goals: reduce unnecessary Windows filesystem work and overlap transfer with restoration without multiplying archive-restoration workers or weakening validation. Preserve current cache representations, controls, identities, state and error policy.

Non-goals: changing worker defaults, parallel restoration of multiple archives, changing save policies, adding dependencies, relaxing metadata preservation or claiming a hosted speedup before another benchmark round.

## Decisions

- Finalize Windows regular-file timestamps and readonly attributes with one `SetFileInformationByHandle` operation on the already-open staging handle. Retain a private per-entry metadata-ready flag so whole-tree publication omits repeated per-file path-based metadata calls. Directories still finalize after children; refused moves reset staging writable and the existing collision-safe merge reapplies metadata. This reduces filesystem calls without changing worker defaults or replacing no-replace moves with ordinary rename.
- Give prefetch separate preparation and per-snapshot readiness signals. Publish immutable transfer results only after the download file closes successfully. One downloader remains ordered; one consumer restores each ready snapshot after toolchain setup and destination discovery. This overlaps download N+1 with validation/publication N without a new archive API or multiple simultaneous archive restorations.
- Copy prepared state into consumer-owned state. Transfer metrics/files/errors remain producer-owned until their readiness signal; restore metrics and main/post outcomes belong to the consumer. Cancellation joins the producer before private-file cleanup.
- Preserve cache-overlap=false's sequential one-archive behavior and combined-layout behavior. Corrupt or missing snapshots retain existing strict/non-strict and per-snapshot acceptance semantics; an earlier validated snapshot need not roll back if a later one fails.
- Report restore total as elapsed wall time across the pipeline, excluding preparation, rather than adding overlapping transfer and archive durations. Keep detailed per-snapshot phase metrics and measure actual join waits.

## Risks / Trade-offs

- Filesystem and network work can compete for resources. Keep one restore consumer, existing worker bounds and bounded streaming; use complete workflow measurements to determine benefit.
- Split downloads and one staged tree can coexist on disk, as they can today. Per-snapshot byte limits remain distinct from peak disk/RSS; document the overlap rather than imply an aggregate quota.
- Windows read-only attributes can interfere with error cleanup. Cover corrupt archives, metadata failures, refused moves, existing-file collisions and cancellation with native regression tests.
- Readiness mistakes could expose partial downloads or race with cleanup. Use explicit producer/consumer ownership, synchronization tests and race-detector checks where available.

## Migration Plan

No input, state or archive migration. Validate canonical checks, supported compiler lines and packaged native lifecycle locally; package twice from frozen source and verify all platform assets. Prepare an immutable benchmark candidate. Remote publication requires the maintainer operation described by AGENTS.md; hosted benchmark runs remain a separate step.
