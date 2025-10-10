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
  goVersion = "1.24.8";
  goSHA256s = {
    "linux-amd64" = "6842c516ca66c89d648a7f1dbe28e28c47b61b59f8f06633eb2ceb1188e9251d";
    "linux-arm64" = "38ac33b4cfa41e8a32132de7a87c6db49277ab5c0de1412512484db1ed77637e";
    "darwin-amd64" = "ecb3cecb1e0bcfb24e50039701f9505b09744cc4730a8b9fc512b0a3b47cf232";
    "darwin-arm64" = "0db27ff8c3e35fd93ccf9d31dd88a0f9c6454e8d9b30c28bd88a70b930cc4240";
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
  '';
}
