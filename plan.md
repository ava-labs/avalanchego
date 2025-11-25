# Bazel Integration Plan for avalanchego Monorepo

## Executive Summary
This plan introduces Bazel to the avalanchego monorepo using a phased approach, maintaining the existing Task infrastructure while gradually migrating implementation to Bazel. The repository contains two Go modules that must remain separate for licensing reasons (avalanchego is BSD-3, coreth is LGPL-3), but have circular dependencies requiring careful configuration.

## Repository Context

### Module Structure
- **Main module**: `github.com/ava-labs/avalanchego` (BSD-3 license)
  - Location: Repository root
  - go.mod contains: `replace github.com/ava-labs/avalanchego/graft/coreth => ./graft/coreth`
- **Coreth module**: `github.com/ava-labs/avalanchego/graft/coreth` (LGPL-3 license)
  - Location: `graft/coreth/`
  - go.mod contains: `replace github.com/ava-labs/avalanchego => ../../`
  - Circular dependency with main module
- **Tools module**: `tools/go.mod` for build tools

### Key Binaries to Build
1. **avalanchego**: Main node binary (`/cmd/avalanchego/main.go`)
2. **coreth**: EVM plugin (`/graft/coreth/plugin/main.go`)
3. **xsvm**: Example VM plugin (`/vms/example/xsvm/cmd/xsvm/main.go`)
4. **tmpnetctl**: Test network controller (`/tests/fixture/tmpnet/cmd/tmpnetctl/main.go`)
5. **bootstrap-monitor**: Bootstrap monitoring (`/tests/fixture/bootstrapmonitor/cmd/main.go`)

### Build Requirements
- Go version: 1.24.9 (must match across all modules)
- CGO flags: `-O2 -D__BLST_PORTABLE__` for cryptography
- LDFlags: Inject git commit via `-X github.com/ava-labs/avalanchego/version.GitCommit`
- Static linking: Optional via musl-gcc when `STATIC_COMPILATION=1`

### Code Generation Requirements
1. **Protobuf**: Two buf modules (`proto/` and `connectproto/`) using buf v1.52.1
2. **Mocks**: Using mockgen with `go tool mockgen`
3. **Canoto**: Serialization for consensus (`go tool canoto`)
4. **Coreth-specific**:
   - gencodec: JSON/RLP encoding
   - rlpgen: RLP serialization
5. **Contract bindings**: Solidity contracts with abigen

### Integration Approach
- **Nix Integration**: Start with shell wrapper, eventually migrate to rules_nixpkgs
- **Module Strategy**: Single Bazel workspace with local_repository for coreth
- **Coexistence**: Tasks remain as entrypoints, Bazel handles dependency graph
- **CI/CD**: Gradual migration, maintain green builds throughout

## Phase 1: Foundation & Basic Build

### Task 1.1: Add Bazel to Nix Environment
**Subagent Type**: General-purpose
**Description**: Update flake.nix to provide Bazel and related tools

**Steps**:
1. Edit `flake.nix` to add bazel, buildifier, buildozer packages
2. Update shell hook if needed for Bazel cache configuration
3. Test with `nix develop -c bazel version`

**Validation**:
- `nix develop -c bazel version` returns version 6.x or 7.x
- `nix develop -c buildifier --version` works
- No conflicts with existing tools

### Task 1.2: Initialize Bazel Workspace
**Subagent Type**: General-purpose
**Description**: Create root Bazel configuration files

**Steps**:
1. Create `MODULE.bazel` (preferred) or `WORKSPACE.bazel` at repository root
2. Configure with:
   ```python
   bazel_dep(name = "rules_go", version = "0.50.1")
   bazel_dep(name = "gazelle", version = "0.39.1")

   go_sdk = use_extension("@rules_go//go:extensions.bzl", "go_sdk")
   go_sdk.download(version = "1.24.9")
   ```
3. Create `.bazelrc` with common settings:
   ```
   build --incompatible_enable_cc_toolchain_resolution
   test --test_output=errors
   ```
4. Add `.bazelignore` with:
   ```
   .git
   node_modules
   .avalanchego
   ```

**Validation**:
- `bazel query //...` returns empty result without errors
- Bazel recognizes workspace root

### Task 1.3: Configure Gazelle for Multi-Module
**Subagent Type**: General-purpose
**Description**: Set up Gazelle with proper module mapping

**Steps**:
1. Create root `BUILD.bazel`:
   ```python
   load("@gazelle//:def.bzl", "gazelle")

   # gazelle:prefix github.com/ava-labs/avalanchego
   # gazelle:go_generate_proto false
   gazelle(name = "gazelle")
   ```
2. Create `graft/coreth/BUILD.bazel`:
   ```python
   # gazelle:prefix github.com/ava-labs/avalanchego/graft/coreth
   ```
