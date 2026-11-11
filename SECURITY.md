# Security policy

## Reporting a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/nicoheidtke/setup-go-better/security/advisories/new) when the repository offers **Report a vulnerability**. Describe the affected action distribution SHA, platform, impact and minimal reproduction. Keep exploit details, credentials and private source out of ordinary issues and pull requests.

This policy does not enable GitHub's reporting feature. If the private form is unavailable, open an issue asking only for a private security contact, without describing the vulnerability or attaching logs. Maintainers must enable private vulnerability reporting when the repository is eligible, and verify the form before public adoption. No security email address or response deadline is promised. See [GitHub's private reporting instructions](https://docs.github.com/en/code-security/how-tos/report-and-fix-vulnerabilities/report-privately).

## Supported versions

The latest published bundled prerelease is the focus for investigation and fixes. There is no stable or long-term support line; older previews do not receive a promised backport. The action selects Go 1.26 or newer and targets Linux, macOS and Windows on amd64 and arm64. Platform scope is not proof that every cache mode or lifecycle policy has been validated there; [development evidence](docs/DEVELOPMENT.md) records actual checks and remaining gates.

Support is best effort. Fixes, when available, are delivered in a new distribution revision; consumers pinned to a SHA must update explicitly. The [consumer security model](docs/SECURITY_MODEL.md) explains permissions, cache trust, persistent runners and diagnostic privacy.
