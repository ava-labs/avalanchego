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
      osArch = "${os}-${nixArchToGoArch arch}";
    };

  # Update the following to change the version:
  lintVersion = "1.64.8";
  lintSHA256s = {
    "linux-amd64" = "b6270687afb143d019f387c791cd2a6f1cb383be9b3124d241ca11bd3ce2e54e";
    "linux-arm64" = "a6ab58ebcb1c48572622146cdaec2956f56871038a54ed1149f1386e287789a5";
    "darwin-amd64" = "b52aebb8cb51e00bfd5976099083fbe2c43ef556cef9c87e58a8ae656e740444";
    "darwin-arm64" = "70543d21e5b02a94079be8aa11267a5b060865583e337fe768d39b5d3e2faf1f";
  };

  targetSystem = parseSystem pkgs.system;
in
pkgs.stdenv.mkDerivation {
  name = "golangci-lint-${lintVersion}";
  version = lintVersion;

  src = pkgs.fetchurl {
    url = "https://github.com/golangci/golangci-lint/releases/download/v${lintVersion}/golangci-lint-${lintVersion}-${targetSystem.osArch}.tar.gz";
    sha256 = lintSHA256s.${targetSystem.osArch} or (throw "Unsupported system: ${pkgs.system}");
  };

  installPhase = ''
    mkdir -p $out/bin
    cp -r ./golangci-lint $out/bin
  '';
}
