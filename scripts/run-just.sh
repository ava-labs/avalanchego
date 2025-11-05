#!/usr/bin/env bash
set -euo pipefail

if command -v just &> /dev/null; then
    exec just "$@"
elif command -v nix &> /dev/null; then
    exec nix run nixpkgs#just -- "$@"
else
    echo "Error: Neither 'just' nor 'nix' is installed." >&2
    echo "" >&2
    echo "Please install one of the following:" >&2
    echo "" >&2
    echo "Option 1 - Install just:" >&2
    echo "  - Visit: https://github.com/casey/just#installation" >&2
    echo "  - Or use cargo: cargo install just" >&2
    echo "" >&2
    echo "Option 2 - Install nix:" >&2
    echo "  - Visit: https://github.com/DeterminateSystems/nix-installer" >&2
    echo "  - Or run: curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install" >&2
    exit 1
fi
