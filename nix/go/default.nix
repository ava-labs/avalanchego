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
  goVersion = "1.25.10";
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
    "linux-amd64" = "42d4f7a32316aa66591eca7e89867256057a4264451aca10570a715b3637ba70";
    "linux-arm64" = "654da1f9b50a5d1c2a85ccf8ed405aa89c06e94d18384628bf186f7712677b08";
    "darwin-amd64" = "52321165a3146cd91865ef98371506a846ed4dc4f9f1c9323e5ad90d2a411e06";
    "darwin-arm64" = "795691a425de7e7cdba3544f354dcd2cebcf52e87dc6898193878f34eb6d634f";
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
