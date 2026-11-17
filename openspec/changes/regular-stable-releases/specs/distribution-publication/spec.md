## MODIFIED Requirements

### Requirement: Exact-byte local preparation

`release prepare` SHALL create and verify exact-byte distribution Git objects and an annotated local tag, preserving required executable modes and the planned distribution parent when present and the exact source parent. It SHALL retain the source checkout/index and perform no remote writes. Repeating preparation with the same unchanged plan SHALL reuse its matching prepared objects; changed tags or committed content SHALL fail validation.

#### Scenario: Prepare on Windows
- **WHEN** local preparation runs on a filesystem whose modes cannot establish Unix Git modes
- **THEN** committed Unix executables still receive the required executable Git mode

#### Scenario: Repeat preparation
- **WHEN** preparation resumes the same plan with matching local objects
- **THEN** it preserves those object identities rather than manufacturing a competing distribution

### Requirement: Explicit publication and authentication boundaries

`release publish` SHALL default to preflight without retrieving API credentials or calling GitHub write APIs. Only `--execute` SHALL perform remote publication. Execution SHALL atomically push planned source/distribution/version-tag refs with exact guards and source already matching its remote branch, then upload/verify assets and publish a stable release, marking it latest only when promotion is enabled. API credentials SHALL remain separate from Git transport authentication and absent from Git command arguments.

#### Scenario: Dry run
- **WHEN** publish is invoked without execute
- **THEN** it checks the prepared plan without pushing refs or modifying GitHub release data

#### Scenario: Remote refs changed
- **WHEN** remote refs differ from both the planned starting state and exact completed push
- **THEN** execution refuses the conflicting update rather than forcing a replacement

### Requirement: Provenance and maintainer side-effect limits

Local checksum/provenance verification SHALL establish artifact consistency rather than publisher authenticity, hosted attestation, native coverage, or performance. Release commands SHALL NOT dispatch hosted workflows, alter repository settings, or move aliases outside the planned major/minor pair. Optional hosted attestations and release-immutability claims SHALL remain distinct from local build verification and require their own evidence.

#### Scenario: Local verified package
- **WHEN** a package passes local integrity and reproducibility checks
- **THEN** that result alone does not claim a GitHub-hosted attestation or successful execution on all target platforms

#### Scenario: Dated prerelease publication
- **WHEN** the maintainer requests publication using a private-era dated preview tag
- **THEN** the public stable publisher rejects the tag before changing refs or release data

#### Scenario: Stable release publication
- **WHEN** the maintainer explicitly executes publication
- **THEN** the publisher confines its changes to the planned refs, selected aliases, and release assets/data without changing other aliases or repository settings

## ADDED Requirements

### Requirement: Stable release versions and promotion

Planning SHALL accept exact stable vMAJOR.MINOR.PATCH versions without leading zeroes and reject dated previews, aliases, unsupported suffixes, and reused tags. Normal publication SHALL apply the independently selected latest and eligible major/minor alias policies after verifying release assets. Maintenance publication SHALL support advancing an older version line without replacing GitHub latest or moving aliases backwards.

#### Scenario: Normal release
- **WHEN** v1.2.3 is published with promotion
- **THEN** its fixed version identifies its packaged distribution and v1/v1.2 move to that distribution after verification

#### Scenario: Alias conflict
- **WHEN** an alias changes after planning to an unexpected value
- **THEN** promotion refuses replacement and the same plan remains available for inspection

#### Scenario: Maintenance release
- **WHEN** a maintainer disables latest and alias updates for an older version
- **THEN** the exact release is published without moving aliases or latest

### Requirement: First distribution publication

Planning SHALL accept an absent distribution branch without creating it. Execution SHALL atomically create that branch and the version tag with an absent-ref guard. A competing branch creation SHALL fail without partially publishing refs.

#### Scenario: First release race
- **WHEN** another writer creates the absent distribution branch after planning
- **THEN** execution refuses the stale plan and preserves remote refs

### Requirement: Manual hosted release workflow

The manual release workflow SHALL run reusable six-platform validation and reproducible packaging before optional publication. Publication SHALL require reviewed automation from main, a selected immutable source, a version, and maintainer authorization. Notes SHALL be generated when omitted. Preparation alone SHALL remain the default. Release runs SHALL serialize without cancellation and retain recovery inputs before remote writes.

#### Scenario: Validation fails
- **WHEN** a native validation or packaging gate fails
- **THEN** publication does not execute

#### Scenario: Prepare only
- **WHEN** publication is not selected
- **THEN** the workflow writes no remote Git refs or release data

### Requirement: Main-only source and release-owned provenance

Source and documentation changes SHALL merge into main or a maintained source branch before release. Generated distribution history SHALL remain separate and never require merging into main. Consumer examples SHALL use the usage guide's recommended major, a matching exact version, or a full distribution SHA. Exact source/distribution provenance and validation evidence SHALL be recorded with each release.

#### Scenario: Patch release
- **WHEN** a compatible patch release is published
- **THEN** major-version examples continue to work without a post-release source documentation commit
