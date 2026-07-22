{
  # To test with arbitrary firewood versions (alternative to firewood-go-ethhash):
  #  - Install nix: https://nixos.org/download/
  #  - Clone firewood locally at desired version/commit
  #  - Build: `cd ffi && nix build`
  #  - In your Go project: `go mod edit -replace github.com/ava-labs/firewood-go-ethhash/ffi=/path/to/firewood/ffi/result/ffi`

  description = "Firewood FFI library and development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
    rust-overlay.url = "github:oxalica/rust-overlay?ref=40e6ccc06e1245a4837cbbd6bdda64e21cc67379";
    crane.url = "github:ipetkov/crane";
    flake-utils.url = "github:numtide/flake-utils";
    golang.url = "github:ava-labs/avalanchego?dir=nix/go&ref=89ac856f87556f8dd38ed93530d10eb92210da35";
  };

  outputs = { self, nixpkgs, rust-overlay, crane, flake-utils, golang }:
    flake-utils.lib.eachDefaultSystem (system:
    let
      overlays = [ (import rust-overlay) ];
      pkgs = import nixpkgs { inherit system overlays; };
      inherit (pkgs) lib;

      go = golang.packages.${system}.default;

      rustToolchain = pkgs.rust-bin.stable.latest.default.override {
        extensions = [ "rust-src" "rustfmt" "clippy" ];
      };

      craneLib = (crane.mkLib pkgs).overrideToolchain rustToolchain;

      # Extract crate info from Cargo.toml files
      ffiCargoToml = builtins.fromTOML (builtins.readFile ./Cargo.toml);

      src = lib.cleanSourceWith {
        src = craneLib.path ./..;
        filter = path: type:
          (lib.hasSuffix ".md" path) ||
          (lib.hasSuffix ".go" path) ||
          (lib.hasSuffix "go.mod" path) ||
          (lib.hasSuffix "go.sum" path) ||
          (lib.hasSuffix "firewood.h" path) ||
          (craneLib.filterCargoSources path type);
      };

      commonArgs = {
        inherit src;
        strictDeps = true;
        dontStrip = true;

        # Build only the firewood-ffi crate
        pname = ffiCargoToml.package.name;
        version = ffiCargoToml.package.version;

        nativeBuildInputs = with pkgs; [
          pkg-config
        ];

      } // lib.optionalAttrs pkgs.stdenv.isDarwin {
        # Set macOS deployment target for Darwin builds
        MACOSX_DEPLOYMENT_TARGET = "15.0";
      };

      cargoArtifacts = craneLib.buildDepsOnly (commonArgs // {
        # Use cargo alias defined in .cargo/config.toml
        cargoBuildCommand = "cargo build-static-ffi";
      });

      firewood-ffi = craneLib.buildPackage (commonArgs // {
        inherit cargoArtifacts;
        # Use cargo alias defined in .cargo/config.toml
        cargoBuildCommand = "cargo build-static-ffi";

        # Disable tests - we only need to build the static library
        doCheck = false;

        # Install the static library and header
        postInstall = ''
          # Create a package structure compatible with FIREWOOD_LD_MODE=STATIC_LIBS
          mkdir -p $out/ffi
          cp -R ./ffi/* $out/ffi/
          mkdir -p $out/ffi/libs/${pkgs.stdenv.hostPlatform.config}
          cp target/maxperf/libfirewood_ffi.a $out/ffi/libs/${pkgs.stdenv.hostPlatform.config}/

          # Switch CGO LDFLAGS to STATIC_LIBS mode. Uses a file path (not package pattern) so
          # Go only resolves this file's stdlib imports, avoiding the module-level
          # golang.org/x/tools dependency that can't be downloaded in the nix sandbox.
          cd $out/ffi
          HOME=$TMPDIR GOFILE=firewood.go FIREWOOD_LD_MODE=STATIC_LIBS ${go}/bin/go run ./gen/update-cgo-ldflags/main.go
        '';

        meta = with lib; {
          description = "C FFI bindings for Firewood, an embedded key-value store";
          homepage = "https://github.com/ava-labs/firewood";
          license = {
            fullName = "Ava Labs Ecosystem License 1.1";
            url = "https://github.com/ava-labs/firewood/blob/main/LICENSE.md";
          };
          platforms = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
        };
      });
    in
    {
      packages = {
        inherit firewood-ffi;
        default = firewood-ffi;
      };

      apps.go = {
        type = "app";
        program = "${pkgs.writeShellScript "go" ''
          # Ensure a C compiler is available for cgo (e.g. on NixOS
          # where there is no system gcc).
          export PATH="${pkgs.stdenv.cc}/bin:$PATH"
          exec ${go}/bin/go "$@"
        ''}";
      };

      apps.jq = {
        type = "app";
        program = "${pkgs.jq}/bin/jq";
      };

      apps.just = {
        type = "app";
        program = "${pkgs.just}/bin/just";
      };

      apps.gh = {
        type = "app";
        program = "${pkgs.gh}/bin/gh";
      };

      devShells.default = craneLib.devShell {
        inputsFrom = [ firewood-ffi ];

        packages = with pkgs; [
          firewood-ffi
          gh
          go
          jq
          just
          rustToolchain
        ];

        shellHook = ''
          # Ensure golang bin is in the path
          GOBIN="$(go env GOPATH)/bin"
          if [[ ":$PATH:" != *":$GOBIN:"* ]]; then
            export PATH="$GOBIN:$PATH"
          fi

          # Force sequential build of vendored jemalloc for reproducibility
          export MAKEFLAGS="-j1"
        '';
      };
    });
}
