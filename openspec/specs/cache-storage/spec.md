# cache-storage Specification

## Purpose

Define the supported remote cache representations, encoding choices, and bounded verified transfers that make complete snapshots available for restoration.

## Requirements

### Requirement: Supported storage and bundled codecs

The cache SHALL support `cache-storage` values `archive` and `chunked`, defaulting to `archive`, with `cache-compression` values `gzip`, `zstd`, and `none`, defaulting to `gzip`. Encoding and decoding SHALL use bundled capabilities without requiring runner compression or archive executables.

#### Scenario: Archive storage with defaults

- **WHEN** caching is enabled without storage or compression overrides
- **THEN** snapshots use whole-archive storage with gzip compression

#### Scenario: Chunked storage with an alternate codec

- **WHEN** chunked storage is selected with zstd or no compression
- **THEN** snapshot data uses that codec and is recoverable without an external codec tool

#### Scenario: Unsupported representation

- **WHEN** an unsupported storage or compression value is supplied
- **THEN** configuration is rejected

### Requirement: Gzip compression level control

The cache SHALL accept `cache-gzip-level` as `default` or an integer from 1 through 9, defaulting to the existing standard-library compression level. Valid levels SHALL affect gzip encoding for archive and chunked storage and SHALL be ignored for other codecs. Changing level SHALL preserve gzip decoder compatibility, while newly encoded chunk identities can differ. Cache-identity owns namespace compatibility.

#### Scenario: Explicit gzip level

- **WHEN** gzip compression is selected with level 1
- **THEN** newly encoded data uses that level and remains readable by the supported gzip restore path

#### Scenario: Level with another codec

- **WHEN** zstd or none is selected with a valid gzip level
- **THEN** the selected codec is used without applying the gzip level

#### Scenario: Invalid gzip level

- **WHEN** a gzip level outside the supported choices is supplied
- **THEN** configuration is rejected even when the selected codec is not gzip

### Requirement: Independent compression and chunk concurrency controls

The cache SHALL accept `cache-compression-concurrency` and `cache-chunk-concurrency` from 1 through 16, each defaulting to 4. The former SHALL control whole-archive compression; chunked storage SHALL use its separate chunk-processing bound. Setting a control to 1 SHALL provide sequential processing for its applicable work. Cache-identity owns compatibility when tuning changes.

#### Scenario: Sequential chunk processing

- **WHEN** chunked storage uses `cache-chunk-concurrency: 1`
- **THEN** chunk compression and transfer processing is sequential

#### Scenario: Bounded independent chunk transfers

- **WHEN** chunked storage is selected with multiple chunk workers and larger upload/download worker settings
- **THEN** nested blob worker settings do not multiply the chunk-processing concurrency bound

#### Scenario: Invalid compression worker count

- **WHEN** either compression or chunk concurrency is below 1 or above 16
- **THEN** configuration is rejected

### Requirement: Upload transfer controls

The cache SHALL accept `cache-upload-concurrency` from 1 through 32, defaulting to 4, and `cache-upload-chunk-bytes` from 1 through 4194304000, defaulting to 33554432. Applicable whole-archive uploads SHALL honor these bounded transfer controls.

#### Scenario: Upload configuration

- **WHEN** a supported upload worker count and block size are supplied
- **THEN** archive transfers use those controls while preserving the complete snapshot bytes

#### Scenario: Invalid upload control

- **WHEN** either upload control is outside its accepted range
- **THEN** configuration is rejected

### Requirement: Download transfer controls and validated fallback

The cache SHALL accept `cache-download-concurrency` from 1 through 32, defaulting to 4, and `cache-download-chunk-bytes` from 1 through 4194304000, defaulting to 33554432. Whole-archive downloads SHALL use bounded range transfers when supported, validate returned ranges and lengths, and accept a complete initial response as a single-download fallback. Concurrency 1 SHALL use a single GET.

#### Scenario: Server sends a full response

- **WHEN** the initial range request receives the complete archive instead of a partial response
- **THEN** that response is consumed without downloading the archive a second time

#### Scenario: Inconsistent range response

- **WHEN** a range response has an inconsistent offset, length, total size, or supplied object identity
- **THEN** the transfer fails and its partial data is not offered for restoration

#### Scenario: Sequential download

- **WHEN** download concurrency is 1
- **THEN** the archive is requested through the single-GET path

