# cache-policy Specification

## Purpose

Define when setup may restore or save Go caches and how configuration, runner access, and main/post compatibility constrain those operations.

## Requirements

### Requirement: Cache mode selects permitted lifecycle work

Caching SHALL default to `off` and accept `off`, `restore`, or `restore-save`. Off SHALL avoid cache-root discovery, dependency scanning, cache transfers, snapshot creation, and save registration. Restore SHALL request restoration without registering a save. Restore-save SHALL request restoration and an automatic post-job save when preparation and runner access permit it.

#### Scenario: Caching is omitted

- **WHEN** setup runs without a cache mode
- **THEN** Go setup proceeds without cache preparation, transfer, or post-save registration

#### Scenario: Restore-only use

- **WHEN** `cache: restore` is selected
- **THEN** restoration can run and no automatic snapshot save is registered

#### Scenario: Restore-save with a cold cache

- **WHEN** restore-save preparation succeeds with write access but no snapshot matches
- **THEN** the miss does not prevent registering an eligible post-job save

### Requirement: Runner restrictions constrain requested operations

Cache operations SHALL honor known `ACTIONS_CACHE_MODE` restrictions: `read` permits restore only, `write-only` permits save only, and `none` permits neither. Post SHALL recheck current write permission. Unset or unknown signals SHALL preserve older-runner compatibility and leave enforcement to the service. Intentional policy skips SHALL not themselves fail strict mode.

#### Scenario: Read-only runner

- **WHEN** restore-save is requested with runner mode `read`
- **THEN** restore is permitted and automatic saving is not registered

#### Scenario: Write-only runner

- **WHEN** restore-save is requested with runner mode `write-only`
- **THEN** lookup and download are skipped while preparation can register a later save

#### Scenario: Runner permission narrows after main

- **WHEN** main registered a save and post observes runner mode `read` or `none`
- **THEN** post skips saving without attempting a service write

#### Scenario: Known policy disables all caching

- **WHEN** runner mode `none` accompanies an otherwise valid cache configuration
- **THEN** the requested cache work is skipped without treating the policy restriction as an operational error

### Requirement: Automatic saves respect job outcome

Automatic post-save SHALL run only for successful jobs by default. `cache-save-on-failure` SHALL default to false; true SHALL additionally allow failed jobs to save. Cancelled jobs SHALL never qualify for automatic post-save. Qualification SHALL remain subject to restore-save mode, valid main/post handoff, and runner write permission.

#### Scenario: Successful eligible job

- **WHEN** a successful job has a registered restore-save handoff and write access
- **THEN** its automatic post phase may save snapshots according to cache-snapshots decisions

#### Scenario: Failed job without opt-in

- **WHEN** a job fails and cache-save-on-failure is false
- **THEN** its automatic post-save is excluded

#### Scenario: Failed job with opt-in

- **WHEN** a failed job has cache-save-on-failure true and otherwise qualifies
- **THEN** its automatic post-save is permitted

#### Scenario: Cancelled job with failure saves enabled

- **WHEN** a job is cancelled with cache-save-on-failure true
- **THEN** the automatic post-save condition still excludes it

### Requirement: Configuration errors are distinct from operational failures

Invalid cache configuration SHALL fail setup regardless of `cache-strict`. Operational cache failures SHALL warn and allow continuation by default; `cache-strict: true` SHALL propagate them as failures. Ordinary misses and intentional skips SHALL remain non-errors. The domain capability owning each option SHALL define its accepted values and restrictions.

#### Scenario: Invalid mode while strictness is disabled

- **WHEN** an unsupported cache mode is supplied with cache-strict false
- **THEN** setup fails input validation

#### Scenario: Credentials are unavailable

- **WHEN** cache work requires unavailable runtime credentials
- **THEN** default policy warns and continues, while strict policy fails

#### Scenario: Transfer or archive processing fails

- **WHEN** requested cache work encounters a transport, permission, or archive error
- **THEN** the configured operational error policy determines whether setup or post fails

### Requirement: Main and post retain compatible save intent

The main/post handoff SHALL retain the prepared snapshot identities, paths, and relevant save configuration without persisting runtime bearer credentials or signed URLs. Post SHALL use freshly injected credentials, accept compatible saved configuration, and reject missing, corrupt, or incompatible referenced state under the configured error policy. When no state is registered, post SHALL perform no cache save.

#### Scenario: Fresh post credentials

- **WHEN** runtime credentials change between successful main preparation and post
- **THEN** post uses its current credentials with the saved snapshot identities and configuration

#### Scenario: Compatible older configuration omits build-save opt-out

- **WHEN** a compatible saved configuration omits the later build-save opt-out field
- **THEN** the existing default of allowing build saves is retained

#### Scenario: Referenced state is corrupt

- **WHEN** post is given a registered state reference whose contents are corrupt or incompatible
- **THEN** no save is performed from that state and the configured error policy applies

#### Scenario: No state was registered

- **WHEN** post has no registered cache state
- **THEN** it performs no cache save

### Requirement: Overlap preserves operation boundaries

`cache-overlap` SHALL default to true and permit cache lookup/download work after exact Go selection while toolchain preparation runs. Destination discovery and publication SHALL use the selected installed Go toolchain after setup succeeds. False SHALL retain sequential operation. Normal cancellation cleanup SHALL stop and join outstanding prefetch work before removing its temporary files; abrupt termination cleanup remains best effort.

#### Scenario: Overlapped download completes before installation

- **WHEN** prefetch completes while the selected toolchain is still being prepared
- **THEN** cache data is not published to destination caches until toolchain setup succeeds and supplies its roots

#### Scenario: Overlap is disabled

- **WHEN** cache-overlap is false
- **THEN** cache restoration starts after successful toolchain preparation

#### Scenario: Toolchain preparation fails during prefetch

- **WHEN** setup exits with toolchain preparation failure after prefetch starts
- **THEN** normal cleanup cancels and joins prefetch work without publishing the prefetched cache data

### Requirement: Cancellation stops further cache snapshot work

Cache processing SHALL observe cancellation and stop starting subsequent snapshots. Cancellation errors SHALL follow the configured cache operational error policy. Normal cleanup SHALL wait for active workers before removing their private resources; this SHALL NOT imply immediate interruption of blocking filesystem calls or cleanup after forceful process termination.

#### Scenario: Cancellation before any snapshot

- **WHEN** cache processing starts with an already cancelled context
- **THEN** no snapshot lookup or upload begins and strict mode propagates cancellation while default policy warns

#### Scenario: Cancellation between snapshots

- **WHEN** cancellation occurs during the first split snapshot
- **THEN** the next snapshot does not start
