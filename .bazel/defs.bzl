"""Custom Bazel macros for avalanchego."""

# The _go_test alias is required because Starlark forbids a load()
# binding and def with the same name in the same file.
load("@io_bazel_rules_go//go:def.bzl", _go_test = "go_test")

def go_test(**kwargs):
    """Repo-owned go_test wrapper.

    All root-module go_test targets are routed through this wrapper via
    `gazelle:map_kind go_test go_test //.bazel:defs.bzl` in the root
    BUILD.bazel.

    Keep this as a thin pass-through so the repo retains a single Go-test
    hook for Gazelle and any future Go-specific policy.
    """
    _go_test(**kwargs)

def graft_go_test(**kwargs):
    """go_test wrapper with long (900s) timeout for graft modules.

    Graft modules (coreth, evm, subnet-evm) have longer test timeouts
    than the root module (120s). Bazel's "long" timeout (900s) covers
    all graft modules' requirements (up to 900s).
    """
    kwargs.setdefault("timeout", "long")  # 900s
    go_test(**kwargs)
