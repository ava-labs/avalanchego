{ pkgs ? (
    let
      inherit (builtins) fetchTree fromJSON readFile;
      inherit ((fromJSON (readFile ../flake.lock)).nodes) nixpkgs gomod2nix;
    in
    import (fetchTree nixpkgs.locked) {
      overlays = [
        (import "${fetchTree gomod2nix.locked}/overlay.nix")
      ];
    }
  )
, mkGoEnv ? pkgs.mkGoEnv
, go ? pkgs.go
}:

let
  goEnv = mkGoEnv {
    pwd = ../.;
    modules = ./gomod2nix.toml;
    go = go;
    CGO_ENABLED = "1";
  };
in
pkgs.mkShell {
  packages = [
    goEnv

    # Build requirements
    pkgs.git

    # Task runner
    pkgs.go-task

    # Monitoring tools
    pkgs.promtail                                   # Loki log shipper
    pkgs.prometheus                                 # Metrics collector

    # Kube tools
    pkgs.kubectl                                    # Kubernetes CLI
    pkgs.k9s                                        # Kubernetes TUI
    pkgs.kind                                       # Kubernetes-in-Docker
    pkgs.kubernetes-helm                            # Helm CLI (Kubernetes package manager)

    # Linters
    pkgs.shellcheck

    # Protobuf
    pkgs.buf
    pkgs.protoc-gen-go
    pkgs.protoc-gen-go-grpc
    pkgs.protoc-gen-connect-go

    # Solidity compiler
    pkgs.solc

    # s5cmd for rapid s3 interactions
    pkgs.s5cmd
  ];

  shellHook = ''
    # Ensure golang bin is in the path
    GOBIN="$(go env GOPATH)/bin"
    if [[ ":$PATH:" != *":$GOBIN:"* ]]; then
      export PATH="$GOBIN:$PATH"
    fi
  '';
}
