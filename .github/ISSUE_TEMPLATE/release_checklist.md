---
name: Release Checklist
about: Create a ticket to track a release
title: ''
labels: release
assignees: ''

---

**Release**
The release version and a description of the planned changes to be included in the release.

**Issues**
Link the major issues planned to be included in the release.

**Documentation**
Link the relevant documentation PRs for this release.

**Checklist**
- [ ] Update version in scripts/versions.sh and plugin/evm/version.go
- [ ] Bump AvalancheGo dependency for RPCChainVM Compatibility
- [ ] Add new entry in compatibility.json for RPCChainVM Compatibility
- [ ] Update AvalancheGo compatibility in README
- [ ] Bump cmd/simulator go mod (if needed)
- [ ] Confirm goreleaser job has successfully generated binaries by checking the releases page
