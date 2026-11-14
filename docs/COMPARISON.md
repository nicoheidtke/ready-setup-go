# Choosing between setup-go-faster and setup-go

Reviewed 2026-11-10 against official [`actions/setup-go` v7.0.0](https://github.com/actions/setup-go/releases/tag/v7.0.0), commit `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e`, and this repository's implementation. This compares behavior, not measured speed.

Choose the official action for broad compatibility and established defaults. Consider this action when fresh, isolated job snapshots and explicit cache controls justify adopting another action.

| Area | Official `actions/setup-go` | `setup-go-faster` |
| --- | --- | --- |
| Versions/files | Older Go, prereleases, ranges, aliases and wildcards; four version-file formats. [Version inputs](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/README.md#supported-version-syntax). | Go 1.26+ releases, exact/minor selectors; go.mod/go.work. Explicit version wins; concrete preferred toolchain then go directive, preserving exact patches. |
| Installation | Matching tool cache first; go-versions manifest, then official Go fallback. [Installer](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/src/installer.ts). | Matching completed toolchain first; official metadata/downloads with SHA-256 and size verification. Both offer `check-latest`. |
| Environment | Adds Go and GOPATH/bin to PATH; exports GOTOOLCHAIN=local. An already-local setting makes file parsing ignore toolchain. [Setup](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/src/main.ts), [parser](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/src/installer.ts#L598). | Adds Go to PATH, exports GOROOT, preserves GOTOOLCHAIN. Set `GOTOOLCHAIN: local` yourself to prevent later switching. |
| Cache policy | On by default; boolean switch, successful-job saves, operational errors warn. [Lifecycle](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/action.yml). | Off by default; restore-only or restore-save, optional strict errors/save-on-failure; cancelled jobs never save. |
| Freshness | Combined modules/build archive; exact-key hits skip subsequent saves. [Save](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/src/cache-save.ts). | Split by default; fresh build snapshots. Unchanged modules skip packing/upload after full-content checks. Combined is optional. |
| Keys/sharing | OS, architecture, Linux image, exact Go and dependency hash; shared across jobs. Root go.mod default; explicit globs; no prefix fallback. [Keys](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/src/cache-restore.ts). | Workflow/job isolation plus explicit matrix scope. Recursive module/workspace manifests or explicit files/directories, without globs. Compatible-dependency fallback within the namespace. |
| Transfer | Toolkit archive handling; zstd where available, gzip fallback. [Compression](https://github.com/actions/toolkit/blob/main/packages/cache/src/internal/cacheUtils.ts#L93). | Bundled gzip/zstd/none codecs; install/prefetch overlap; configurable transfer workers; optional content-defined chunk reuse. |
| Scope/extras | Architecture override, mirrors, problem matchers and GHES guidance. [Options](https://github.com/actions/setup-go/blob/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/action.yml). | Hosted Linux/macOS/Windows on amd64/arm64; JSON reports and standalone installer. No mirror/architecture override or matchers; see [validation boundaries](DEVELOPMENT.md). |

Both use Node 24 and prebuilt action code; neither builds the installer on consumer runners. This action bundles six native executables by default; optional lightweight distributions fetch only the matching platform executable, verified against pinned metadata.

What you gain and pay for:

- Fresh snapshots can retain recent source, race/coverage and job-specific results. Warm saves also create uploads/entries; isolation duplicates data across jobs and can increase cache eviction pressure.
- Unchanged-module checks still read every file; changed modules pay that plus packing. Chunk reuse can avoid repeated transfers, but still packs the tree and adds requests, buffers and temporary disk work.
- Both consume runner time and GitHub cache capacity. Neither automatically includes separate linter/plugin caches. Cache formats differ, so migration starts cold.

The official action can also use separately configured `actions/cache` steps. Compare acquisition, restore, workload and post-save together; no controlled end-to-end performance advantage has been established. Follow the [measurement methodology](BENCHMARKS.md) for representative comparisons. See [usage](USAGE.md) and [architecture/resource behavior](ARCHITECTURE.md) for details.
