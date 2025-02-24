{ pkgs, package, rev }:

pkgs.dockerTools.buildImage {
  name = "avalanchego";
  # TODO(marun) This should be the commit hash
  tag = rev;
  created = "now";
  copyToRoot = pkgs.buildEnv {
    name = "image-root";
    paths = [
      # Ensure the binary is in the expected path
      (pkgs.runCommand "copy-binary" {} ''
        mkdir -p $out/build/avalanchego
        cp ${package}/bin/avalanchego $out/build/avalanchego/
      '')
      package
    ];
    pathsToLink = [ "/build" ];
  };
  config.Cmd = [ "/build/avalanchego/avalanchego" ];
}
