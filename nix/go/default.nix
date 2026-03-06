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
  goVersion = "1.25.0";
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
    "linux-amd64" = "2852af0cb20a13139b3448992e69b868e50ed0f8a1e5940ee1de9e19a123b613";
    "linux-arm64" = "05de75d6994a2783699815ee553bd5a9327d8b79991de36e38b66862782f54ae";
    "darwin-amd64" = "5bd60e823037062c2307c71e8111809865116714d6f6b410597cf5075dfd80ef";
    "darwin-arm64" = "544932844156d8172f7a28f77f2ac9c15a23046698b6243f633b0a0b00c0749c";
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
