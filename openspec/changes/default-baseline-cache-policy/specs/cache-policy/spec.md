## ADDED Requirements

### Requirement: Save branches default to the repository default branch

`cache-save-branches` SHALL default to `default` and accept `default` or `all`. Default SHALL permit saves only when the actual runner ref is the repository default branch established from bounded event metadata. Missing, malformed, oversized or inconsistent metadata SHALL disable saves without failing restores or strict mode. All SHALL permit branch-local saves subject to lifecycle and runner permissions. Post SHALL recheck this policy.

#### Scenario: Default branch writer

- **WHEN** valid runner event metadata identifies the repository default branch and the runner ref matches that branch
- **THEN** default branch policy permits an otherwise eligible save

#### Scenario: Feature branch or pull request

- **WHEN** the runner ref is a feature branch, tag or pull-request merge ref under default policy
- **THEN** caching restores normally and saves are skipped

#### Scenario: Default branch cannot be established

- **WHEN** event metadata is absent, invalid, larger than 1 MiB or belongs to another repository
- **THEN** default-policy saves are skipped with a fixed explanatory reason and restore remains available

#### Scenario: Branch-local writes are enabled

- **WHEN** cache-save-branches is all
- **THEN** an otherwise eligible job can save within GitHub's existing ref scope without requiring default-branch metadata

#### Scenario: Branch context changes before post

- **WHEN** main permits saving but post no longer establishes an eligible ref
- **THEN** post skips saving before backend construction or snapshot scanning

#### Scenario: Invalid branch policy

- **WHEN** an unsupported cache-save-branches value is supplied
- **THEN** configuration validation fails regardless of strictness

## MODIFIED Requirements

### Requirement: Main and post retain compatible save intent

The main/post handoff SHALL retain prepared snapshot identities, paths, successful dependency-match outcomes and save configuration without runtime bearer credentials or signed URLs. Post SHALL use fresh credentials and reject missing, corrupt or incompatible state under the operational error policy. Compatible old state omitting the new save policies SHALL retain always-refresh and all-branch eligibility. No registered state SHALL perform no save.

#### Scenario: Fresh post credentials

- **WHEN** runtime credentials change between successful main preparation and post
- **THEN** post uses its current credentials with the saved snapshot identities and configuration

#### Scenario: Compatible older configuration omits build-save opt-out

- **WHEN** a compatible saved configuration omits the later build-save opt-out field
- **THEN** the existing default of allowing build saves is retained

#### Scenario: Compatible older configuration omits baseline policies

- **WHEN** compatible saved configuration lacks cache-save-policy and cache-save-branches
- **THEN** post retains legacy always-refresh and all-branch behavior rather than silently interpreting the state as a new baseline invocation

#### Scenario: Referenced state is corrupt

- **WHEN** post is given a registered state reference whose contents are corrupt or incompatible
- **THEN** no save is performed from that state and the configured error policy applies

#### Scenario: No state was registered

- **WHEN** post has no registered cache state
- **THEN** it performs no cache save
