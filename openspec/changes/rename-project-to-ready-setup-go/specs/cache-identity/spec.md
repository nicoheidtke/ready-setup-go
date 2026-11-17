# Spec Delta

## MODIFIED Requirements

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
