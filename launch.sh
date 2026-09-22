#!/usr/bin/env bash
set -euo pipefail
case "$RUNNER_OS" in Linux) platform=linux ;; macOS) platform=darwin ;; Windows) platform=windows ;; *) echo 'Unsupported RUNNER_OS' >&2; exit 1 ;; esac
case "$RUNNER_ARCH" in X64) arch=amd64 ;; ARM64) arch=arm64 ;; *) echo 'Unsupported RUNNER_ARCH' >&2; exit 1 ;; esac
suffix=''; if [[ "$platform" == windows ]]; then suffix='.exe'; fi
exec "$SETUP_GO_BETTER_ROOT/bin/$platform-$arch/setup-go-better$suffix" setup \
  --go-version "$SETUP_GO_BETTER_VERSION" --check-latest="$SETUP_GO_BETTER_LATEST" "$@"
