{ pkgs }:
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
      goURLPath = "${os}-${nixArchToGoArch arch}";
    };

  # Update the following to change the version:
  goVersion = "1.23.6";
  goSHA256s = {
    "linux-amd64" = "9379441ea310de000f33a4dc767bd966e72ab2826270e038e78b2c53c2e7802d";
    "linux-arm64" = "561c780e8f4a8955d32bf72e46af0b5ee5e0debe1e4633df9a03781878219202";
    "darwin-amd64" = "782da50ce8ec5e98fac2cd3cdc6a1d7130d093294fc310038f651444232a3fb0";
    "darwin-arm64" = "5cae2450a1708aeb0333237a155640d5562abaf195defebc4306054565536221";
  };

  targetSystem = parseSystem pkgs.system;
in
pkgs.stdenv.mkDerivation {
  name = "go-${goVersion}";
  version = goVersion;

  inherit (targetSystem) goos goarch;
  GOOS = targetSystem.goos;
  GOARCH = targetSystem.goarch;

  src = pkgs.fetchurl {
    url = "https://go.dev/dl/go${goVersion}.${targetSystem.goURLPath}.tar.gz";
    sha256 = goSHA256s.${targetSystem.goURLPath} or (throw "Unsupported system: ${pkgs.system}");
  };

  installPhase = ''
    mkdir -p $out
    cp -r ./* $out/
    chmod +x $out/bin/go
  '';
}
