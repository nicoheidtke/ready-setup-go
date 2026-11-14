# distribution-publication Specification

## Purpose

Produce verifiable native action distributions and support reviewable maintainer release preparation, explicit publication, and conflict-safe recovery.

## Requirements

### Requirement: Packaging modes and output preservation

Packaging SHALL default to a bundled six-platform distribution and support a single-platform diagnostic subset. Lightweight packaging SHALL retain standalone assets and pin their exact sizes/digests while omitting bundled binaries; it SHALL require an asset destination and fixed public release locator. Packaging SHALL reject nonempty or overlapping output/asset directories without deleting existing contents.

#### Scenario: Stale output directory
- **WHEN** packaging targets a directory containing prior files
- **THEN** it refuses that destination and preserves its contents

#### Scenario: Lightweight package
- **WHEN** valid lightweight packaging options are supplied
- **THEN** the distribution contains the native-download manifest and bootstrap, omits bundled binaries, and retains matching native release assets

### Requirement: Source provenance and reproducible candidates

Packaging SHALL record source revision, clean/dirty status, source-content identity, compiler, and platform set, and reject source changes detected during packaging. Clean-required builds SHALL reject dirty sources; dirty diagnostic builds SHALL be distinguishable and require explicit verifier acceptance. Release planning SHALL require two distinct byte-identical distribution and asset builds matching the current clean source and compiler.

#### Scenario: Source changes while packaging
- **WHEN** source identity changes during package creation
- **THEN** packaging fails rather than declaring the output a valid completed candidate

#### Scenario: Reproduction differs
- **WHEN** the two candidate builds have different packaged or asset bytes
- **THEN** release planning refuses them

### Requirement: Complete distribution integrity verification

Verification SHALL check complete regular-file checksums, required runtime files, platform set, provenance, and checkout bytes. When an asset directory is supplied, it SHALL also verify standalone/bundled or manifest asset agreement and ZIP content/modes. Tampering, missing/extra covered files, incompatible platform declarations, and dirty candidates without opt-in SHALL fail. Lightweight release locators SHALL match the planned repository and tag.

#### Scenario: Checksum coverage is incomplete
- **WHEN** a distribution contains an unlisted regular file or altered covered bytes
- **THEN** verification fails

#### Scenario: Manifest and native asset disagree
- **WHEN** an asset directory is supplied and a lightweight asset differs in size or digest from the pinned manifest
- **THEN** distribution verification fails before release publication

#### Scenario: Asset verification is omitted
- **WHEN** verification runs without an asset directory
- **THEN** it checks the distribution without claiming release-asset or ZIP verification

### Requirement: Reviewable release plan

`release plan` SHALL validate candidates, source/branch identity, destination repository/refs/tag, and notes before saving a reviewable plan in an ignored path. It SHALL leave Git refs and GitHub data unchanged and refuse overwriting an existing plan. Release remotes SHALL identify the selected GitHub repository without embedded credentials or ambiguous push destinations. Release commands do not execute test, lint, or native validation suites.

#### Scenario: Existing plan
- **WHEN** planning targets an already existing plan file
- **THEN** it refuses overwriting and requires inspection or resumption

#### Scenario: Ambiguous push destination
- **WHEN** a configured remote has multiple or mismatched push destinations
- **THEN** release validation refuses publication

### Requirement: Exact-byte local preparation

`release prepare` SHALL create and verify exact-byte distribution Git objects and an annotated local tag, preserving required executable modes and the planned preview parent. It SHALL retain the source checkout/index and perform no remote writes. Repeating preparation with the same unchanged plan SHALL reuse its matching prepared objects; changed tags or committed content SHALL fail validation.

#### Scenario: Prepare on Windows
- **WHEN** local preparation runs on a filesystem whose modes cannot establish Unix Git modes
- **THEN** committed Unix executables still receive the required executable Git mode

#### Scenario: Repeat preparation
- **WHEN** preparation resumes the same plan with matching local objects
- **THEN** it preserves those object identities rather than manufacturing a competing distribution

### Requirement: Explicit publication and authentication boundaries

`release publish` SHALL default to preflight without retrieving API credentials or calling GitHub write APIs. Only `--execute` SHALL perform remote publication. Execution SHALL atomically push planned source/preview/tag refs with exact guards and a fast-forward source update, then upload/verify assets and publish a prerelease without marking it latest. API credentials SHALL remain separate from Git transport authentication and absent from Git command arguments.

#### Scenario: Dry run
- **WHEN** publish is invoked without execute
- **THEN** it checks the prepared plan without pushing refs or modifying GitHub release data

#### Scenario: Remote refs changed
- **WHEN** remote refs differ from both the planned starting state and exact completed push
- **THEN** execution refuses the conflicting update rather than forcing a replacement

### Requirement: Verified publication and recoverable interruption

Publication SHALL verify expected asset names, sizes, and server SHA-256 digests before publishing and verify the fetched published release again before reporting success. Resuming the same plan SHALL accept exact already-pushed refs and matching draft/published data. Duplicate releases, changed notes/tags/assets, or missing assets in a published release SHALL fail rather than trigger destructive replacement. Ambiguous writes SHALL NOT be automatically retried.

#### Scenario: Interrupted matching upload
- **WHEN** publication resumes with matching already uploaded draft assets
- **THEN** it reuses verified assets and continues the same planned release

#### Scenario: Final verification unavailable
- **WHEN** publication may have succeeded but its final verification cannot complete
- **THEN** the command reports failure and supports inspection/resumption without claiming the release was absent

### Requirement: Provenance and maintainer side-effect limits

Local checksum/provenance verification SHALL establish artifact consistency rather than publisher authenticity, hosted attestation, native coverage, or performance. Release commands SHALL NOT dispatch hosted workflows, alter repository settings, or move stable aliases. Optional hosted attestations and release-immutability claims SHALL remain distinct from local build verification and require their own evidence.

#### Scenario: Local verified package
- **WHEN** a package passes local integrity and reproducibility checks
- **THEN** that result alone does not claim a GitHub-hosted attestation or successful execution on all target platforms

#### Scenario: Dated prerelease publication
- **WHEN** the maintainer explicitly executes publication
- **THEN** the publisher confines its changes to the planned refs and prerelease assets/data without changing stable aliases or repository settings
