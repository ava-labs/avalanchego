{
  # To use:
  #  - install nix: `./scripts/run_task.sh install-nix`
  #  - run `nix develop` or use direnv (https://direnv.net/)
  #    - for quieter direnv output, set `export DIRENV_LOG_FORMAT=`

  description = "AvalancheGo development environment";

  # Flake inputs
  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2505.*.tar.gz";
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
          ] ++ lib.optionals stdenv.isDarwin [
            # macOS-specific frameworks
            darwin.apple_sdk.frameworks.Security
          ];

          # Add scripts/ directory to PATH so kind-with-registry.sh is accessible
          shellHook = ''
            export PATH="$PWD/scripts:$PATH"
          '';
        };
      });
    };
}
