# netbox-mcp

An MCP server for [NetBox](https://netboxlabs.com/docs/netbox/) 4.6+, written in Go. It serves over stdio or streamable HTTP and is packaged as a Nix flake with a NixOS module.

The server is read-only by default. Writes are enabled per verb, and every write carries a reason that NetBox records in its changelog.

## Tools

| Group | Tools |
|---|---|
| core (always on) | `netbox_status` (connectivity/token/version probe), `netbox_api` (any `/api/` path, verb-gated) |
| ipam | `netbox_prefix`, `netbox_ip_address`, `netbox_ip_range`, `netbox_vlan`, `netbox_vlan_group`, `netbox_vrf`, `netbox_available` (free/allocate IPs, prefixes, VLANs) |
| dcim | `netbox_site`, `netbox_location`, `netbox_rack`, `netbox_device`, `netbox_interface`, `netbox_cable` |
| tenancy | `netbox_tenant` |
| virtualization | `netbox_virtual_machine`, `netbox_vm_interface` |
| extras | `netbox_journal_entry`, `netbox_tag`, `netbox_object_change` (changelog, read-only), `netbox_custom_field` (definitions, read-only) |

Resource tools take `action: list|get|create|update|delete`:
- `list` returns brief objects unless you pass `fields`. It caps results at 200 items and 60 KiB, and adds a `_truncation` note when it has to cut.
- `get` returns the full object plus its `_etag`.

## Configuration

| Flag | Env | Default |
|---|---|---|
| `--url` | `NETBOX_URL` | required |
| `--api-token-file` | `NETBOX_API_TOKEN_FILE` | see below |
| `--allow-create`, `--allow-update`, `--allow-delete` | | all off |
| `--max-bulk` | | 50 |
| `--tool-groups` | | all |
| `--stdio` / `--http` | | stdio |
| `--addr`, `--path` | | `127.0.0.1:8231`, `/mcp` |
| `--http-auth-token-file` | `NETBOX_MCP_HTTP_AUTH_TOKEN_FILE` | none (a non-loopback listener requires one) |
| `--request-timeout`, `--log-level` | `NETBOX_MCP_LOG_LEVEL` | `30s`, `info` |

**Token.** Use a NetBox v2 token (`nbt_<key>.<token>`) belonging to a dedicated user whose object permissions are scoped to what the server should touch. The server looks for it in this order:
1. `--api-token-file`
2. `NETBOX_API_TOKEN_FILE`
3. `NETBOX_API_TOKEN` (logs a warning)
4. the systemd credential `api-token`

There is no flag that takes the token directly.

**Write safety.** These rules cover the resource tools, `netbox_available` and `netbox_api` alike:
- A disabled verb is removed from the tool schemas and also rejected by the handler.
- Every write requires `reason`, which is sent as `changelog_message`.
- Bulk arrays are all-or-nothing and capped at `--max-bulk`.
- Updates accept `if_match`, which is sent as `If-Match`.
- Every write is audit-logged with NetBox's `X-Request-ID`. Look the change up with `netbox_object_change` and the query `{"request_id": ...}`.

## Nix

```nix
{
  inputs.netbox-mcp.url = "github:lcleveland/netbox-mcp";

  outputs = { nixpkgs, netbox-mcp, ... }: {
    nixosConfigurations.host = nixpkgs.lib.nixosSystem {
      modules = [
        netbox-mcp.nixosModules.default
        {
          services.netbox-mcp = {
            enable = true;
            url = "https://netbox.example.com";
            apiTokenFile = "/run/secrets/netbox-mcp-token"; # sops-nix / agenix
            allowCreate = true;
            allowUpdate = true;
          };
        }
      ];
    };
  };
}
```

The module:
- passes secrets through systemd `LoadCredential`, so they never reach the Nix store, argv or the environment;
- runs the service as a hardened DynamicUser;
- refuses a non-loopback listener without `bearerTokenFile`.

To install only the CLI for stdio use on a workstation, set `services.netbox-mcp.installCli = true;` and leave `enable` off.

### Claude Code (stdio)

```sh
claude mcp add netbox -e NETBOX_URL=https://netbox.example.com \
  -e NETBOX_API_TOKEN_FILE=$HOME/.config/netbox-mcp/token -- netbox-mcp
```

### Development

```sh
nix develop            # go, gopls, nixfmt
go test ./...
nix flake check        # package build + Go tests + module eval checks
nix build .#checks.x86_64-linux.vm-netbox   # VM test against a real NetBox (~7 min)
```

The design decisions are recorded in [Map: Go NetBox MCP server](https://github.com/lcleveland/netbox-mcp/issues/1). Research notes are in [`docs/research/`](docs/research/).
