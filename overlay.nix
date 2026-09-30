# nixpkgs.overlays = [ netbox-mcp.overlays.default ]; gives pkgs.netbox-mcp.
# Not needed for the NixOS module, which defaults to this flake's build.
final: _prev: {
  netbox-mcp = final.callPackage ./pkgs/netbox-mcp.nix { };
}