3. Configure local_repository in MODULE.bazel:
   ```python
   local_path_override(
       module_name = "coreth",
       path = "graft/coreth",
   )
   ```
4. Set up go.mod replace directive handling

**Validation**:
- `bazel run //:gazelle` executes without errors
- No import resolution errors

### Task 1.4: Generate Initial BUILD Files
**Subagent Type**: General-purpose
**Description**: Run Gazelle to create BUILD files

**Steps**:
1. Run `bazel run //:gazelle` from repository root
2. Run `cd graft/coreth && bazel run //:gazelle`
3. Review generated BUILD files for obvious issues
4. Commit generated files to track changes

**Validation**:
- BUILD.bazel files created throughout repository
- `bazel query //...` shows all targets
- No duplicate target names

### Task 1.5: Fix Basic Build Issues
**Subagent Type**: General-purpose
**Description**: Resolve initial compilation errors

**Common Issues to Address**:
1. Missing CGO flags for BLST:
   ```python
   go_library(
       cgo_flags = ["-O2", "-D__BLST_PORTABLE__"],
   )
   ```
2. Circular dependencies between modules
3. Missing test data dependencies
4. Incorrect import paths

**Validation**:
- `bazel build //...` attempts compilation (failures expected)
- Main packages start compiling

## Phase 2: Primary Binaries

### Task 2.1: Build avalanchego Binary
**Subagent Type**: General-purpose
**Description**: Configure main binary build with version injection

**Steps**:
1. Update `cmd/avalanchego/BUILD.bazel`:
   ```python
   go_binary(
       name = "avalanchego",
       embed = [":avalanchego_lib"],
       x_defs = {
           "github.com/ava-labs/avalanchego/version.GitCommit": "{STABLE_GIT_COMMIT}",
       },
   )
   ```
2. Add genrule for git commit extraction
3. Configure static linking option
4. Test binary execution

**Validation**:
- `bazel build //cmd/avalanchego` produces binary
- `bazel-bin/cmd/avalanchego/avalanchego_/avalanchego --version` shows correct version
- Binary size comparable to Task-built version

### Task 2.2: Build coreth Plugin
**Subagent Type**: General-purpose
**Description**: Build coreth as a plugin library

**Steps**:
1. Update `graft/coreth/plugin/BUILD.bazel`:
   ```python
   go_binary(
       name = "coreth",
       embed = [":plugin_lib"],
       linkmode = "plugin",
   )
   ```
2. Configure plugin ID injection
3. Set up installation rules for `~/.avalanchego/plugins`

**Validation**:
- Plugin file created with correct naming
- Plugin loads successfully in avalanchego
- No unresolved symbols

### Task 2.3: Create Task Wrappers
**Subagent Type**: General-purpose
**Description**: Update Taskfile.yml to use Bazel

**Steps**:
1. Add new tasks to `Taskfile.yml`:
   ```yaml
   build-bazel:
     desc: Builds avalanchego using Bazel
     cmd: nix develop -c bazel build //cmd/avalanchego

   build-bazel-all:
     desc: Builds all targets using Bazel
     cmd: nix develop -c bazel build //...
   ```
2. Add comparison task for validation
3. Document Bazel usage in task descriptions

**Validation**:
- `task build-bazel` successfully builds
- Output location documented
- No regression in existing tasks

## Phase 3: Code Generation

### Task 3.1: Protobuf Generation
**Subagent Type**: General-purpose
**Description**: Configure protobuf generation with buf

**Steps**:
1. Create proto_library rules for `proto/` and `connectproto/`
2. Configure buf integration:
   ```python
   buf_lint_test(
       name = "proto_lint",
       config = "buf.yaml",
       targets = [":proto"],
   )
   ```
3. Set up go_proto_library generation
4. Ensure generated code matches current output

**Validation**:
- `bazel build //proto/...` generates proto code
- Generated files match git-committed versions
- No import path issues

### Task 3.2: Mock Generation
**Subagent Type**: General-purpose
**Description**: Configure mockgen rules

**Steps**:
1. Add gomock rules where needed:
   ```python
   gomock(
       name = "mock_manager",
       source = "manager.go",
       package = "mock",
   )
   ```
2. Handle cross-module mock generation
3. Ensure proper import paths

**Validation**:
- Generated mocks compile successfully
- Mock files match current versions
- Tests using mocks pass

### Task 3.3: Gazelle Automation
**Subagent Type**: General-purpose
**Description**: Add automation tasks and CI integration

**Steps**:
1. Add to `Taskfile.yml`:
   ```yaml
   update-gazelle:
     desc: Updates Bazel BUILD files using Gazelle
     cmd: nix develop -c bazel run //:gazelle

   check-gazelle:
     desc: Checks that Bazel BUILD files are up-to-date
     cmds:
       - task: update-gazelle
       - task: check-clean-branch
   ```
