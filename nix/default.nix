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
, buildGoApplication ? pkgs.buildGoApplication
, go ? pkgs.go
, rev
}:

buildGoApplication {
  pname = "avalanchego";
  version = "1.12.2";
  src = ./.;
  pwd = ./.;
  modules = ./gomod2nix.toml;

  doCheck = false;

  go = go;

  CGO_ENABLED = "1";

  AVALANCHEGO_COMMIT = rev;
  buildPhase = ''
    bash -x ./scripts/build_avalanche.sh
  '';

  # Install the binary from where the build script places it
  installPhase = ''
    mkdir -p $out/bin
    mv build/avalanchego $out/bin/
  '';
}
