"""Module extension to validate Go version matches expected version from nix/go/default.nix."""

# Expected Go version - must match nix/go/default.nix goVersion
# Update this when updating the Go version in nix/go/default.nix
EXPECTED_GO_VERSION = "1.24.11"

def _go_version_check_impl(repository_ctx):
    """Repository rule that validates Go version at analysis time."""

    # Get the Go version from the host
    result = repository_ctx.execute(["go", "version"])
    if result.return_code != 0:
        fail("Failed to run 'go version'. Are you in a nix shell? Run: nix develop")

    # Parse version from output like "go version go1.24.11 linux/amd64"
    output = result.stdout.strip()
    parts = output.split(" ")
    if len(parts) < 3:
        fail("Unexpected 'go version' output: {}".format(output))

    # Extract version number (remove "go" prefix)
    go_version_str = parts[2]
    if go_version_str.startswith("go"):
        go_version_str = go_version_str[2:]

    # Check if version matches
    if go_version_str != EXPECTED_GO_VERSION:
        fail("""
================================================================================
GO VERSION MISMATCH
================================================================================
Expected: go{expected}
Found:    go{found}

This usually means:
1. You're not running inside 'nix develop', OR
2. Your nix shell is stale (reload with 'direnv reload' or exit and re-enter)

To fix:
  nix develop   # Enter fresh nix shell
  # or
  direnv reload # If using direnv

Expected version is defined in: nix/go/default.nix
================================================================================
""".format(expected = EXPECTED_GO_VERSION, found = go_version_str))

    # Create a marker file to indicate check passed
    repository_ctx.file("BUILD.bazel", """
# Go version check passed: go{version}
# This repository exists only to validate the Go version at analysis time.
""".format(version = go_version_str))

_go_version_check_repo = repository_rule(
    implementation = _go_version_check_impl,
    local = True,  # Re-run on every build to catch stale shells
    environ = ["PATH"],  # Depend on PATH to detect shell changes
)

def _go_version_check_extension_impl(module_ctx):
    _go_version_check_repo(name = "go_version_check")

go_version_check = module_extension(
    implementation = _go_version_check_extension_impl,
)
