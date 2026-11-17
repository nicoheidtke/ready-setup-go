# go-version-selection Specification

## Purpose

Select a supported exact Go release from explicit selectors or version-file directives while preserving the caller's resolution policy.

## Requirements

### Requirement: Supported release selectors

Selection SHALL accept Go 1.x release selectors starting at 1.26 as exact patches (`1.26.0`) or minors (`1.26` and `1.26.x`). It SHALL reject unsupported releases, prereleases, aliases, custom toolchain names, and arbitrary ranges.

#### Scenario: Exact patch remains exact
- **WHEN** `1.26.0` is requested
- **THEN** selection requests that patch rather than promoting it to another patch

#### Scenario: Unsupported syntax fails
- **WHEN** the selector is `stable`, `1.26rc1`, `^1.26.0`, or `1.25.9`
- **THEN** selection fails

### Requirement: Explicit version input takes precedence

The setup operation SHALL use a nonempty trimmed `go-version` without reading `go-version-file`. If neither input is supplied, it SHALL fail rather than discover a version file implicitly.

#### Scenario: An unused version file does not exist
- **WHEN** a supported explicit Go version and a nonexistent version-file path are supplied
- **THEN** selection uses the explicit version without failing because that file is absent

#### Scenario: No version input
- **WHEN** both version inputs are empty
- **THEN** selection fails even if the current directory contains `go.mod`

### Requirement: Version-file paths and read failures

Selection SHALL support Go module/workspace directive files supplied by relative or absolute path. Relative action paths SHALL resolve from `GITHUB_WORKSPACE`; standalone paths SHALL resolve from the current directory. Missing, unreadable, or oversized files SHALL fail selection.

#### Scenario: Nested workspace file
- **WHEN** only a readable relative version-file path containing spaces is supplied in an action
- **THEN** selection reads that path relative to the workspace

#### Scenario: Required file is absent
- **WHEN** no explicit version is supplied and the requested file does not exist
- **THEN** selection fails before toolchain installation

### Requirement: Version directives select the compiler

Selection SHALL require a valid top-level `go` release directive. A concrete supported `toolchain goVERSION` SHALL select that exact compiler if it is not older than the `go` minimum; absent or `default` toolchain directives SHALL use the `go` selector. Duplicate, malformed, or unsupported relevant directives SHALL fail.

#### Scenario: Preferred compiler with older language minimum
- **WHEN** a version file contains `go 1.25.0` and `toolchain go1.26.4`
- **THEN** selection requests exactly `1.26.4`

#### Scenario: Default retains patch specificity
- **WHEN** a version file contains `go 1.26.0` and `toolchain default`
- **THEN** selection requests exactly `1.26.0`

#### Scenario: Preferred compiler is below minimum
- **WHEN** a version file contains `go 1.26.4` and `toolchain go1.26.3`
- **THEN** selection fails

#### Scenario: Comments and unrelated directives
- **WHEN** comments or unrelated directive blocks contain version-like text
- **THEN** selection uses the actual top-level version directives

### Requirement: Installed patch preference and latest resolution

With `check-latest` false by default, a minor selector SHALL prefer the highest usable installed matching patch. With `check-latest` true, or without an installed match, it SHALL resolve a matching stable official release for the platform. Exact installed matches SHALL remain reusable without metadata requests even with `check-latest` true.

#### Scenario: Highest installed patch wins
- **WHEN** a minor selector matches several usable installed patches and latest checking is disabled
- **THEN** selection chooses the highest matching installed patch without a network request

#### Scenario: Latest minor patch is requested
- **WHEN** a minor selector has latest checking enabled
- **THEN** selection resolves official metadata and chooses its highest matching platform archive, reusing that exact installation if available

#### Scenario: Official resolution is unavailable
- **WHEN** metadata retrieval fails or no matching stable platform archive exists
- **THEN** selection fails without substituting an unrelated release
