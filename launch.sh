#!/usr/bin/env bash
# The installer-only comparison path deliberately requires a bundled binary.
if [ -f "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/NATIVE_DOWNLOADS.json" ]; then
  echo 'Matching-platform downloads require the Node action; use the root action or a bundled distribution for the Bash composite.' >&2
  exit 1
fi
set -euo pipefail
case "$RUNNER_OS" in Linux) platform=linux ;; macOS) platform=darwin ;; Windows) platform=windows ;; *) echo 'Unsupported RUNNER_OS' >&2; exit 1 ;; esac
case "$RUNNER_ARCH" in X64) arch=amd64 ;; ARM64) arch=arm64 ;; *) echo 'Unsupported RUNNER_ARCH' >&2; exit 1 ;; esac
suffix=''; if [[ "$platform" == windows ]]; then suffix='.exe'; fi
exec "$SETUP_GO_FASTER_ROOT/bin/$platform-$arch/setup-go-faster$suffix" setup \
  --go-version "${SETUP_GO_FASTER_VERSION:-}" --go-version-file "${SETUP_GO_FASTER_VERSION_FILE:-}" --check-latest="$SETUP_GO_FASTER_LATEST" --summary="${SETUP_GO_FASTER_SUMMARY:-true}" "$@"
