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
, buildGoApplication ? pkgs.buildGoApplication
, go ? pkgs.go
, rev ? "dev"
}:

buildGoApplication {
  pname = "avalanchego";
  version = "1.13.5";
  src = ../.;
  pwd = ../.;
  modules = ./gomod2nix.toml;
  subPackages = [ "main" ];

  doCheck = false;

  go = go;

  buildInputs = [ pkgs.glibc.static ];

  CGO_ENABLED = "1";
  CGO_CFLAGS = "-O2 -D__BLST_PORTABLE__";
  CGO_LDFLAGS = "-L${pkgs.glibc.static}/lib";

  ldflags = [
    "-X github.com/ava-labs/avalanchego/version.GitCommit=${rev}"
    "-extldflags '-static'"
    "-linkmode external"
  ];

  postInstall = ''
    mv $out/bin/main $out/bin/avalanchego
  '';
}
