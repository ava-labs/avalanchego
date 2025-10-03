{
  # To use:
  #  - install nix: `./scripts/run_task.sh install-nix`
  #  - run `nix develop` or use direnv (https://direnv.net/)
  #    - for quieter direnv output, set `export DIRENV_LOG_FORMAT=`

  description = "Go implementation of an Avalanche node";

  # Flake inputs
  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2505.*.tar.gz";
    flake-utils.url = "github:numtide/flake-utils";
    gomod2nix = {
      url = "github:maru-ava/gomod2nix?ref=cc78df101c18e6687f4e7e9d41b2d8c8d070cdb0";
      inputs = {
        nixpkgs.follows = "nixpkgs";
        flake-utils.follows = "flake-utils";
      };
    };
  };

  # Flake outputs
  outputs = { self, nixpkgs, flake-utils, gomod2nix }:
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
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ gomod2nix.overlays.default ];
        };
      });
    in
    {
      # Package outputs
      packages = forAllSystems ({ pkgs }:
        let
          go = import ./nix/go.nix { inherit pkgs; };
          rev = self.rev or self.dirtyRev or "dev";
        in
        {
          default = import ./nix/build.nix {
            inherit pkgs go rev;
            buildGoApplication = pkgs.buildGoApplication;
          };

          container = import ./nix/container.nix {
            inherit pkgs rev;
            package = self.packages.${pkgs.system}.default;
          };
        }
      );

      # Development environment output
      devShells = forAllSystems ({ pkgs }: {
        default = import ./nix/shell.nix {
          inherit pkgs;
          mkGoEnv = pkgs.mkGoEnv;
          go = import ./nix/go.nix { inherit pkgs; };
        };
      });
    };
}
