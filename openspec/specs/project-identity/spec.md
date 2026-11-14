# project-identity Specification

## Purpose

Keep project, distribution, and runtime naming consistent across all supported entrypoints and contributor tools.

## Requirements

### Requirement: Unified project identity

The project SHALL use setup-go-faster for repository, module, command, executable, native asset, runtime-directory, content-identity domain, and diagnostic names; display metadata SHALL use Setup Go Faster and project environment variables SHALL use SETUP_GO_FASTER. Packaging, verification, acquisition, setup, and post SHALL agree on these names.

#### Scenario: Native distribution execution
- **WHEN** a bundled or lightweight distribution runs setup and post
- **THEN** its launchers locate the setup-go-faster native executable and use matching project environment and private state names

#### Scenario: Contributor and consumer references
- **WHEN** documentation, workflow metadata, release tooling, and Go sources reference this project
- **THEN** they use the setup-go-faster repository and module identity consistently
