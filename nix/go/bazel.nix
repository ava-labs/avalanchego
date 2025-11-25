# Bazel wrapper for Go SDK
# Called by rules_nixpkgs_go to build the Go toolchain
#
# This file re-uses the same Go derivation as nix develop,
# ensuring version consistency between Nix and Bazel environments.
{ pkgs ? import <nixpkgs> {} }:
import ./default.nix { inherit pkgs; }
