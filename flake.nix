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
      # Development environment output
      devShells = forAllSystems ({ pkgs }: {
        default = pkgs.mkShell {
          # The Nix packages provided in the environment
          packages = with pkgs; [
            # Build requirements
            git

            # Task runner
            go-task

            # Local Go package
            (import ./nix/go.nix { inherit pkgs; })

            # Monitoring tools
            promtail                                   # Loki log shipper
            prometheus                                 # Metrics collector

            # Kube tools
            kubectl                                    # Kubernetes CLI
            k9s                                        # Kubernetes TUI
            kind                                       # Kubernetes-in-Docker
            kubernetes-helm                            # Helm CLI (Kubernetes package manager)

            # Linters
            shellcheck

            # Protobuf
            buf
            protoc-gen-go
            protoc-gen-go-grpc
            protoc-gen-connect-go

            # Solidity compiler
            solc

            # s5cmd for rapid s3 interactions
            s5cmd
          ] ++ lib.optionals stdenv.isDarwin [
            # macOS-specific frameworks
            darwin.apple_sdk.frameworks.Security
          ];

          # Add scripts/ directory to PATH so kind-with-registry.sh is accessible
          shellHook = ''
            export PATH="$PWD/scripts:$PATH"

            # Ensure golang bin is in the path
            GOBIN="$(go env GOPATH)/bin"
            if [[ ":$PATH:" != *":$GOBIN:"* ]]; then
              export PATH="$GOBIN:$PATH"
            fi
          '';
        };
      });
    };
}
