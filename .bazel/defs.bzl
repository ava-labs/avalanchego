"""Custom Bazel macros for avalanchego."""

load("@io_bazel_rules_go//go:def.bzl", _go_test = "go_test")

def go_test(**kwargs):
    """go_test wrapper that enables shuffle by default.

    Shuffle is injected via args rather than .bazelrc --test_arg so that
    it only applies to Go test binaries (not gazelle_test, etc.).
    Override with: bazel test --test_arg=-test.shuffle=off //...
    """
    args = list(kwargs.pop("args", []))
    args.append("-test.shuffle=on")
    kwargs["args"] = args
    _go_test(**kwargs)

def graft_go_test(**kwargs):
    """go_test wrapper with long (900s) timeout for graft modules.

    Graft modules (coreth, evm, subnet-evm) have longer test timeouts
    than the root module (120s). Bazel's "long" timeout (900s) covers
    all graft modules' requirements (up to 900s).
    """
    kwargs.setdefault("timeout", "long")  # 900s
    go_test(**kwargs)
