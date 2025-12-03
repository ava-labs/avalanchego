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
  goVersion = "1.24.9";
  # The sha256 checksums can fetched from https://go.dev/dl/ for new versions.
  goSHA256s = {
    "linux-amd64" = "5b7899591c2dd6e9da1809fde4a2fad842c45d3f6b9deb235ba82216e31e34a6";
    "linux-arm64" = "9aa1243d51d41e2f93e895c89c0a2daf7166768c4a4c3ac79db81029d295a540";
    "darwin-amd64" = "961aa2ae2b97e428d6d8991367e7c98cb403bac54276b8259aead42a0081591c";
    "darwin-arm64" = "af451b40651d7fb36db1bbbd9c66ddbed28b96d7da48abea50a19f82c6e9d1d6";
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

  # Skip unpacking since we need special handling for the tarball
  dontUnpack = true;

  installPhase = ''
    mkdir -p $out
    # Extract directly to output, stripping the 'go' directory prefix
    # and ignoring permission/ownership issues in containers
    tar xzf $src -C $out --strip-components=1 --no-same-owner --no-same-permissions
    # Ensure go binary is executable
    chmod +x $out/bin/go
    # ROOT marker required by rules_go/rules_nixpkgs for SDK identification
    touch $out/ROOT
  '';
}