#### Scenario: Invalid download control

- **WHEN** either download control is outside its accepted range
- **THEN** configuration is rejected

### Requirement: Public snapshot byte limit

The cache SHALL accept `cache-max-bytes` from 1 through 10737418240, defaulting to 10737418240. This limit SHALL bound expanded tar bytes including headers and padding and transferred archive bytes. Chunked snapshots SHALL also reject inconsistent or excessive aggregate encoded and expanded sizes. These limits SHALL apply before a snapshot is accepted or published; they are not a total process-memory or disk quota.

#### Scenario: Expanded archive exceeds the limit

- **WHEN** decoding expands an archive beyond the configured maximum
- **THEN** the snapshot is rejected without publishing cache files

#### Scenario: Encoded transfer exceeds the limit

- **WHEN** a transfer declares or yields more archive data than the configured maximum
- **THEN** the transfer is rejected

#### Scenario: Invalid maximum

- **WHEN** the maximum is zero, negative, or greater than 10737418240
- **THEN** configuration is rejected

### Requirement: Verified chunk reconstruction and reuse

Chunked storage SHALL validate manifest structure, reference bounds, chunk encoded digests and sizes, expanded sizes, and aggregate consistency before handing a reconstructed archive to restoration. Reuse SHALL require an exact finalized chunk lookup; a reservation conflict alone SHALL NOT establish availability. A missing referenced chunk SHALL produce a cache miss rather than an incomplete restore.

#### Scenario: Reuse of an available chunk

- **WHEN** an unchanged chunk has an exact finalized remote entry in the compatible namespace
- **THEN** it can be referenced without uploading its payload again

#### Scenario: Referenced chunk was evicted

- **WHEN** a manifest exists but a referenced chunk no longer exists
- **THEN** the snapshot is a miss and no reconstructed partial archive is restored

#### Scenario: Corrupt chunk or invalid manifest

- **WHEN** a chunk digest or size differs from its reference, or manifest bounds are invalid
- **THEN** reconstruction fails before local cache publication

#### Scenario: Uncommitted reservation conflict

- **WHEN** a chunk upload conflicts but no exact finalized chunk can be found
- **THEN** the conflict is not accepted as reusable chunk data

### Requirement: Complete remote publication

The cache SHALL report a snapshot saved only after archive/codec finalization, required payload transfers, and service finalization succeed. Chunked manifests SHALL be published only after every referenced chunk is available as finalized data. Transfer, encoding, commit, or finalization failure SHALL prevent successful publication of that snapshot.

#### Scenario: Payload upload fails

- **WHEN** a required payload upload fails
- **THEN** the snapshot is not finalized as a successful save

#### Scenario: Chunk data remains unavailable

- **WHEN** any chunk cannot be uploaded or confirmed available
- **THEN** its snapshot manifest is not published

#### Scenario: Service finalization refuses the entry

- **WHEN** all payload bytes transfer but service finalization fails or is refused
- **THEN** the save is an operational failure rather than a successful publication

### Requirement: Cache service credential separation

The cache SHALL use runner-issued credentials for cache service control requests, without requiring a PAT or extra GITHUB_TOKEN permission. Blob transfers SHALL use their signed URLs without a bearer Authorization header. Control and blob redirects SHALL be refused. Cache-policy owns credential-free main/post persistence.

#### Scenario: Blob download or upload

- **WHEN** the service returns a signed blob URL
- **THEN** the blob request uses that URL without forwarding the runner bearer token

#### Scenario: Redirected endpoint

- **WHEN** a cache control or blob endpoint responds with a redirect
- **THEN** the request is refused instead of forwarding credentials to the destination

### Requirement: Bounded cancellable transfer work

Cache transfer and encoding work SHALL bound concurrency and retained payload buffers, propagate cancellation, and join active workers before normal private-file cleanup. Retries SHALL be bounded and limited to replay-safe operations; reservation, finalization, partially consumed bodies, and invalid content SHALL NOT be replayed as transient successes. Abrupt termination can leave private files or uncommitted remote data.

#### Scenario: Cancellation during parallel work

- **WHEN** cancellation interrupts active transfer or encoding workers
- **THEN** normal return waits for those workers and does not publish an incomplete snapshot

#### Scenario: Ambiguous service finalization failure

- **WHEN** a finalization request fails ambiguously
- **THEN** it is not retried as though the first finalization were known to have failed
