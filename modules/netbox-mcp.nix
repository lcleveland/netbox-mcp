{
  config,
  lib,
  pkgs,
  ...
}:
let
  inherit (lib)
    mkIf
    mkOption
    mkEnableOption
    types
    optional
    literalExpression
    ;
  cfg = config.services.netbox-mcp;
  groups = [
    "core"
    "ipam"
    "dcim"
    "tenancy"
    "virtualization"
    "extras"
  ];
  isLoopback = a: a == "::1" || a == "localhost" || lib.hasPrefix "127." a;
  inStore = p: p != null && lib.hasPrefix builtins.storeDir p;
  staticUser = cfg.user != null;

  args = [
    "--http"
    "--addr"
    (
      if lib.hasInfix ":" cfg.listenAddress then
        "[${cfg.listenAddress}]:${toString cfg.port}"
      else
        "${cfg.listenAddress}:${toString cfg.port}"
    )
    "--path"
    cfg.path
    "--url"
    cfg.url
    "--tool-groups"
    (lib.concatStringsSep "," cfg.toolGroups)
    "--max-bulk"
    (toString cfg.maxBulk)
    "--request-timeout"
    cfg.requestTimeout
    "--log-level"
    cfg.logLevel
  ]
  ++ optional cfg.allowCreate "--allow-create"
  ++ optional cfg.allowUpdate "--allow-update"
  ++ optional cfg.allowDelete "--allow-delete"
  ++ cfg.extraArgs;
