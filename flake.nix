{
  description = "MCP server for NetBox, packaged with a NixOS module";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      inherit (nixpkgs) lib;
      forAllSystems = lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
      ];
      pkgsFor = system: nixpkgs.legacyPackages.${system};
    in
    {
      overlays.default = import ./overlay.nix;

      nixosModules = {
        netbox-mcp =
          { pkgs, ... }:
          {
            imports = [ ./modules/netbox-mcp.nix ];
            services.netbox-mcp.package =
              lib.mkDefault
                self.packages.${pkgs.stdenv.hostPlatform.system}.netbox-mcp;
          };
        default = self.nixosModules.netbox-mcp;
      };

      packages = forAllSystems (system: rec {
        netbox-mcp = (pkgsFor system).callPackage ./pkgs/netbox-mcp.nix { };
        default = netbox-mcp;
      });

      checks = forAllSystems (
        system:
        {
          # Runs the Go test suite in checkPhase.
          package = self.packages.${system}.netbox-mcp;
        }
        // import ./tests/eval.nix {
          inherit self lib;
          pkgs = pkgsFor system;
        }
        // {
          # ~7 minutes (NetBox migrations); build alone with
          # nix build .#checks.<system>.vm-netbox
          vm-netbox = import ./tests/vm-netbox.nix {
            inherit self;
            pkgs = pkgsFor system;
          };
        }
      );

      formatter = forAllSystems (system: (pkgsFor system).nixfmt-tree);

      devShells = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.go
              pkgs.gopls
              pkgs.gotools
              pkgs.nixfmt
              pkgs.jq
            ];
            env.CGO_ENABLED = "0";
          };
        }
      );
    };
}
