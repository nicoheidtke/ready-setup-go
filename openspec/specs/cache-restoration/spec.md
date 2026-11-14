# cache-restoration Specification

## Purpose

Define acceptance and filesystem publication of complete cache snapshots while preserving existing usable local cache contents on trusted runner filesystems.

## Requirements

### Requirement: Complete validation precedes local publication

Restoration SHALL validate the complete archive and compression stream in private staging before publishing any of its cache files. Truncation, corrupt compression, invalid archive termination or trailing data, and applicable byte or metadata budget violations SHALL reject the snapshot before publication. The public byte-limit contract belongs to cache-storage.

#### Scenario: Valid entry precedes corrupt data

- **WHEN** an archive contains a valid new file followed by corrupt or invalid archive data
- **THEN** neither that file nor any other entry from the archive is published to local caches

#### Scenario: Invalid final compression data

- **WHEN** a gzip or zstd stream fails its final integrity checks after file data has been staged
- **THEN** the snapshot is rejected without changing destination cache entries

#### Scenario: Metadata amplification exceeds limits

- **WHEN** archive entries exceed the bounded metadata, path-depth, or header budgets
- **THEN** the archive is rejected before local publication even if its file payload is small

### Requirement: Unsafe archive entries are rejected

Restoration SHALL accept only supported regular files and directories under the requested cache roots. It SHALL reject links, special or sparse entries, unsafe or nonportable paths, unknown roots, invalid metadata, duplicate names, case collisions including implicit ancestors, and file/directory path conflicts before publishing archive entries.

#### Scenario: Traversal or link entry

- **WHEN** an archive entry traverses outside a cache root or represents a symbolic link, hard link, or special file
- **THEN** the archive is rejected without publishing its entries

#### Scenario: Case collision

- **WHEN** archive paths differ only by case, including differently spelled parent directories
- **THEN** the archive is rejected consistently across supported platforms

#### Scenario: File is also an ancestor

- **WHEN** one entry is a regular file and another requires that path to be a directory
- **THEN** the archive is rejected regardless of entry order

### Requirement: Destination conflicts are checked before merge

Restoration SHALL inspect destination paths and ancestors before publication and reject symbolic-link paths or incompatible file/directory destinations. This acceptance guarantee assumes a trusted runner filesystem and does not promise protection against hostile concurrent filesystem writers.

#### Scenario: Existing destination is a link

- **WHEN** a destination path or ancestor is a symbolic link
- **THEN** restoration fails without following it to publish cache entries

#### Scenario: Destination file blocks a directory

- **WHEN** an existing regular file occupies a path needed as a directory by the archive
- **THEN** preflight rejects the merge before publishing other archive files

### Requirement: Existing regular files and unrelated contents are preserved

Restoration SHALL merge validated snapshots without replacing existing regular files or removing unrelated local contents. New files SHALL retain supported archived permissions and modification times, and temporarily changed existing directory permissions SHALL be restored during normal completion or cleanup.

#### Scenario: Local collision and unrelated file

- **WHEN** a validated snapshot contains a path already occupied by a local regular file alongside missing paths
- **THEN** the existing file wins, missing files are added, and unrelated local files remain

#### Scenario: Read-only module directories

- **WHEN** valid new files must be added beneath an existing read-only module directory
- **THEN** supported file metadata is restored and the existing directory regains its original permissions

#### Scenario: File appears before publication

- **WHEN** an ordinary local regular file appears at a missing destination before its final publication
- **THEN** publication preserves that local file instead of replacing it

### Requirement: Publication controls are bounded and cancellable

Restoration SHALL accept `cache-restore-concurrency` from 1 through 16, defaulting to 4, to bound validated-file publication work; 1 SHALL select sequential file publication. This control SHALL NOT increase download or chunk-processing concurrency. Normal cancellation or failure SHALL join publication workers before staging cleanup and directory-mode restoration.

#### Scenario: Sequential publication

- **WHEN** restore concurrency is 1
- **THEN** validated files publish sequentially with the same acceptance and preservation behavior

#### Scenario: Invalid publication worker count

- **WHEN** restore concurrency is outside 1 through 16
- **THEN** configuration is rejected

#### Scenario: Cancellation during publication

- **WHEN** cancellation interrupts active file publication
- **THEN** normal return waits for active publication workers before cleaning private staging or restoring directory modes

### Requirement: Merge failure limits remain observable

Restoration SHALL surface merge or cleanup errors rather than report a successful restore. A merge I/O failure or cancellation after validation can leave successfully added new files; existing regular files SHALL remain protected. Restoration SHALL NOT promise whole-tree rollback, and cleanup after abrupt process termination remains best effort.

#### Scenario: I/O failure after publication begins

- **WHEN** a valid snapshot encounters an I/O error after some new files have published
- **THEN** restoration fails, already added files can remain, and existing regular files are not overwritten or deleted

#### Scenario: Abrupt process termination

- **WHEN** the process terminates before normal cancellation and cleanup can finish
- **THEN** private staging can remain and no whole-tree rollback is promised
