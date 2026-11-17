# action-acquisition Specification

## Purpose

Acquire the action's native executable for the runner platform from a pinned bundled or lightweight distribution before entering native setup.

## Requirements

### Requirement: Distribution-based executable selection

Root and Node-alias actions SHALL select the matching Linux, macOS, or Windows executable for amd64 or arm64 and reject unsupported platforms. Bundled distributions SHALL execute their included platform binary without an action-binary download. Lightweight distributions SHALL acquire only their matching platform asset from the pinned download manifest. Source-only revisions SHALL NOT be treated as runnable distributions.

#### Scenario: Bundled offline execution
- **WHEN** a supported runner uses a complete bundled distribution
- **THEN** it starts the included matching executable without fetching an action binary

#### Scenario: Lightweight platform selection
- **WHEN** a supported runner uses a lightweight distribution
- **THEN** acquisition selects only that platform's manifest asset

### Requirement: Pinned download verification and reuse

Lightweight acquisition SHALL validate manifest structure, supported platform, locator, size, and digest bounds. Before execution it SHALL verify exact downloaded byte count and SHA-256 against metadata in the pinned distribution, not a remotely obtained checksum. Reused temporary executables, including post-job reuse, SHALL pass the same size/digest check and be regular files rather than symlinks.

#### Scenario: Post reuses downloaded bytes
- **WHEN** post finds the already acquired matching executable
- **THEN** it rechecks its size and digest before execution without needing another asset download

#### Scenario: Reused executable changed
- **WHEN** an existing temporary executable has changed or become a symlink
- **THEN** acquisition fails instead of executing it

### Requirement: Public credential-free acquisition

Lightweight acquisition SHALL use public GitHub release assets over HTTPS with bounded redirects confined to approved GitHub asset hosts and without embedded URL credentials. It SHALL NOT forward runtime cache credentials, `GITHUB_TOKEN`, PATs, or an Authorization header. Private/offline access SHALL require a bundled distribution rather than authenticated lightweight fallback.

#### Scenario: Asset redirects to an external host
- **WHEN** a release asset redirects outside the approved HTTPS GitHub asset hosts
- **THEN** acquisition rejects the redirect without sending credentials

#### Scenario: Private asset requires authentication
- **WHEN** a lightweight asset cannot be fetched anonymously
- **THEN** setup fails and directs the consumer to bundled distribution use

### Requirement: Verified publication and acquisition failure

Lightweight acquisition SHALL publish a temporary executable for use only after complete verification and SHALL refuse corrupt, truncated, excessive, or unusable responses. Failed or cooperatively cancelled acquisition SHALL NOT start a partial executable or fall back to unverified bytes or another version. Normal cleanup SHALL remove private partial downloads; abrupt termination retains the setup-execution cleanup limits.

#### Scenario: Digest mismatch
- **WHEN** downloaded bytes do not match the pinned digest
- **THEN** setup fails without publishing or executing those bytes

#### Scenario: Cancellation during download
- **WHEN** acquisition observes cancellation before executable publication
- **THEN** it stops acquisition and does not launch native setup

#### Scenario: Mutable asset locator changes
- **WHEN** the public asset is replaced with different bytes or removed
- **THEN** acquisition fails verification or availability rather than weakening the pinned integrity requirement
