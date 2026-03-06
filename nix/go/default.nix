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
  goVersion = "1.26.1";
  # The sha256 checksums can fetched from https://go.dev/dl/ for new versions.
  #
  # If using a version of nix < 2.25, it will be necessary to manually update the golang flake hash:
  #
  #  HASH=$(nix hash path ./nix/go)
  #  jq --arg hash "$HASH" \
  #    '.nodes["go-flake"].locked += {"lastModified": 1, "narHash": $hash}' \
  #    flake.lock > flake.lock.tmp
  #  mv flake.lock.tmp flake.lock
  #
  goSHA256s = {
    "linux-amd64" = "031f088e5d955bab8657ede27ad4e3bc5b7c1ba281f05f245bcc304f327c987a";
    "linux-arm64" = "a290581cfe4fe28ddd737dde3095f3dbeb7f2e4065cab4eae44dfc53b760c2f7";
    "darwin-amd64" = "65773dab2f8cc4cd23d93ba6d0a805de150ca0b78378879292be0b903b8cdd08";
    "darwin-arm64" = "353df43a7811ce284c8938b5f3c7df40b7bfb6f56cb165b150bc40b5e2dd541f";
  };

  targetSystem = parseSystem pkgs.stdenv.hostPlatform.system;
in
pkgs.stdenv.mkDerivation {
  name = "go-${goVersion}";
  version = goVersion;

  inherit (targetSystem) goos goarch;
  GOOS = targetSystem.goos;
  GOARCH = targetSystem.goarch;

  src = pkgs.fetchurl {
    url = "https://go.dev/dl/go${goVersion}.${targetSystem.goURLPath}.tar.gz";
    sha256 = goSHA256s.${targetSystem.goURLPath} or (throw "Unsupported system: ${pkgs.stdenv.hostPlatform.system}");
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
  '';
}
