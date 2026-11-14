# go-toolchain-installation Specification

## Purpose

Obtain usable Go installations for supported runner platforms while validating fresh downloads and preserving existing local installations.

## Requirements

### Requirement: Supported platform installations

Installation SHALL support Linux, macOS, and Windows on amd64 and arm64 and SHALL reject unsupported platform combinations. The selected installation SHALL contain the requested exact release's version metadata and platform Go executable.

#### Scenario: Supported archive format
- **WHEN** an official release archive is available for a supported platform
- **THEN** setup can prepare its platform-appropriate Go installation

#### Scenario: Unsupported platform
- **WHEN** setup runs for an unsupported operating system or architecture
- **THEN** installation fails rather than choosing another platform's toolchain

### Requirement: Inspect and reuse completed local installations

Installation SHALL discover completed matching toolchains in the runner tool cache and the action installation directory. Reuse SHALL require completion evidence, matching version metadata, and regular Go executable presence. Local reuse SHALL inspect these properties without promising complete rehashing or fresh authentication of local content.

#### Scenario: Completed exact match
- **WHEN** a usable completed local installation matches the selected exact version
- **THEN** setup reuses it without downloading that toolchain

#### Scenario: Incomplete candidate
- **WHEN** a candidate lacks completion evidence or the expected version/executable
- **THEN** it is excluded from usable installed candidates

### Requirement: Official acquisition and integrity verification

Fresh toolchains SHALL come from official Go release metadata and downloads over HTTPS. Installation SHALL verify the archive's exact size and SHA-256 against official metadata and verify the expected installation before publication. Invalid metadata, corrupt or truncated downloads, and extraction failures SHALL fail without publishing a completed installation.

#### Scenario: Verified fresh installation
- **WHEN** a complete official archive matches its metadata and contains the expected installation
- **THEN** setup publishes a usable installation for subsequent steps

#### Scenario: Corrupt download
- **WHEN** downloaded content has an incorrect size or digest
- **THEN** setup fails and does not publish that toolchain

### Requirement: Safe bounded extraction

Installation SHALL extract privately and reject unsafe paths, links, unsupported special entries, duplicate files, and archives exceeding independent expansion or entry safety limits before publication. These limits SHALL NOT be represented as total process-memory or filesystem-allocation quotas.

#### Scenario: Unsafe archive entry
- **WHEN** an archive contains traversal, a link, or a duplicate file destination
- **THEN** extraction fails without publishing the installation

#### Scenario: Excessive expansion
- **WHEN** a compressed archive exceeds the extraction safety budget
- **THEN** setup rejects it even if the compressed payload matches its metadata

### Requirement: Preserve existing destinations

Installation SHALL avoid replacing existing installations. A competing completed installation of the selected release can be reused; an incomplete existing destination SHALL fail rather than be overwritten. Other usable installed versions SHALL remain intact when acquisition fails.

#### Scenario: Incomplete destination already exists
- **WHEN** a selected release's destination already contains an incomplete installation
- **THEN** setup reports failure and preserves its contents

#### Scenario: Another usable version exists
- **WHEN** installing a new version fails verification
- **THEN** the previously usable version remains intact

### Requirement: Interrupted installation limits

Cooperatively cancelled or failed downloads/extraction SHALL NOT publish a completed toolchain and SHALL clean private staging during normal cleanup. Abrupt termination has best-effort cleanup only. Installation safety SHALL assume trusted local filesystem ownership rather than defend against hostile concurrent writers or promise shared-filesystem coordination.

#### Scenario: Cancellation during acquisition
- **WHEN** cancellation is observed before publication
- **THEN** setup returns failure without publishing a completed new installation and performs normal staging cleanup

#### Scenario: Forceful process termination
- **WHEN** the operating system terminates setup without allowing cleanup handlers to run
- **THEN** private temporary data can remain and no graceful-cleanup guarantee applies
