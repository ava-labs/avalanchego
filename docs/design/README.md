# Design documents

This directory holds design-time documents for changes that need explicit
alignment on direction, trade-offs, or approach before or alongside
implementation.

These documents complement repository documentation near the code; they do not
replace it. Use them for deciding and aligning. Use repository documentation
for preserving the reasoning a future reader will need after implementation.

## Documents

- [Bazel multi-architecture AvalancheGo image](./bazel-multiarch-image.md) - plan
  for building the Debian amd64/arm64 image from the Bazel dependency graph
- [Multi-module release](./multi-module-release.md) - design-time context for
  multi-module release work
