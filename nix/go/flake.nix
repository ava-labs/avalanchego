{
  # To use locally: nix run ./nix/go
  # To use remotely: nix run 'github:ava-labs/avalanchego?dir=nix/go'
  # To use remotely at a specific revision: nix run 'github:ava-labs/avalanchego?dir=nix/go&ref=[SHA]'

  description = "Go toolchain for Avalanche projects";

  # Flake inputs
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
  };

  # Flake outputs
  outputs = { self, nixpkgs }:
    let
      # Systems supported
      allSystems = [
        "x86_64-linux" # 64-bit Intel/AMD Linux
        "aarch64-linux" # 64-bit ARM Linux
        "x86_64-darwin" # 64-bit Intel macOS
        "aarch64-darwin" # 64-bit ARM macOS
      ];

      # Helper to provide system-specific attributes
      forAllSystems = f: nixpkgs.lib.genAttrs allSystems (system: f {
        pkgs = import nixpkgs { inherit system; };
      });
    in
    {
      # Export the Go package for other flakes to consume
      packages = forAllSystems ({ pkgs }: {
        default = import ./. { inherit pkgs; };
      });
    };
}
