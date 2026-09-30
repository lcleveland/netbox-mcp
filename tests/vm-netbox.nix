# End-to-end: the NixOS module against a real NetBox in a VM. First boot runs
# NetBox's migrations, so this takes ~7 minutes; the other checks stay fast.
{ pkgs, self }:
let
  token = "nbt_mcptestkey01.0123456789abcdef0123456789abcdef01234567";
  seed = pkgs.writeText "seed.py" ''
    from users.models import Token, User
    from users.choices import TokenVersionChoices
    u = User.objects.create_superuser("mcp", "mcp@example.com", "unused-password")
    Token.objects.create(user=u, key="mcptestkey01", token="0123456789abcdef0123456789abcdef01234567", version=TokenVersionChoices.V2)
  '';
in
pkgs.testers.runNixOSTest {
  name = "netbox-mcp-vm";
  nodes.machine = {
    imports = [ self.nixosModules.netbox-mcp ];
    virtualisation.memorySize = 2048;
    services.netbox = {
      enable = true;
      package = pkgs.netbox;
      bind = "127.0.0.1:8001";
    };
    # A fixed test token; a real deployment uses sops-nix/agenix.
    environment.etc."netbox-mcp-token".text = token;
    services.netbox-mcp = {
      enable = true;
      url = "http://127.0.0.1:8001";
      apiTokenFile = "/etc/netbox-mcp-token";
      allowCreate = true;
    };
    environment.systemPackages = [ pkgs.jq ];
  };

  testScript = ''
    import json

    TOKEN = "${token}"
    machine.wait_for_unit("netbox.target")
    # LOGIN_REQUIRED makes anonymous API calls 403; any HTTP answer means up.
    machine.wait_until_succeeds("curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8001/api/status/ | grep -qE '200|403'", timeout=900)
    machine.succeed("netbox-manage shell < ${seed}")
    machine.wait_for_unit("netbox-mcp.service")
    machine.wait_until_succeeds("curl -sf http://127.0.0.1:8231/healthz")

    sid = None
    def rpc(method, params=None, notify=False):
        global sid
        body = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            body["params"] = params
        if not notify:
            body["id"] = 1
        machine.succeed("cat > /tmp/req.json << 'JSON'\n" + json.dumps(body) + "\nJSON")
        h = f"-H 'Mcp-Session-Id: {sid}'" if sid else ""
        out = machine.succeed(
            "curl -sS -D /tmp/hdr -H 'Content-Type: application/json' "
            f"-H 'Accept: application/json, text/event-stream' {h} "
            "--data @/tmp/req.json http://127.0.0.1:8231/mcp"
        )
        if sid is None:
            sid = machine.succeed("grep -i '^mcp-session-id:' /tmp/hdr | cut -d' ' -f2 | tr -d '\\r\\n'")
        if notify:
            return None
        lines = [l[5:].strip() for l in out.splitlines() if l.startswith("data:")]
        resp = json.loads(lines[-1] if lines else out)
        assert "error" not in resp, resp
        return resp["result"]

    def tool(name, args=None, want_error=False):
        r = rpc("tools/call", {"name": name, "arguments": args or {}})
        assert bool(r.get("isError")) == want_error, r
        return r.get("structuredContent") or r["content"][0]["text"]

    rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "vm", "version": "0"}})
    rpc("notifications/initialized", notify=True)

    with subtest("status"):
        st = tool("netbox_status")
        assert st["authenticated"] and st["user"] == "mcp", st
        assert st["netbox_version"].startswith("4."), st

    with subtest("delete is off by default"):
        tools = {t["name"]: t for t in rpc("tools/list")["tools"]}
        enum = tools["netbox_prefix"]["inputSchema"]["properties"]["action"]["enum"]
        assert "create" in enum and "delete" not in enum and "update" not in enum, enum
        tool("netbox_api", {"method": "DELETE", "path": "/api/dcim/sites/1/", "reason": "x"}, want_error=True)

    with subtest("create with reason lands in the changelog"):
        out = tool("netbox_site", {"action": "create", "reason": "vm test site", "body": {"name": "VM Test", "slug": "vm-test"}})
        rid = out["request_id"]
        ch = tool("netbox_api", {"method": "GET", "path": "/api/core/object-changes/", "query": {"request_id": rid}})
        assert ch["count"] == 1 and ch["results"][0]["message"] == "vm test site", ch
        tool("netbox_site", {"action": "create", "body": {"name": "No Reason", "slug": "no-reason"}}, want_error=True)

    with subtest("list and allocate"):
        p = tool("netbox_prefix", {"action": "create", "reason": "vm test", "body": {"prefix": "10.9.0.0/29"}})
        pid = p["result"]["id"]
        lst = tool("netbox_prefix", {"action": "list", "query": {"prefix": "10.9.0.0/29"}})
        assert lst["count"] == 1, lst
        ip = tool("netbox_available", {"action": "allocate", "kind": "ip", "parent_id": pid, "reason": "vm test"})
        assert ip["result"]["address"] == "10.9.0.1/29", ip

    with subtest("token stays out of argv and environ"):
        pid = machine.succeed("systemctl show -p MainPID --value netbox-mcp").strip()
        machine.fail(f"tr '\\0' '\\n' < /proc/{pid}/cmdline | grep -F mcptestkey01")
        machine.fail(f"tr '\\0' '\\n' < /proc/{pid}/environ | grep -F mcptestkey01")
  '';
}
