# Proposal

## Why

Align the project identity with its central aim of faster complete Go workflows.

## What Changes

- Use setup-go-faster consistently in source, modules, commands, native assets, environment variables, runtime paths, metadata, documentation, and specifications.
- **BREAKING**: use the unified executable, environment, state, and content-identity names without aliases.

## Capabilities

### New Capabilities

- `project-identity`: consistent project, distribution, and runtime naming.

### Modified Capabilities

- `setup-execution`: standalone fallback variables use the project identity.
- `cache-identity`: dependency discovery excludes the project runtime directory.

## Impact

Go and Node entrypoints, native packaging and verification, cache staging and content fingerprints, release tooling, workflow references, contributor tooling, and local checkout/project configuration. No dependency changes or performance claims.
