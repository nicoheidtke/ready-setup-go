# Tasks

## 1. Windows archive materialization

- [x] 1.1 Reduce redundant Windows filesystem work while retaining archive validation, metadata and safe publication; verify focused native archive regression tests and retain a local diagnostic comparison.
- [x] 1.2 Update architecture and measurement guidance for the implemented materialization behavior; verify documentation consistency without claiming hosted performance.

## 2. Split-snapshot pipeline

- [x] 2.1 Implement ordered per-snapshot transfer readiness and restore consumption after successful toolchain discovery; verify overlap, partial failures, close/cancellation joining, cleanup and metrics with focused cache tests.
- [x] 2.2 Document pipeline ordering, worker/disk boundaries and elapsed metric semantics; verify documentation checks and preserve the sequential opt-out.

## 3. Combined validation and benchmark candidate

- [x] 3.1 Review the combined implementation and run canonical checks, supported compiler checks, concurrency race checks where supported, and native packaged lifecycle checks across affected codecs/storage; record actual coverage and any unavailable platform checks.
- [x] 3.2 Freeze reviewed source, build reproducible bundled distributions and verify assets/provenance; provide the concrete candidate and the remaining publication or benchmark handoff step.

## Workflow follow-up

- Publish the prepared candidate when explicitly authorized by the maintainer, then benchmark that immutable distribution with native restore phase diagnostics enabled.
- Review results against the previous batch; local diagnostics alone do not establish a hosted workflow speedup.
