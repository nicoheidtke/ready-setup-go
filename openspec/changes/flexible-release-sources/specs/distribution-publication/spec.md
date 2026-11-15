## MODIFIED Requirements

### Requirement: Explicit publication and authentication boundaries

`release publish` SHALL default to preflight without retrieving API credentials or calling GitHub write APIs. Only `--execute` SHALL perform remote publication. Execution SHALL atomically push planned distribution/version-tag refs with exact guards without pushing source branches, then upload/verify assets and publish a stable release using the independently planned latest policy. API credentials SHALL remain separate from Git transport authentication and absent from Git command arguments.

#### Scenario: Dry run
- **WHEN** publish is invoked without execute
- **THEN** it checks the prepared plan without pushing refs or modifying GitHub release data

#### Scenario: Remote refs changed
- **WHEN** remote refs differ from both the planned starting state and exact completed push
- **THEN** execution refuses the conflicting update rather than forcing a replacement

### Requirement: Provenance and maintainer side-effect limits

Local checksum/provenance verification SHALL establish artifact consistency rather than publisher authenticity, hosted attestation, native coverage, or performance. Release commands SHALL NOT dispatch hosted workflows, alter repository settings, or move aliases outside the planned major/minor subset. Optional hosted attestations and release-immutability claims SHALL remain distinct from local build verification and require their own evidence.

#### Scenario: Local verified package
- **WHEN** a package passes local integrity and reproducibility checks
- **THEN** that result alone does not claim a GitHub-hosted attestation or successful execution on all target platforms

#### Scenario: Dated prerelease publication
- **WHEN** a dated private-era preview is requested
- **THEN** planning rejects the unsupported version without changing refs or repository settings

## ADDED Requirements

### Requirement: Immutable release source selection

The workflow SHALL accept a repository source branch and optional full commit SHA, require the commit to belong to that branch, and resolve one immutable source SHA before validation. Every candidate check and build SHALL use that SHA. Planning SHALL accept a clean historical checkout reachable from its recorded source anchor. Publication SHALL NOT push source branches or retarget a saved plan when a branch advances.

#### Scenario: Historical source
- **WHEN** a maintainer selects an older commit reachable from release/v1
- **THEN** validation, packaging, provenance, and publication use that commit without moving release/v1

#### Scenario: Unreachable commit
- **WHEN** a selected commit does not belong to the selected branch
- **THEN** resolution fails before candidate execution or publication

### Requirement: Independent latest and alias policies

Latest selection SHALL support auto, true, and false independently of alias updates. Auto SHALL mark only a version at least as new as existing stable version tags latest. Enabled alias updates SHALL advance only eligible major/minor series without downgrades. Planning SHALL freeze these decisions; resumption SHALL retain them and refuse conflicting alias writes.

#### Scenario: Older major maintenance
- **WHEN** v1.4.2 follows v1.4.1 and v2.0.0 using default policies
- **THEN** v1 and v1.4 advance while GitHub latest remains v2.0.0

#### Scenario: Older minor maintenance
- **WHEN** v1.4.2 follows v1.5.0
- **THEN** v1.4 can advance while v1 remains on v1.5.0

### Requirement: Current automation for historical releases

Release dispatch SHALL use reviewed automation from main while treating the selected source as a separate snapshot. Candidate checks/builds SHALL lack publication credentials. A separate publication job SHALL use the trusted publisher, exact prepared inputs, and recorded source. Requested hosted attestations SHALL succeed before publication. Release records SHALL distinguish automation and action source revisions.

#### Scenario: Old source lacks current publisher
- **WHEN** a compatible historical source is selected
- **THEN** current trusted publication tooling handles its prepared distribution while its own source checks remain enforced

#### Scenario: Requested attestation fails
- **WHEN** an enabled attestation job fails
- **THEN** publication does not execute
