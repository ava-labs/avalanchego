{
  # To use:
  #  - install nix: https://github.com/DeterminateSystems/nix-installer?tab=readme-ov-file#install-nix
  #  - run `nix develop` or use direnv (https://direnv.net/)
  #    - for quieter direnv output, set `export DIRENV_LOG_FORMAT=`

  description = "Go implementation of an Avalanche node";

  # Flake inputs
  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2405.*.tar.gz";
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
    (flake-utils.lib.eachDefaultSystem
      (system:
        let
          # Helper functions to derive Go arch from Nix arch
          nixArchToGoArch = arch: {
            "x86_64" = "amd64";
            "aarch64" = "arm64";
          }.${arch} or arch;

          # Split system into arch and os
          parseSystem = system:
            let
              parts = builtins.split "-" system;
              arch = builtins.elemAt parts 0;
              os = builtins.elemAt parts 2;
            in {
              goarch = nixArchToGoArch arch;
              goos = os;
              # Construct URL path in Go's format
              goURLPath = "${os}-${nixArchToGoArch arch}";
            };

          # Platform-specific SHA256 hashes for Go 1.23.6
          goSHA256s = {
            "linux-amd64" = "9379441ea310de000f33a4dc767bd966e72ab2826270e038e78b2c53c2e7802d";
            "linux-arm64" = "561c780e8f4a8955d32bf72e46af0b5ee5e0debe1e4633df9a03781878219202";
            "darwin-amd64" = "782da50ce8ec5e98fac2cd3cdc6a1d7130d093294fc310038f651444232a3fb0";
            "darwin-arm64" = "5cae2450a1708aeb0333237a155640d5562abaf195defebc4306054565536221";
          };

          pkgs = nixpkgs.legacyPackages.${system}.extend (final: prev:
            let
              # Convert the nix system to the golang system
              # TODO(marun) Rename to reflect golang-specific nature
              targetSystem = parseSystem system;
            in {
              go_1_23_6 = final.stdenv.mkDerivation rec {
                name = "go-1.23.6";
                version = "1.23.6";

                inherit (targetSystem) goos goarch;
                GOOS = targetSystem.goos;
                GOARCH = targetSystem.goarch;

                src = final.fetchurl {
                  url = "https://go.dev/dl/go${version}.${targetSystem.goURLPath}.tar.gz";
                  sha256 = goSHA256s.${targetSystem.goURLPath} or (throw "Unsupported system: ${system}");
                };

                installPhase = ''
                  mkdir -p $out
                  cp -r ./* $out/
                  chmod +x $out/bin/go
                '';
              };
            }
          );

          # The current default sdk for macOS fails to compile go projects, so we use a newer one for now.
          # This has no effect on other platforms.
          callPackage = pkgs.darwin.apple_sdk_11_0.callPackage or pkgs.callPackage;
        in
        rec {
          packages.default = callPackage ./. {
            inherit (gomod2nix.legacyPackages.${system}) buildGoApplication;
            go = pkgs.go_1_23_6;
            rev = self.rev or "dev";
          };

          packages.container = callPackage ./container.nix {
            package = packages.default;
            rev = self.rev or "dev";
          };

          packages.kind-with-registry = pkgs.stdenv.mkDerivation {
            pname = "kind-with-registry";
            version = "1.0.0";

            src = pkgs.fetchurl {
              url = "https://raw.githubusercontent.com/kubernetes-sigs/kind/7cb9e6be25b48a0e248097eef29d496ab1a044d0/site/static/examples/kind-with-registry.sh";
              sha256 = "0gri0x0ygcwmz8l4h6zzsvydw8rsh7qa8p5218d4hncm363i81hv";
            };

            phases = [ "installPhase" ];

            installPhase = ''
              mkdir -p $out/bin
              install -m755 $src $out/bin/kind-with-registry.sh
            '';

            meta = with pkgs.lib; {
              description = "Script to set up kind with a local registry";
              license = licenses.mit;
              maintainers = with maintainers; [ "maru-ava" ];
            };
          };

          devShells.default = callPackage ./shell.nix {
            inherit (gomod2nix.legacyPackages.${system}) mkGoEnv;
            go = pkgs.go_1_23_6;
            kind-with-registry = (self.packages.${system}.kind-with-registry);
          };
        })
    );
}
