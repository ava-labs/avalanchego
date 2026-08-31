#!/usr/bin/env bash
#
# Pins for Go tools invoked by version rather than through tools/external/go.mod.
#
# abigen cannot live in tools/external. Adding it there makes gazelle emit a
# second use_repo for com_github_ava_labs_libevm in MODULE.bazel, which the main
# go_deps usage already declares, and `bazelisk mod tidy` then fails. Pin it here
# instead, and let scripts/download_go_dependencies.sh warm the module cache so
# `go run` never has to reach the module proxy in CI.
#
# Usage: source this file.

# Ignore warnings about variables appearing unused; consumers use them.
# shellcheck disable=SC2034
ABIGEN_PKG="github.com/ava-labs/libevm/cmd/abigen@v1.13.14-0.2.0.release"
