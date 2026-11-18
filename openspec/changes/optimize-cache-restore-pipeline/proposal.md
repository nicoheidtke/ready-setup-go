# Proposal

## Why

The Exasol compile-mode benchmark repeatedly spends roughly 45-52 seconds completing Windows restore after the final cache download. Split-snapshot prefetch also waits for every download before restoring any snapshot, leaving transfer and filesystem work unnecessarily serialized.

## What Changes

- Reduce Windows archive materialization filesystem work while preserving complete private validation, supported metadata, existing-file priority and bounded cancellation-safe cleanup.
- Let the ordered restore consumer process a completed split snapshot while the downloader fetches the next snapshot, after successful toolchain setup establishes destination roots.
- Retain inputs, defaults, keys, archive representations, main/post state and existing failure policy. No migration or new runtime dependency is required.
- Prepare a locally verified bundled candidate for the next hosted benchmark; do not infer hosted improvement from local diagnostics.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This is an implementation refactor under the existing cache-restoration, cache-storage, setup-execution and diagnostics contracts; `skip_specs` explicitly retains those requirements unchanged.

## Impact

Runtime changes are confined to `internal/cachearchive` and `internal/cache`, with regression coverage, architecture/measurement documentation and packaged lifecycle validation. Two implementation agents own the archive and prefetch areas separately. Publishing or dispatching hosted workflows remains a separate maintainer operation.
