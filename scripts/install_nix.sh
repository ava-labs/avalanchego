#!/usr/bin/env bash
set -euo pipefail

# Ensure nix is installed

if command -v nix > /dev/null; then
  echo "Nix is already installed. Exiting."
  exit 0
fi

case "$(uname -s)" in
  Darwin)
    echo "macOS detected"
    echo "Installing using the Lix installer to allow for a clean uninstall"
    curl -sSf -L https://install.lix.systems/lix | sh -s -- install
    ;;
  Linux)
    echo "linux detected"
    echo "Installing using upstream Nix installer"
    sh <(curl -L https://nixos.org/nix/install) --daemon

    echo "Enabling flakes in user config..."
    mkdir -p ~/.config/nix
    if ! grep -q "experimental-features" ~/.config/nix/nix.conf 2>/dev/null; then
      echo "experimental-features = nix-command flakes" >> ~/.config/nix/nix.conf
    fi
    ;;
  *)
    echo "Unsupported OS: $(uname -s)"
    exit 1
    ;;
esac

echo "Nix installation complete."
