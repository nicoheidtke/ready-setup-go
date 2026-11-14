# Spec Delta

## Purpose

Expose native setup through maintained action and standalone entrypoints, integrating selected toolchains with runner environments and propagating process outcomes.

## ADDED Requirements

### Requirement: Root action and Node alias parity

The root action and `experimental/node` alias SHALL expose the same inputs, outputs, defaults, and native main/post lifecycle through Node 24. Consumers SHALL run packaged native executables without compiling the action or installing npm dependencies. Acquiring those executables is owned by `action-acquisition`.

#### Scenario: Alias invocation
- **WHEN** a consumer uses the Node alias with the same inputs as the root action
- **THEN** it receives the same setup and cache lifecycle contract

### Requirement: Installer-only Bash entrypoint

The `experimental/composite` action and `launch.sh` SHALL provide installer-only setup with version, version-file, latest-check, and summary controls. The composite SHALL expose selected version/root outputs and disable the cache lifecycle. Bash acquisition SHALL require bundled executables and reject lightweight distributions explicitly.

#### Scenario: Composite setup
- **WHEN** the bundled composite action sets up Go
- **THEN** it exposes `go-version` and `goroot` without registering automatic cache restore/save

#### Scenario: Lightweight Bash use
- **WHEN** the Bash comparison path is invoked from a lightweight distribution
- **THEN** it fails with the bundled-distribution requirement

### Requirement: Standalone invocation and option precedence

The executable SHALL support `setup` with explicit flags, rejecting unknown commands, invalid options, and unexpected positional arguments. Explicit setup flags SHALL override corresponding action-input environment values. Node launchers SHALL also support the documented `SETUP_GO_FASTER_VERSION`, `SETUP_GO_FASTER_VERSION_FILE`, and `SETUP_GO_FASTER_LATEST` fallback variables.

#### Scenario: Explicit option overrides action input
- **WHEN** `--go-version` supplies a supported version different from `INPUT_GO-VERSION`
- **THEN** standalone setup uses the explicit flag

#### Scenario: Invalid setup invocation
- **WHEN** setup receives an invalid option or an unexpected positional argument
- **THEN** it fails before installation

### Requirement: Export the selected toolchain to the runner

After successful installation, setup SHALL expose exact `go-version` without a `go` prefix and absolute `goroot` through runner output files, set `GOROOT`, and add the toolchain's `bin` to subsequent steps' `PATH`. It SHALL preserve `GOTOOLCHAIN`; later Go commands remain subject to Go's own switching policy. Enabled cache outputs SHALL use the same runner protocol, with meanings owned by `diagnostics`.

#### Scenario: Runner environment integration
- **WHEN** setup succeeds with writable runner output/environment files
- **THEN** subsequent steps receive the selected toolchain root and executable path and the existing `GOTOOLCHAIN` value remains unchanged

#### Scenario: Unsafe environment field or write failure
- **WHEN** an exported field contains a newline/NUL or a configured runner output file cannot be written
- **THEN** setup fails rather than report successful export

### Requirement: Standalone environment and result transport

Outside GitHub Actions, setup SHALL resolve relative version files from the current directory, use the operating-system temporary directory when `RUNNER_TEMP` is unset, and leave the parent shell's environment unchanged. It SHALL support `--json FILE` and `--json -` for installer results while sending diagnostics to stderr. Automatic cache lifecycle SHALL require the GitHub action runtime.

#### Scenario: Standalone JSON output
- **WHEN** standalone setup succeeds with `--json -`
- **THEN** stdout contains the installer result and diagnostics remain on stderr

#### Scenario: Standalone shell remains unchanged
- **WHEN** setup completes outside a runner without environment-file destinations
- **THEN** it reports its selected installation without modifying the invoking shell's environment

### Requirement: Main and post process outcomes

The Node bridge SHALL invoke the native executable without shell interpolation, propagate native exit failures, and fail on startup errors or signal termination. Post with no saved cache state SHALL exit without starting native work; native `post` SHALL reject arguments. Post-save eligibility is owned by `cache-policy`.

#### Scenario: Version file contains spaces
- **WHEN** a Node action input contains a version-file path with spaces
- **THEN** that path reaches native setup as one argument

#### Scenario: Missing post state
- **WHEN** the Node post hook has no saved cache state
- **THEN** it succeeds without launching a native process

#### Scenario: Child fails
- **WHEN** native setup exits unsuccessfully or cannot start
- **THEN** the action fails

### Requirement: Cancellation propagation and cleanup limits

Setup and post SHALL use bounded cancellable operation contexts. The Node bridge SHALL forward interrupt/termination signals, wait for the child to close, and report cancellation as failure even if the child returns success. Cooperative native cancellation SHALL propagate to active work. Cleanup is best effort under abrupt termination; Windows forceful termination SHALL NOT imply graceful POSIX cleanup.

#### Scenario: Cooperative launcher cancellation
- **WHEN** the Node launcher receives an interrupt while native work is active
- **THEN** it requests child termination, waits for child closure, and returns failure

#### Scenario: Windows forceful termination
- **WHEN** Windows forcibly terminates the native process
- **THEN** deferred cleanup is not guaranteed to run
