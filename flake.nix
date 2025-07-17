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
            self.packages.${system}.kind-with-registry # Script installing kind configured with a local registry

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
        };
      });

      # Package to install the kind-with-registry script
      packages = forAllSystems ({ pkgs }: {
        kind-with-registry = pkgs.stdenv.mkDerivation {
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
      });
    };
}
