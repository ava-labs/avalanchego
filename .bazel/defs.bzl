"""Custom Bazel macros for avalanchego."""

load("@io_bazel_rules_go//go:def.bzl", _go_test = "go_test")

def graft_go_test_900s(**kwargs):
    """go_test wrapper with 900s timeout for graft/coreth and graft/subnet-evm.

    These modules have longer test timeouts than the main repo (120s).
    See graft/coreth/scripts/build_test.sh and graft/subnet-evm/scripts/build_test.sh.
    """
    kwargs.setdefault("timeout", "long")  # 900s
    _go_test(**kwargs)

def graft_go_test_600s(**kwargs):
    """go_test wrapper with 600s timeout for graft/evm.

    This module has a 600s test timeout.
    See graft/evm/scripts/build_test.sh.
    """

    # "moderate" is 300s, "long" is 900s - use long to be safe for 600s requirement
    kwargs.setdefault("timeout", "long")  # 900s (closest to 600s that's >= 600s)
    _go_test(**kwargs)