2. Add to `.github/workflows/ci.yml`:
   ```yaml
   check_gazelle:
     name: Up-to-date BUILD files
     runs-on: ubuntu-latest
     steps:
       - uses: actions/checkout@v4
       - uses: ./.github/actions/install-nix
       - shell: nix develop --command bash -x {0}
         run: ./scripts/run_task.sh check-gazelle
   ```

**Validation**:
- CI job catches outdated BUILD files
- Task updates all BUILD files correctly
- No manual BUILD file editing needed

## Phase 4: Testing Infrastructure

### Task 4.1: Unit Tests
**Subagent Type**: General-purpose
**Description**: Configure test execution via Bazel

**Steps**:
1. Configure test exclusions in BUILD files:
   ```python
   # gazelle:exclude **/*_test.go
   ```
   Where needed for e2e/integration tests
2. Set up race detection:
   ```
   bazel test --features=race //...
   ```
3. Configure test timeouts and sizes

**Validation**:
- `bazel test //...` runs unit tests
- Test results match Task-based testing
- Coverage reporting works

### Task 4.2: Integration with Tasks
**Subagent Type**: General-purpose
**Description**: Update test tasks to use Bazel

**Steps**:
1. Update `Taskfile.yml`:
   ```yaml
   test-unit-bazel:
     desc: Runs unit tests using Bazel
     cmd: nix develop -c bazel test //...
   ```
2. Add test caching configuration
3. Preserve test output format compatibility

**Validation**:
- Test caching reduces execution time
- Parallel test execution works
- CI remains green

## Phase 5: Advanced Features (Future Work)

### Task 5.1: E2E Test Support
- Configure Ginkgo integration
- Handle test data and fixtures
- Set up tmpnet coordination

### Task 5.2: Docker Image Building
- Add rules_oci for container builds
- Multi-platform support
- Image signing and pushing

### Task 5.3: CI/CD Integration
- Remote cache configuration
- Build event streaming
- Dependency graph visualization

## Phase 6: Advanced Integration (Long-term)

### Task 6.1: rules_nixpkgs Integration
- Remove shell wrapper dependency
- Hermetic tool provisioning
- Reproducible builds across environments

### Task 6.2: Full Code Generation
- Canoto generation rules
- Contract binding generation
- Coreth-specific generators (gencodec, rlpgen)

## Execution Notes

### Parallel Execution Opportunities
- Phase 1: Tasks 1.1 and 1.2 can run in parallel
- Phase 2: Tasks 2.1 and 2.2 can run in parallel after Phase 1
- Phase 3: All tasks can run in parallel after Phase 2
- Phase 4: Can start immediately after Phase 2

### Critical Success Factors
1. **Never break existing builds** - Tasks must continue working
2. **Validate at each step** - Compare Bazel vs Task outputs
3. **Maintain CI green** - Add new checks without breaking old ones
4. **Document differences** - Any behavioral changes must be noted
5. **Enable rollback** - Keep ability to revert to Task-only builds

### Common Pitfalls to Avoid
1. **Module boundaries** - Respect the licensing separation
2. **Import cycles** - Handle circular dependencies carefully
3. **CGO configuration** - Ensure BLST flags are properly set
4. **Plugin architecture** - Maintain plugin ID and loading mechanism
5. **Test data** - Ensure test fixtures are properly included

### Validation Checklist for Each Phase
- [ ] Existing Task commands still work
- [ ] Bazel builds produce working binaries
- [ ] Binary sizes are comparable (±10%)
- [ ] Test coverage remains the same
- [ ] CI/CD pipeline remains green
- [ ] No performance regressions
- [ ] Generated code matches exactly
- [ ] Documentation updated

## Resources and References

### Bazel Documentation
- [rules_go](https://github.com/bazelbuild/rules_go)
- [Gazelle](https://github.com/bazelbuild/bazel-gazelle)
- [rules_nixpkgs](https://github.com/tweag/rules_nixpkgs) (for Phase 6)

### Repository-Specific
- Main module: `/go.mod`
- Coreth module: `/graft/coreth/go.mod`
- Task definitions: `/Taskfile.yml` and `/graft/coreth/Taskfile.yml`
- CI workflows: `/.github/workflows/ci.yml` and `/.github/workflows/coreth-ci.yml`
- Build scripts: `/scripts/build.sh` and related

### Testing Commands for Validation
```bash
# After Phase 1
nix develop -c bazel version
nix develop -c bazel query //...

# After Phase 2
nix develop -c bazel build //cmd/avalanchego
./bazel-bin/cmd/avalanchego/avalanchego_/avalanchego --version

# After Phase 3
nix develop -c bazel run //:gazelle
nix develop -c bazel build //proto/...

# After Phase 4
nix develop -c bazel test //...
nix develop -c bazel test --features=race //...
```

This plan provides a complete roadmap for introducing Bazel to the avalanchego monorepo while maintaining stability and compatibility throughout the migration process.