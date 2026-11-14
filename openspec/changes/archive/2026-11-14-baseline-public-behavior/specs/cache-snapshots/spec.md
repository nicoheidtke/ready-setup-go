# Spec Delta

## Purpose

Define which local Go cache contents form snapshots and which eligible snapshots need saving, independently of job-level save permission and storage encoding.

## ADDED Requirements

### Requirement: Cache roots come from the selected Go executable

Enabled caching SHALL query `GOCACHE` and `GOMODCACHE` from the selected installed Go executable, respecting user cache-path settings. Root discovery SHALL use that selected toolchain independently of the caller's `GOROOT` or automatic toolchain switching setting. Nonabsolute, equal, or overlapping roots SHALL be rejected; `GOCACHE=off` SHALL be incompatible with caching. Root-discovery failures SHALL follow cache-policy operational error handling.

#### Scenario: User overrides cache locations

- **WHEN** user settings provide distinct absolute GOCACHE and GOMODCACHE paths
- **THEN** snapshot roots use those paths as reported by the selected Go executable

#### Scenario: Caller has an incompatible toolchain setting

- **WHEN** the caller's GOROOT or GOTOOLCHAIN would select another installation
- **THEN** root discovery still queries the chosen installed Go and does not change the caller's preserved GOTOOLCHAIN policy

#### Scenario: Build cache is disabled

- **WHEN** the selected Go reports GOCACHE as off
- **THEN** cache preparation rejects that root under the configured operational error policy

#### Scenario: Roots overlap

- **WHEN** module and build cache roots are equal or one contains the other
- **THEN** cache preparation rejects the root configuration

### Requirement: Layout controls snapshot grouping

`cache-layout` SHALL default to `split`, grouping modules and build contents into separate snapshots with independent restore/save outcomes. `combined` SHALL group both roots into one snapshot and consider its complete contents for each eligible save. Unsupported layout values SHALL fail input validation. Layout compatibility partitioning is owned by cache-identity.

#### Scenario: Default split layout

- **WHEN** enabled caching omits cache-layout
- **THEN** modules and build contents are prepared as separate snapshots

#### Scenario: Combined layout

- **WHEN** cache-layout is combined
- **THEN** one snapshot contains both module and build roots

#### Scenario: One split snapshot misses

- **WHEN** a module snapshot restores but no build snapshot matches
- **THEN** the module restoration remains usable and the build snapshot has its independent miss outcome

### Requirement: Unchanged restored modules skip packing and upload

For split module snapshots with a successful restored-content baseline, save SHALL compare current paths, permissions, and complete file contents with the restored snapshot before packing. Unchanged modules SHALL skip packing and upload even when timestamps differ. Same-size content changes or differing retained local contents SHALL count as changes. Combined snapshots SHALL NOT use this module-only skip rule.

#### Scenario: Only module timestamps change

- **WHEN** restored split module paths, permissions, and file contents are unchanged but timestamps differ
- **THEN** module save skips packing and upload

#### Scenario: Module content changes without size change

- **WHEN** a restored module file is replaced with different content of the same size
- **THEN** module save detects the change and considers the snapshot for publication

#### Scenario: Preserved local module differs from restored content

- **WHEN** restoration preserves an existing local module file whose contents differ from the archived file
- **THEN** the later module comparison treats the local tree as changed from the restored baseline

#### Scenario: Modules were not restored

- **WHEN** an eligible split module snapshot has no successful restored baseline
- **THEN** the absence of a baseline does not itself suppress packing or saving nonempty contents

### Requirement: Build saves refresh eligible nonempty snapshots

`cache-build-save` SHALL default to true. With build saving enabled, eligible nonempty build snapshots SHALL be considered for a fresh save even after a restore hit, without inferring unchanged builds from matching dependencies. In split layout, false SHALL skip build packing and upload before accessing the build root, without suppressing restores or module saves. False with combined layout SHALL fail validation.

#### Scenario: Build restore hit with default save policy

- **WHEN** an eligible job restored build contents and later has a nonempty build snapshot
- **THEN** a fresh build save is considered even when dependency inputs are unchanged

#### Scenario: Build saving disabled on a cold miss

- **WHEN** cache-build-save is false in split layout and no build snapshot restored
- **THEN** build packing and upload are skipped while restore behavior and changed-module saving remain enabled

#### Scenario: Disabled build root is unavailable

- **WHEN** build saving is disabled and the build root is unavailable at save time
- **THEN** the build-save skip does not inspect or pack that root

#### Scenario: Combined snapshot cannot disable only builds

- **WHEN** cache-build-save is false with combined layout
- **THEN** setup rejects the configuration

### Requirement: Empty snapshots do not upload

An eligible snapshot containing no regular files after applicable filtering SHALL skip upload. Missing cache roots SHALL contribute no files. An empty split snapshot SHALL NOT suppress an independently eligible nonempty snapshot. Snapshot preparation and packing failures SHALL follow cache-policy operational error handling rather than being reported as successful empty saves.

#### Scenario: Both roots are empty

- **WHEN** snapshot packing finds no regular files
- **THEN** no snapshot payload is uploaded

#### Scenario: Empty modules and nonempty builds

- **WHEN** split modules contain no files and the eligible build snapshot is nonempty
- **THEN** modules skip upload while the build snapshot can save independently

### Requirement: Build age filtering affects snapshot contents only

`cache-build-max-age` SHALL default to zero, disabling filtering, and accept nonnegative durations. For both archive and chunked storage, enabled snapshot packing SHALL omit recognized hash-named Go build objects older than the cutoff while retaining module entries and unrecognized build files. Filtering SHALL NOT delete or modify local files. Invalid durations or negative values SHALL fail configuration validation.

#### Scenario: Old canonical build object

- **WHEN** a recognized build-cache object is older than the configured positive age
- **THEN** it is omitted from the packed snapshot and remains present locally

#### Scenario: Modules and unrecognized build files are old

- **WHEN** module entries and unrecognized build files are older than the same cutoff
- **THEN** age filtering retains them in the snapshot

#### Scenario: Filtering is disabled

- **WHEN** cache-build-max-age is zero
- **THEN** object age does not exclude build entries from the snapshot

#### Scenario: Invalid age

- **WHEN** cache-build-max-age is malformed or negative
- **THEN** setup rejects the configuration
