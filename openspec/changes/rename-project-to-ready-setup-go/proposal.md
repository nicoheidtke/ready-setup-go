# Proposal

## Why

Establish the distinct ready-setup-go identity before adoption, with no compatibility aliases or retained source history.

## What Changes

- Use ready-setup-go consistently in source, modules, commands, native assets, environment variables, runtime paths, metadata, documentation, and specifications.
- **BREAKING**: use the unified executable, environment, state, and content-identity names without aliases.

## Capabilities

### Modified Capabilities

- `project-identity`: consistent ready-setup-go project, distribution, and runtime naming.
- `setup-execution`: standalone fallback variables use the project identity.
- `cache-identity`: dependency discovery excludes the project runtime directory.

## Impact

Go and Node entrypoints, native packaging and verification, cache staging and content fingerprints, release tooling, workflow references, contributor tooling, and a single-commit local main history. The maintainer handles hosted repository rename, pushing, release deletion, and obsolete hosted refs. No dependency changes or performance claims.
