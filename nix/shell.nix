{ pkgs ? (
    let
      inherit (builtins) fetchTree fromJSON readFile;
      inherit ((fromJSON (readFile ./flake.lock)).nodes) nixpkgs gomod2nix;
    in
    import (fetchTree nixpkgs.locked) {
      overlays = [
        (import "${fetchTree gomod2nix.locked}/overlay.nix")
      ];
    }
  )
, mkGoEnv ? pkgs.mkGoEnv
, go ? pkgs.go
, kind-with-registry
}:

let
  goEnv = mkGoEnv {
    pwd = ./.;
    go = go;
    CGO_ENABLED = "1";
  };
in
pkgs.mkShell {
  packages = [
    goEnv

    # Monitoring tools
    pkgs.promtail                                   # Loki log shipper
    pkgs.prometheus                                 # Metrics collector

    # Kube tools
    pkgs.kubectl                                    # Kubernetes CLI
    pkgs.kind                                       # Kubernetes-in-Docker
    pkgs.kubernetes-helm                            # Helm CLI (Kubernetes package manager)

    # Script installing kind configured with a local registry
    kind-with-registry
  ];
}
