# Documentation index

Cross-cutting repository documentation lives here. Package- and
feature-specific documentation should usually live next to the code it
describes.

Start here when you are looking for repository-wide guidance or for documents
that span multiple areas of the tree.

## Guides

- [Documentation guidelines](./documentation-guidelines.md) - how to decide
  what belongs in repository documentation, where it should live, and how it
  should be maintained
- [Tasks](./tasks.md) - why this repo uses Task and how tasks should be written
- [CI](./ci.md) - cross-cutting CI conventions for workflows and actions
- [CI disk space](./ci-disk-space.md) - shared CI runner disk-space policy and diagnostics
- [Container image builds](./image-builds.md) - how repository tasks build
  container images and provide Go dependencies without module proxy access
- [Bazel](./bazel.md) - Bazel-related repository guidance
- [External consumption](./external_consumption.md) - guidance for externally
  consumed repository outputs

## Design-time documents

Documents under `design/` capture design-time or broader cross-cutting context
when a change needs alignment before or alongside implementation.

- [Design documents](./design/README.md) - index of design-time documents
