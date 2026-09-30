# Module evaluation checks: no VM, just the generated unit.
{
  pkgs,
  self,
  lib,
}:
let
  evalModule =
    module:
    (lib.nixosSystem {
      inherit (pkgs.stdenv.hostPlatform) system;
      modules = [
        self.nixosModules.netbox-mcp
        {
          services.netbox-mcp.package = lib.mkForce (pkgs.writeShellScriptBin "netbox-mcp" "exit 0");
          boot.loader.grub.enable = false;
          fileSystems."/" = {
            device = "none";
            fsType = "tmpfs";
          };
          system.stateVersion = lib.trivial.release;
        }
        module
      ];
    }).config;

  failed = config: map (a: a.message) (builtins.filter (a: !a.assertion) config.assertions);

  base = extra: {
    services.netbox-mcp = {
      enable = true;
      url = "https://netbox.example";
      apiTokenFile = "/run/secrets/netbox-token";
    }
    // extra;
  };

  # Evaluates cleanly and the generated unit passes the grep checks.
  unitCheck =
    name: extra: checks:
    let
      config = evalModule (base extra);
      broken = failed config;
      svc = config.systemd.services.netbox-mcp;
    in
    assert broken == [ ] || throw "${name}: ${lib.concatStringsSep "; " broken}";
    pkgs.runCommand "netbox-mcp-${name}" { } ''
      cat > cmd <<'EOF'
      ${svc.serviceConfig.ExecStart}
      EOF
      cat > env <<'EOF'
      ${lib.concatStringsSep "\n" (lib.mapAttrsToList (k: v: "${k}=${v}") svc.environment)}
      EOF
      cat > creds <<'EOF'
      ${lib.concatStringsSep "\n" svc.serviceConfig.LoadCredential}
      EOF
      check() { grep -qF -- "$1" "$2" || { echo "missing from $2: $1"; cat "$2"; exit 1; }; }
      refute() { if grep -qF -- "$1" "$2"; then echo "unexpected in $2: $1"; cat "$2"; exit 1; fi; }
      ${checks}
      touch $out
    '';

  # Evaluation must trip an assertion containing `want`.
  mustFail =
    name: extra: want:
    let
      broken = failed (evalModule (base extra));
    in
    assert
      lib.any (lib.hasInfix want) broken
      || throw "${name}: expected assertion '${want}', got: ${toString broken}";
    pkgs.runCommand "netbox-mcp-${name}" { } "touch $out";
in
{
  module-eval = unitCheck "module-eval" { } ''
    check "--http" cmd
    check "--addr 127.0.0.1:8231" cmd
    check "--url https://netbox.example" cmd
    check "--tool-groups core,ipam,dcim,tenancy,virtualization,extras" cmd
    check "api-token:/run/secrets/netbox-token" creds
    refute "/run/secrets/netbox-token" cmd
    refute "netbox-token" env
    refute "--allow-" cmd
  '';

  module-full =
    unitCheck "module-full"
      {
        allowCreate = true;
        allowUpdate = true;
        listenAddress = "0.0.0.0";
        bearerTokenFile = "/run/secrets/bearer";
        toolGroups = [ "ipam" ];
        maxBulk = 10;
      }
      ''
        check "--allow-create" cmd
        check "--allow-update" cmd
        refute "--allow-delete" cmd
        check "--addr 0.0.0.0:8231" cmd
        check "--max-bulk 10" cmd
        check "http-auth-token:/run/secrets/bearer" creds
        refute "/run/secrets/bearer" cmd
      '';

  module-delete-warns =
    let
      config = evalModule (base {
        allowDelete = true;
      });
    in
    assert
      lib.any (lib.hasInfix "allowDelete is true") config.warnings || throw "no allowDelete warning";
    pkgs.runCommand "netbox-mcp-delete-warns" { } "touch $out";

  module-no-token = mustFail "no-token" { apiTokenFile = null; } "apiTokenFile";
  module-token-in-store = mustFail "token-in-store" {
    apiTokenFile = "${builtins.storeDir}/abc-token";
  } "world-readable";
  module-open-listener = mustFail "open-listener" {
    listenAddress = "0.0.0.0";
  } "without bearerTokenFile";
}