in
{
  options.services.netbox-mcp = {
    enable = mkEnableOption "the NetBox MCP server (streamable HTTP)";

    package = mkOption {
      type = types.package;
      default = pkgs.callPackage ../pkgs/netbox-mcp.nix { };
      defaultText = literalExpression "pkgs.netbox-mcp";
      description = "The netbox-mcp package.";
    };

    url = mkOption {
      type = types.str;
      example = "https://netbox.example.com";
      description = "Base URL of the NetBox instance (4.6 or later).";
    };

    apiTokenFile = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "/run/secrets/netbox-mcp-token";
      description = ''
        Runtime path to a file holding a NetBox v2 API token (`nbt_<key>.<token>`).
        Passed via systemd `LoadCredential`, so it never enters the Nix store, argv
        or the environment. Use sops-nix, agenix or a root-owned 0400 file. The
        token's user permissions are the outer limit on what this server can do.
      '';
    };

    allowCreate = mkOption {
      type = types.bool;
      default = false;
      description = "Enable create actions (and IP/prefix/VLAN allocation).";
    };
    allowUpdate = mkOption {
      type = types.bool;
      default = false;
      description = "Enable update actions.";
    };
    allowDelete = mkOption {
      type = types.bool;
      default = false;
      description = "Enable delete actions. NetBox has no undo.";
    };

    maxBulk = mkOption {
      type = types.ints.positive;
      default = 50;
      description = "Maximum number of objects in one bulk write.";
    };

    toolGroups = mkOption {
      type = types.listOf (types.enum groups);
      default = groups;
      description = "Tool groups to register. `core` is always on.";
    };

    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
      description = "Address to listen on. A non-loopback address requires bearerTokenFile.";
    };
    port = mkOption {
      type = types.port;
      default = 8231;
      description = "TCP port for the MCP endpoint.";
    };
    path = mkOption {
      type = types.str;
      default = "/mcp";
      description = "URL path of the MCP endpoint.";
    };
    bearerTokenFile = mkOption {
      type = types.nullOr types.str;
      default = null;
      example = "/run/secrets/netbox-mcp-bearer";
      description = ''
        Runtime path to a shared secret HTTP clients must send as
        `Authorization: Bearer <token>`. Required for a non-loopback listener.
        `/healthz` stays open.
      '';
    };
    openFirewall = mkOption {
      type = types.bool;
      default = false;
      description = "Open port in the firewall.";
    };

    requestTimeout = mkOption {
      type = types.str;
      default = "30s";
      description = "Timeout for a single NetBox request, as a Go duration.";
    };
    logLevel = mkOption {
      type = types.enum [
        "debug"
        "info"
        "warn"
        "error"
      ];
      default = "info";
      description = "Log verbosity. Writes are always audit-logged at info.";
    };

    user = mkOption {
      type = types.nullOr types.str;
      default = null;
      description = "Run as this user instead of a systemd DynamicUser.";
    };
    group = mkOption {
      type = types.nullOr types.str;
      default = cfg.user;
      defaultText = literalExpression "config.services.netbox-mcp.user";
      description = "Group to run as when user is set.";
    };

    installCli = mkOption {
      type = types.bool;
      default = cfg.enable;
      defaultText = literalExpression "config.services.netbox-mcp.enable";
      description = ''
        Put the binary on the system PATH. Set this with enable = false on a
        workstation that only spawns the server over stdio from an MCP client.
      '';
    };
    extraArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
      description = "Extra command-line arguments. Never put a secret here.";
    };
    environment = mkOption {
      type = types.attrsOf types.str;
      default = { };
      description = "Extra environment variables. Never put a secret here.";
    };
  };

  config = lib.mkMerge [
    (mkIf cfg.installCli { environment.systemPackages = [ cfg.package ]; })

    (mkIf cfg.enable {
      assertions = [
        {
          assertion = cfg.apiTokenFile != null && lib.hasPrefix "/" cfg.apiTokenFile;
          message = "services.netbox-mcp.apiTokenFile must be an absolute runtime path to a NetBox v2 API token.";
        }
        {
          assertion = !(inStore cfg.apiTokenFile) && !(inStore cfg.bearerTokenFile);
          message = "services.netbox-mcp: token files must not live in ${builtins.storeDir}, which is world-readable. Use sops-nix, agenix or a root-owned 0400 file.";
        }
        {
          assertion = isLoopback cfg.listenAddress || cfg.bearerTokenFile != null;
          message = "services.netbox-mcp.listenAddress is ${cfg.listenAddress} (not loopback) without bearerTokenFile; the server refuses to start unauthenticated on a network address.";
        }
        {
          assertion = cfg.bearerTokenFile == null || lib.hasPrefix "/" cfg.bearerTokenFile;
          message = "services.netbox-mcp.bearerTokenFile must be an absolute path.";
        }
        {
          assertion = !staticUser || cfg.group != null;
          message = "services.netbox-mcp.group must be set when user is set.";
        }
        {
          assertion = lib.hasPrefix "/" cfg.path;
          message = "services.netbox-mcp.path must begin with a slash.";
        }
      ];

      warnings =
        optional cfg.allowDelete "services.netbox-mcp.allowDelete is true: a model can delete NetBox objects, and NetBox has no undo. Scope the token's permissions to what you intend to be deletable."
        ++
          optional (cfg.openFirewall && isLoopback cfg.listenAddress)
            "services.netbox-mcp.openFirewall has no effect while listenAddress is loopback (${cfg.listenAddress}).";

      users = mkIf staticUser {
        users.${cfg.user} = {
          isSystemUser = true;
          group = cfg.group;
        };
        groups.${cfg.group} = { };
      };

      systemd.services.netbox-mcp = {
        description = "NetBox MCP server";
        documentation = [ "https://github.com/lcleveland/netbox-mcp" ];
        wantedBy = [ "multi-user.target" ];
        after = [ "network-online.target" ];
        wants = [ "network-online.target" ];
        environment = cfg.environment;
        serviceConfig = {
          Type = "exec";
          ExecStart = "${lib.getExe cfg.package} ${lib.escapeShellArgs args}";
          Restart = "on-failure";
          RestartSec = 5;
          LoadCredential = [
            "api-token:${cfg.apiTokenFile}"
          ]
          ++ optional (cfg.bearerTokenFile != null) "http-auth-token:${cfg.bearerTokenFile}";

          User = mkIf staticUser cfg.user;
          Group = mkIf staticUser cfg.group;
          DynamicUser = !staticUser;

          AmbientCapabilities = [ "" ];
          CapabilityBoundingSet = [ "" ];
          DevicePolicy = "closed";
          LockPersonality = true;
          MemoryDenyWriteExecute = true;
          NoNewPrivileges = true;
          PrivateDevices = true;
          PrivateTmp = true;
          PrivateUsers = true;
          ProcSubset = "pid";
          ProtectClock = true;
          ProtectControlGroups = true;
          ProtectHome = true;
          ProtectHostname = true;
          ProtectKernelLogs = true;
          ProtectKernelModules = true;
          ProtectKernelTunables = true;
          ProtectProc = "invisible";
          ProtectSystem = "strict";
          RemoveIPC = true;
          # AF_NETLINK: Go's pure resolver reads interface addresses.
          RestrictAddressFamilies = [
            "AF_INET"
            "AF_INET6"
            "AF_NETLINK"
          ];
          RestrictNamespaces = true;
          RestrictRealtime = true;
          RestrictSUIDSGID = true;
          SystemCallArchitectures = "native";
          SystemCallFilter = [
            "@system-service"
            "~@privileged"
            "~@resources"
          ];
          UMask = "0077";
          SocketBindDeny = "any";
          SocketBindAllow = "tcp:${toString cfg.port}";
        };
      };

      networking.firewall.allowedTCPPorts = mkIf cfg.openFirewall [ cfg.port ];
    })
  ];
}
