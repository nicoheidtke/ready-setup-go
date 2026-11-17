# cache-identity Specification

## Purpose

Define which Go dependency inputs and compatibility namespaces identify reusable cache snapshots while preserving isolation and explicit sharing choices.

## Requirements

### Requirement: Dependency selection discovers workspace manifests

An empty `cache-dependency-path` SHALL recursively discover `go.mod`, `go.sum`, `go.work`, and `go.work.sum` beneath the workspace, excluding nested `.git`, `.cache`, `vendor`, `node_modules`, `dist`, `bin`, and `.ready-setup-go` directories. Explicit selection SHALL accept newline-separated workspace-relative files of any name or directories using recursive manifest discovery; it SHALL NOT interpret paths as globs.

#### Scenario: Multi-module workspace

- **WHEN** automatic discovery encounters manifests in several workspace modules
- **THEN** all discovered manifest names and contents contribute to the dependency fingerprint

#### Scenario: Output or vendor manifests change

- **WHEN** only manifests beneath excluded nested directories change during automatic discovery
- **THEN** those changes do not change the dependency fingerprint

#### Scenario: Explicit file has another name

- **WHEN** cache-dependency-path selects an existing regular workspace file with a nonstandard name
- **THEN** its relative name and contents contribute without requiring a Go manifest basename

#### Scenario: Explicit directory limits discovery

- **WHEN** cache-dependency-path selects a workspace subdirectory
- **THEN** recursive manifest discovery is limited to the selected directory rather than unselected workspace directories

### Requirement: Dependency selection rejects unsafe or unavailable inputs

Dependency preparation SHALL reject absolute selections, workspace escapes, missing selected paths, selected symlinks, and non-file/non-directory selections. Discovered manifest entries SHALL be regular files. Selection and hashing SHALL observe cancellation and enforce bounded selection, file-count, path-metadata, and content budgets. Preparation failures SHALL follow cache-policy operational error handling.

#### Scenario: Explicit selection escapes the workspace

- **WHEN** a dependency selection is absolute or resolves outside the workspace
- **THEN** cache preparation rejects it rather than hashing outside the workspace

#### Scenario: Selected file does not exist

- **WHEN** an explicitly selected dependency path is absent
- **THEN** cache preparation fails under the configured operational error policy

#### Scenario: Selection exceeds a resource budget

- **WHEN** selected dependency metadata or contents exceed an enforced logical budget
- **THEN** preparation rejects the selection without creating an identity from a truncated subset

#### Scenario: Cancellation during hashing

- **WHEN** cancellation is observed while hashing selected files
- **THEN** dependency preparation stops without returning a completed fingerprint

### Requirement: Fingerprints reflect selected relative names and contents

The dependency fingerprint SHALL be stable for the same selected workspace-relative names and file contents regardless of selection ordering or repeated overlapping selections. Changes to selected names or contents SHALL affect dependency identity. Resource bounds SHALL preserve fingerprints for accepted selections; timestamps alone SHALL NOT define the fingerprint.

#### Scenario: Overlapping selections

- **WHEN** the same files are selected directly and through an overlapping directory
- **THEN** they contribute once and produce the same fingerprint as the equivalent unique selection

#### Scenario: Same manifest content under a different relative name

- **WHEN** a selected manifest is moved within the workspace without changing its contents
- **THEN** its changed relative name changes dependency identity

#### Scenario: Content changes without dependency path changes

- **WHEN** a selected manifest's contents change
- **THEN** the dependency fingerprint changes

### Requirement: Default namespaces retain compatibility partitions

Default snapshot reuse SHALL remain partitioned by repository, workflow, job, optional `cache-scope`, operating system, architecture, exact selected Go version, layout, storage representation, codec, and snapshot kind. Cache-scope SHALL default to empty and add a workload namespace when nonempty. Dependency identity SHALL distinguish preferred matches within each compatible namespace; scope labels SHALL NOT grant access or authenticate producers.

#### Scenario: Matrix variants use different scopes

- **WHEN** otherwise identical jobs set different cache-scope values
- **THEN** their default snapshots use separate compatible namespaces

#### Scenario: Exact Go patch changes

- **WHEN** the exact selected Go version changes without opting modules into a shared module scope
- **THEN** default snapshot reuse is separated by the changed Go version

#### Scenario: Storage or codec changes

- **WHEN** archive storage changes to chunked or gzip changes to zstd or none
- **THEN** the incompatible representation or codec uses a separate snapshot namespace

### Requirement: Explicit module scope shares only split module snapshots

`cache-module-scope` SHALL default to empty. A nonempty value SHALL require split layout and select a separate repository-local module namespace shared across workflows, jobs, cache-scope values, and selected Go versions. Module sharing SHALL retain OS, architecture, storage, codec, and dependency matching partitions. Build snapshots SHALL retain default isolation. Sharing labels SHALL NOT authenticate cached build inputs.

#### Scenario: Trusted jobs share a module group

- **WHEN** jobs in one repository use the same nonempty module scope and compatible platform, storage, and codec
- **THEN** their split module snapshots can match across workflow, job, workload scope, and selected Go version differences while build snapshots remain isolated

#### Scenario: Module scope returns to empty

- **WHEN** a job clears a previously configured module scope
- **THEN** module snapshots return to the original default namespace

#### Scenario: Combined layout requests module sharing

- **WHEN** a nonempty cache-module-scope is supplied with combined layout
- **THEN** input validation rejects the configuration

#### Scenario: Same label in another repository

- **WHEN** different repositories choose the same module scope label
- **THEN** the label does not place their module snapshots in the same action namespace

### Requirement: Restore prefers dependency matches before compatible fallback

Restore SHALL request dependency-matching snapshots before compatible snapshots in the same namespace. Dependency changes SHALL permit fallback reuse without crossing compatibility partitions. When no matching snapshot is available, restore SHALL produce a miss rather than a selection failure. The service SHALL remain authoritative for branch/token visibility; the action's namespaces SHALL NOT widen that visibility.

#### Scenario: Dependencies match an available snapshot

- **WHEN** the service offers a snapshot under the current dependency prefix
- **THEN** lookup prefers that prefix over the broader compatible prefix

#### Scenario: Dependencies changed

- **WHEN** no dependency-matching snapshot is available but an older compatible snapshot is visible
- **THEN** restore can reuse it within the unchanged namespace

#### Scenario: No visible match

- **WHEN** the service returns no entry for either lookup scope
- **THEN** restore reports a miss without treating absence as an operational error

### Requirement: Save identities remain fresh and immutable

Each invocation's prepared snapshot keys SHALL incorporate run/attempt identity and an invocation-specific random suffix so an eligible save can publish fresh immutable snapshots even after a restore hit. Existing immutable entries SHALL NOT be overwritten. Compatible lookup prefixes SHALL remain reusable across fresh save keys; changing tuning controls alone SHALL NOT create unrelated compatibility partitions.

#### Scenario: Repeated invocation in one run

- **WHEN** two independent invocations use the same dependencies and namespace in one run attempt
- **THEN** their prepared save keys differ while their compatible lookup scopes remain the same

#### Scenario: Eligible build save follows a hit

- **WHEN** an eligible invocation restored a build snapshot and later saves nonempty build contents
- **THEN** the save uses its fresh invocation key rather than replacing the matched entry

#### Scenario: Worker tuning changes

- **WHEN** only worker concurrency or gzip level changes for an otherwise compatible snapshot
- **THEN** its compatibility namespace remains unchanged
