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
  goVersion = "1.25.8";
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
    "linux-amd64" = "ceb5e041bbc3893846bd1614d76cb4681c91dadee579426cf21a63f2d7e03be6";
    "linux-arm64" = "7d137f59f66bb93f40a6b2b11e713adc2a9d0c8d9ae581718e3fad19e5295dc7";
    "darwin-amd64" = "a0b8136598baf192af400051cee2481ffb407f4c113a81ff400896e26cbce9e4";
    "darwin-arm64" = "c6547959f5dbe8440bf3da972bd65ba900168de5e7ab01464fbdc7ac8375c21c";
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
