# Multi-Module Release Strategy

## Summary

Enable third-party consumers to `go get` avalanchego and its
submodules at tagged versions via the Go module proxy, using
`replace` directives in go.mod files for local/CI builds.

## Problem

The repository contains 4 Go modules with circular dependencies:

| Module | Path |
|--------|------|
| Main | `github.com/ava-labs/avalanchego` |
| Coreth | `github.com/ava-labs/avalanchego/graft/coreth` |
| EVM | `github.com/ava-labs/avalanchego/graft/evm` |
| Subnet-EVM | `github.com/ava-labs/avalanchego/graft/subnet-evm` |

Replace directives handle inter-module resolution for local
development. Since replace directives are ignored when a module is
consumed as a dependency, they don't interfere with external
consumption as long as the `require` directives point to valid,
tagged versions.

## Goals

- External consumers can `go get github.com/ava-labs/avalanchego@v1.15.0`
- All submodules resolve to matching versions automatically
- Local development and CI continue to work

## Non-Goals

- Module consolidation (separate effort)
- CI verification of coordinated go.mod state (to be tackled separately)
- Release automation (to be tackled separately)

## Design

**Resolution by context:**

| Context | Mechanism |
|---------|-----------|
| Local development | replace directives in go.mod |
| CI builds | replace directives in go.mod |
| External consumers | require directives (replace ignored) |

When a module is fetched as a dependency, replace directives are
ignored. Only require directives matter.

### Release Workflow

See [Release Procedure](../../RELEASING_README.md) for the step-by-step process.

Between merge and tag, local builds continue to work because replace
directives redirect inter-module references to local paths. Once
tags are pushed, external consumers can fetch the tagged versions.

For user-facing documentation on consuming these modules, see
[Depending on AvalancheGo Modules](../external_consumption.md).

## Alternatives Considered

**Go workspace (go.work):** Replace directives already provide the
same behavior (ignored by external consumers) without adding a new
file or Go workspace semantics. `go.work` can be added separately to
simplify local development and unify the dependency graph for bazel.

**Release branch with resolved dependencies:** Rejected because it
adds branch management complexity and is unnecessary when replace
directives handle development builds.

## Implementation

### Phase 1: Document the Approach

1. Document the replace directive strategy (this document)
2. Create release scripts for updating require directives and tagging
3. Verify local builds, tests, and CI pass

### Phase 2: First Tagged Release

Follow the [Release Procedure](../../RELEASING_README.md) to create the first tagged release.

## References

- [Go Modules Reference - replace directive](https://go.dev/ref/mod#go-mod-file-replace)
- [golang/go#42627 - replace directives break go install](https://github.com/golang/go/issues/42627)
