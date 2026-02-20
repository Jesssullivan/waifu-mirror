{
  description = "Tailnet-only waifu image mirror service";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in {
        packages.default = pkgs.buildGoModule {
          pname = "waifu-mirror";
          version = "0.1.0";
          src = ./.;
          vendorHash = null; # Set after first build or use goModules
          CGO_ENABLED = 1;
          ldflags = [
            "-s" "-w"
            "-X main.version=0.1.0"
            "-X main.commit=${self.shortRev or "dirty"}"
          ];
          meta = {
            description = "Tailnet-only waifu image mirror with terminal optimization";
            mainProgram = "waifu-mirror";
          };
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go_1_25
            gopls
            gotools
          ];
        };
      });
}
