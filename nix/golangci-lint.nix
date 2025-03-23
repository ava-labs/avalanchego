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

  # 1.63.4 is the only binary release compatible with go1.23.
  # Newer versions are built with go1.24.x and consume memory until OOMkilled.
  lintVersion = "1.63.4";
  lintSHA256s = {
    "linux-amd64" = "01abb14a4df47b5ca585eff3c34b105023cba92ec34ff17212dbb83855581690";
    "linux-arm64" = "51f0c79d19a92353e0465fb30a4901a0644a975d34e6f399ad2eebc0160bbb24";
    "darwin-amd64" = "878d017cc360e4fb19510d39852c8189852e3c48e7ce0337577df73507c97d68";
    "darwin-arm64" = "a2b630c2ac8466393f0ccbbede4462387b6c190697a70bc2298c6d2123f21bbf";
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
