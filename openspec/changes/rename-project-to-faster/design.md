# Design

## Context

The repository has Go runtime and contributor tools, dependency-free Node/Bash bridges, six native targets, and a maintained behavioral baseline.

## Goals / Non-Goals

**Goals:** apply one project identity across source and runtime; keep main/post, packaging/bootstrap, and tests coherent.

**Non-Goals:** change setup inputs, archive safety, concurrency, or performance policy; execute hosted workflows or publication.

## Decisions

Use setup-go-faster for lowercase names, Setup Go Faster for display names, and SETUP_GO_FASTER for environment variables. Apply the identity consistently to content-fingerprint domain strings and private staging paths. Update archived planning text as explicitly requested. Retain existing validation procedures and test native bundled/lightweight execution locally.

## Risks / Trade-offs

Executable names, environment variables, private state paths, and content identities form a coordinated naming boundary. Rebuild distributions from the same source and verify launcher/native agreement. Local native checks establish only the platforms actually executed.
