# Spec Delta

## Purpose

Keep project, distribution, and runtime naming consistent across all supported entrypoints and contributor tools.

## MODIFIED Requirements

### Requirement: Unified project identity

The project SHALL use ready-setup-go for repository, module, command, executable, native asset, runtime-directory, content-identity domain, and diagnostic names; display metadata SHALL use Ready Setup Go and project environment variables SHALL use READY_SETUP_GO. Project format identifiers SHALL use the rsg prefix. Packaging, verification, acquisition, setup, and post SHALL agree on these names.

#### Scenario: Native distribution execution
- **WHEN** a bundled or lightweight distribution runs setup and post
- **THEN** its launchers locate the ready-setup-go native executable and use matching project environment and private state names

#### Scenario: Contributor and consumer references
- **WHEN** documentation, workflow metadata, release tooling, and Go sources reference this project
- **THEN** they use the ready-setup-go repository and module identity consistently
