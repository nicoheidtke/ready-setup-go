# Spec Delta

## MODIFIED Requirements

### Requirement: Standalone invocation and option precedence

The executable SHALL support `setup` with explicit flags, rejecting unknown commands, invalid options, and unexpected positional arguments. Explicit setup flags SHALL override corresponding action-input environment values. Node launchers SHALL also support the documented `READY_SETUP_GO_VERSION`, `READY_SETUP_GO_VERSION_FILE`, and `READY_SETUP_GO_LATEST` fallback variables.

#### Scenario: Explicit option overrides action input
- **WHEN** `--go-version` supplies a supported version different from `INPUT_GO-VERSION`
- **THEN** standalone setup uses the explicit flag

#### Scenario: Invalid setup invocation
- **WHEN** setup receives an invalid option or an unexpected positional argument
- **THEN** it fails before installation
