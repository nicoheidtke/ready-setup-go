# diagnostics Specification

## Purpose

Explain setup and cache outcomes through meaningful outputs, concise presentation, and detailed reports while preserving metric and privacy boundaries.

## Requirements

### Requirement: Cache output meanings and complete-hit aggregation

Enabled-cache outputs SHALL report `cache-hit` true only when every requested snapshot restores successfully. `cache-key` SHALL identify fresh invocation snapshot keys and `cache-matched-key` the returned lookup matches, each comma-separated. `cache-restored-bytes` SHALL report received archive payload bytes; `cache-restore-ms` SHALL report restore operation milliseconds excluding path discovery and dependency fingerprinting.

#### Scenario: Partial restore
- **WHEN** one requested snapshot restores and another misses or fails validation
- **THEN** `cache-hit` is false even if lookup returned matched keys

#### Scenario: Cache unavailable before restoration
- **WHEN** enabled caching cannot prepare a usable restore
- **THEN** hit is false and unavailable restore bytes and time remain zero

### Requirement: Operational result labels and key context

Presentation SHALL distinguish `HIT`, `MISS`, `SKIP`, `SAVED`, and `ERROR`, identify partial restores and dependency-compatible fallback, and show lookup/matched/save key context. A matched key SHALL NOT establish successful restoration, and a save key SHALL NOT establish publication. Ordinary misses and intentional skips SHALL remain informational; operational errors SHALL follow cache-policy strictness.

#### Scenario: Matched corrupt data
- **WHEN** lookup finds a snapshot but validation fails
- **THEN** presentation identifies the error rather than reporting a hit solely from the matched key

#### Scenario: Build save disabled
- **WHEN** an eligible build snapshot is skipped by its save control
- **THEN** its result is SKIP with a reason even if a save key is present

### Requirement: Optional separate job summaries

In GitHub Actions with a writable `GITHUB_STEP_SUMMARY`, setup SHALL append concise toolchain/restore results and post SHALL append separate save results. `summary` SHALL default true; false SHALL disable both phases. Summaries SHALL escape text, omit full keys and paths, and retain outcome/reason information. Missing summary destinations and summary-write failures SHALL NOT fail setup or post.

#### Scenario: Summary opt-out persists
- **WHEN** main receives `summary=false` and post later runs
- **THEN** neither phase writes a job summary

#### Scenario: Partial restore summary
- **WHEN** a split restore succeeds for only one snapshot
- **THEN** the restore summary reports that partial result separately from later post-save results

### Requirement: Detailed report destinations and contents

`cache-report` SHALL default empty, with no report file requested. Produced restore JSON SHALL use the configured path and produced post-save JSON its `.post.json` companion. Disabled/unrun phases or early state/backend failures can produce no report. Reports SHALL retain outcome, key/match, reason/error, phase timing, payload/file, and applicable chunk-reuse information.

#### Scenario: Main and post reports
- **WHEN** a cache report path is configured and both phases reach report writing
- **THEN** restore and save results are written to distinct destinations

#### Scenario: Compatible match detail
- **WHEN** a compatible-dependency snapshot is restored
- **THEN** its report distinguishes match type from successful restoration

#### Scenario: Post cannot initialize
- **WHEN** post cannot load valid saved state or initialize its cache backend
- **THEN** a post-report file is not guaranteed even though a path was configured

### Requirement: Installer report contents

Installer reports SHALL describe the selected installation, source, phase timings, transferred bytes, and peak temporary logical payload. Their destination and standalone transport are owned by setup-execution.

#### Scenario: Reused toolchain report
- **WHEN** setup produces an installer report for a reused local installation
- **THEN** the report identifies the selected toolchain and reuse source without implying a fresh download

### Requirement: Diagnostic metric boundaries

Reported phase times SHALL represent elapsed work, with installer body-read time assigned to download and cache preparation excluded from restore total. Overlapped work SHALL NOT be presented as additive setup duration. Transferred payload, regular-file payload, chunk reuse, and peak temporary logical payload SHALL retain their distinct meanings rather than claim total network, service-storage, disk-allocation, or process-memory cost.

#### Scenario: Installation overlaps prefetch
- **WHEN** toolchain work and cache prefetch run concurrently
- **THEN** their phase totals do not establish complete setup elapsed time by addition

#### Scenario: Chunk reuse
- **WHEN** a save reuses existing encoded chunks
- **THEN** reuse bytes describe avoided encoded uploads rather than total remote-storage savings

### Requirement: Progress and presentation controls

GitHub Actions diagnostics SHALL group stages and report actual advancing byte/file progress with throttled updates. Unknown totals SHALL omit percentages; stalled operations SHALL NOT fabricate activity, and failures SHALL NOT claim successful completion. Standalone diagnostics SHALL remain plain without workflow commands or periodic progress. Nonempty `NO_COLOR` SHALL disable action status colors.

#### Scenario: Unknown total
- **WHEN** chunked transfer or publication has no known total
- **THEN** progress reports actual completed work without an invented percentage

#### Scenario: Failed slow operation
- **WHEN** an operation that emitted progress fails or is cancelled
- **THEN** its closing progress indicates it stopped rather than claiming success

### Requirement: Safe human presentation and explicit machine-data limits

Logs, annotations, debug text, and summaries SHALL sanitize untrusted controls and redact current runtime/GitHub token values and HTTP(S) URLs. This SHALL NOT promise masking arbitrary or transformed secrets. Machine JSON and action outputs SHALL remain unredacted data that can contain full paths, keys, and error text; presentation sanitization SHALL NOT imply those artifacts are safe to publish.

#### Scenario: Untrusted diagnostic text
- **WHEN** an error or matched key includes workflow-command controls, a known token, or a URL
- **THEN** human presentation escapes or removes control effects and redacts the recognized sensitive text

#### Scenario: Detailed JSON file
- **WHEN** a cache report is written
- **THEN** it retains full key details rather than inheriting summary omissions

### Requirement: Debug activation and backend error privacy

Native debug diagnostics SHALL use `RUNNER_DEBUG=1` and include detailed reports only when enabled. Cache-backend transport errors entering report fields SHALL use fixed operation/category text without raw endpoint, hostname, certificate, proxy, or cause details. This transport classification SHALL NOT imply sanitization of unrelated report fields or filesystem errors.

#### Scenario: Backend transport error report
- **WHEN** a transport failure contains private request details
- **THEN** the cache report retains a fixed category instead of those raw details

#### Scenario: Debug disabled
- **WHEN** native runner debugging is not enabled
- **THEN** ordinary logs do not emit full debug JSON reports
