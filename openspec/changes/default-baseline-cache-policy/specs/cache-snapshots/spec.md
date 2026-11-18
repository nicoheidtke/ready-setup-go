## ADDED Requirements

### Requirement: Dependency baseline saves avoid exact-match work

`cache-save-policy` SHALL default to `on-miss` and accept `on-miss` or `always`. On-miss SHALL skip snapshot scanning, fingerprinting, packing and upload after successfully restoring a snapshot matching the current dependency fingerprint. A compatible fallback, absent entry or failed restore SHALL NOT suppress a baseline save. Split snapshots SHALL decide independently; combined snapshots SHALL apply the same rule. Invalid policies SHALL fail validation.

#### Scenario: Matching baseline restored

- **WHEN** an on-miss snapshot restores successfully for the current dependency fingerprint
- **THEN** save skips all local snapshot access and upload even if source or local cache contents changed

#### Scenario: Changed dependencies restore a compatible baseline

- **WHEN** an eligible on-miss writer restores a snapshot for an older dependency fingerprint
- **THEN** it saves a nonempty baseline for the current fingerprint, including when restored module contents remain unchanged

#### Scenario: Independent split outcomes

- **WHEN** modules match current dependencies and the build snapshot misses
- **THEN** module save skips work and the build snapshot remains eligible for seeding

#### Scenario: Invalid or evicted snapshot

- **WHEN** lookup finds a matching snapshot but validation fails or a required chunk is missing
- **THEN** the lookup alone does not suppress a later otherwise eligible baseline save

#### Scenario: Always refresh is explicit

- **WHEN** cache-save-policy is always
- **THEN** eligible builds refresh after hits and split modules retain their unchanged-content optimization

## MODIFIED Requirements

### Requirement: Unchanged restored modules skip packing and upload

In always mode, split module snapshots with a restored-content baseline SHALL compare paths, permissions and complete contents before packing. Unchanged modules SHALL skip packing and upload despite timestamp changes. Same-size content changes and differing retained local contents SHALL count as changes. Combined snapshots SHALL NOT use this optimization. On-miss SHALL use dependency-match outcomes without module content checks.

#### Scenario: Only module timestamps change

- **WHEN** always mode restores split module paths, permissions and contents that remain unchanged except timestamps
- **THEN** module save skips packing and upload

#### Scenario: Module content changes without size change

- **WHEN** always mode replaces a restored module file with different content of the same size
- **THEN** module save detects the change and considers the snapshot for publication

#### Scenario: Preserved local module differs from restored content

- **WHEN** always-mode restoration preserves an existing local module file whose contents differ from the archived file
- **THEN** the later module comparison treats the local tree as changed from the restored baseline

#### Scenario: Modules were not restored

- **WHEN** an eligible split module snapshot has no successful restored baseline
- **THEN** absence of that baseline does not suppress packing or saving nonempty contents

### Requirement: Build saves refresh eligible nonempty snapshots

`cache-build-save` SHALL default to true. Enabled build saves SHALL follow cache-save-policy: on-miss seeds missing dependency baselines; always considers fresh nonempty snapshots after hits. In split layout, false SHALL skip build packing and upload before accessing the build root, without suppressing restores or otherwise eligible module saves. False with combined layout SHALL fail validation.

#### Scenario: Build restore hit with default save policy

- **WHEN** an eligible job restores build contents matching current dependencies under the default save policy
- **THEN** build save skips local snapshot access, packing and upload

#### Scenario: Build saving disabled on a cold miss

- **WHEN** cache-build-save is false in split layout and no build snapshot restored
- **THEN** build packing and upload are skipped while restore behavior and otherwise eligible module saving remain enabled

#### Scenario: Disabled build root is unavailable

- **WHEN** build saving is disabled and the build root is unavailable at save time
- **THEN** the build-save skip does not inspect or pack that root

#### Scenario: Combined snapshot cannot disable only builds

- **WHEN** cache-build-save is false with combined layout
- **THEN** setup rejects the configuration
