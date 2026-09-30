# Can `services.netbox` back a NixOS VM check?

Research for issue #5 (wayfinder map #1). Researched 2026-09-29.

**Short answer: yes.** nixpkgs already does this. `nixosTests.netbox` boots
`services.netbox`, creates a superuser and a v2 API token without any prompts,
and drives the REST and GraphQL APIs with curl. An MCP check can copy that test
almost line for line. There are three catches: the first boot takes about 6
minutes because of database migrations, the module's options differ between
26.05 and unstable, and on unstable you should set `package` explicitly.

Sources are pinned to these commits:

- `nixos-unstable` = `7a0f122f5090cf4c2ade2a13a0e229d4e19ba71f` (2026-09-28)
- `nixos-26.05` = `7fc6f2c20af09cdcaf48b92ec3121860139ec668` (2026-09-28)
- NetBox source at tag `v4.6.8`

Links below use branch names. Swap in the SHA to get a permalink.

## 1. Packaged versions

| Channel | Attrs | Version | Module default `package` |
|---|---|---|---|
| nixos-unstable | `netbox` (by-name); `netbox_4_6 = netbox` alias | **4.6.8** | `stateVersion >= 26.11` → `netbox_4_6`, else `netbox_4_5` |
| nixos-unstable | `netbox_4_1`..`netbox_4_5` | removed (`throw "... EOL"`, added 2026-08-24) | n/a |
| nixos-26.05 (stable) | `netbox_4_5` | **4.5.10** | `stateVersion >= 26.05` → `netbox_4_5`, else `netbox_4_4` |
| nixos-26.05 (stable) | `netbox_4_4` | 4.4.10, marked `knownVulnerabilities` | n/a |

Sources:
- [`pkgs/by-name/ne/netbox/package.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/pkgs/by-name/ne/netbox/package.nix) (`version = "4.6.8"`, built with Django 6)
- [`pkgs/top-level/all-packages.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/pkgs/top-level/all-packages.nix) (`netbox_4_6 = netbox;`)
- [`pkgs/top-level/aliases.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/pkgs/top-level/aliases.nix) (the 4.1–4.5 throws)
- 26.05: [`pkgs/by-name/ne/netbox_4_5/package.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-26.05/pkgs/by-name/ne/netbox_4_5/package.nix) and [`netbox_4_4/package.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-26.05/pkgs/by-name/ne/netbox_4_4/package.nix)
- Module `package` option: [unstable](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/nixos/modules/services/web-apps/netbox.nix) and [26.05](https://github.com/NixOS/nixpkgs/blob/nixos-26.05/nixos/modules/services/web-apps/netbox.nix)

**Trap on unstable:** when `system.stateVersion < "26.11"`, the default
`package` is `pkgs.netbox_4_5`, and that attribute is now a `throw`. Tests get
away with it because test nodes default `stateVersion` to
`lib.trivial.release` (currently `26.11`), set by
`system.stateVersion = (lib.mkOverride 1200) lib.trivial.release` in
[`nixos/modules/testing/test-instrumentation.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/nixos/modules/testing/test-instrumentation.nix).
Our module and check should still set `services.netbox.package = pkgs.netbox;`
so they don't depend on this.

## 2. What the module needs

The two branches differ a lot. unstable reworked the module in July–August 2026
(commits `37220b42` "refactor settings", `b50a6502` "add opt-in nginx support",
`f0cd25ba` "harden systemd units").

| Concern | nixos-unstable | nixos-26.05 |
|---|---|---|
| PostgreSQL | `postgresql.createLocally = true` by default. Adds `ensureDatabases = ["netbox"]` and an `ensureUsers` entry with DB ownership, over a Unix socket (`HOST = "/run/postgresql"`) with peer auth | Always enabled, same DB and user |
| Redis | `redis.createLocally = true`. Runs `services.redis.servers.netbox` over a Unix socket (db 0 for tasks, db 1 for caching) | Always enabled, same layout |
| `secretKeyFile` | Optional. If null, `preStart` generates `/var/lib/netbox/secret.key` | **Required** (`types.path`, no default). The test uses `pkgs.writeText`, which is fine for a test |
| API token pepper (needed for v2 tokens) | `apiTokenPepperFiles`, default `{ "1" = "/var/lib/netbox/pepper.1"; }`, auto-generated | `apiTokenPeppersFile`, nullable, must be set to get v2 tokens |
| Listen address | `bind` (gunicorn `--bind`), default `unix:/run/netbox/netbox.sock`, example `"[::1]:8001"`. `listenAddress`, `port` and `unixSocket` are removed | `listenAddress` (default `[::1]`) plus `port` (default `8001`), or `unixSocket` |
| nginx | Opt-in: `nginx.enable` + `nginx.hostname` create a vhost that proxies `/` and aliases `/static/` | No option. The test writes `services.nginx` by hand |
| Units | `netbox.target` groups `netbox.service` (gunicorn), `netbox-rq.service` and a `netbox-housekeeping` timer. `TimeoutStartSec = 10min` | Same set |
| CLI | `netbox-manage` on `PATH` (a Django `manage.py` wrapper). Runs as root via `runuser --preserve-environment -u netbox`, or as the `netbox` user | Same |

**We don't need nginx for an MCP check.** It only matters for `/static/` (the
web UI). The REST API at `/api/` is served by gunicorn directly. So set
`bind = "127.0.0.1:8001"` on unstable (or `listenAddress = "127.0.0.1"` on
26.05) and point the MCP server at `http://127.0.0.1:8001`. The module leaves
`ALLOWED_HOSTS` at `["*"]`, so the `Host` header doesn't matter.

First-start work happens in `netbox.service`'s `preStart`: `migrate`,
`trace_paths`, `collectstatic`, `remove_stale_contenttypes`,
`reindex --lazy` and `clearsessions`. It is skipped on later boots when
`/var/lib/netbox/version` already points at the package. In a fresh VM it
always runs, and it is the cost that dominates (see §4).

## 3. nixpkgs' own test: the pattern to copy

Files: [`nixos/tests/web-apps/netbox/default.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/nixos/tests/web-apps/netbox/default.nix)
and [`testScript.py`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/nixos/tests/web-apps/netbox/testScript.py).
It is registered as `netbox = runTest ./web-apps/netbox/default.nix;` in
[`all-tests.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/nixos/tests/all-tests.nix).
26.05 registers `netbox_4_4`, `netbox_4_5` and `netbox-upgrade`.

- `runTest` is the same machinery as `pkgs.testers.runNixOSTest`, so a flake check works.
- **unstable runs the test in a systemd-nspawn container** (`containers.machine`, not `nodes.machine`). The commit is `c36fc51c` "nixos/tests/netbox: run in nspawn", with the message "Faster and cheaper". Containers need the `uid-range` system feature on the builder (`uid-range.default = containers != { }` in [`nixos/lib/testing/run.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/nixos/lib/testing/run.nix)). A plain `ubuntu-latest` + install-nix-action runner may not advertise that feature. **A classic `nodes.machine` VM, which only needs `kvm` and `nixos-test`, is the safer choice for our CI**, and it matches how netskope-mcp's `tests/module.nix` is built.
- The 26.05 VM version sets `virtualisation.memorySize = 2048`. The qemu-vm default is 1024 MiB and 1 core ([`qemu-vm.nix`](https://github.com/NixOS/nixpkgs/blob/nixos-unstable/nixos/modules/virtualisation/qemu-vm.nix)).
- Readiness: `machine.wait_for_unit("netbox.target")`, then `machine.wait_until_succeeds("journalctl --since -1m --unit netbox --grep Listening")`.
- The test also runs OpenLDAP to exercise LDAP login. We don't need that.

Superuser and token setup in the unstable test, with no prompts:

```python
machine.succeed("netbox-manage createsuperuser --noinput --username netbox --email netbox@example.com")
machine.succeed("cat '${changePassword}' | netbox-manage shell --interface python")   # sets a password
stdout = machine.succeed("cat ${createToken} | netbox-manage shell")
```

where `createToken` is:

```python
from users.models import Token, User
from users.choices import TokenVersionChoices
u = User.objects.first()
t = Token.objects.create(user=u, token="0123456789abcdef0123456789abcdef01234567", version=TokenVersionChoices.V2)
print(t.get_auth_header_prefix())
```

After that, the test sends `Authorization: Token <prefix><plaintext>` on every
request. It creates, lists, patches and deletes sites, prefixes, manufacturers
and device-types, and runs a GraphQL query. It also asserts
`GET /api/status/` → `netbox-version`.

## 4. Boot time and memory

These are measured timings from Hydra logs of nixpkgs' own tests on x86_64
builders. Everything was already built, so the numbers are test runtime only.

| Build | Kind | `wait_for_unit netbox.target` | Whole test script |
|---|---|---|---|
| [hydra 346858144](https://hydra.nixos.org/build/346858144) (26.05, `netbox_4_5`) | VM, 2048 MiB, 1 core | **396.6 s** | 461 s |
| [hydra 347301453](https://hydra.nixos.org/build/347301453) (unstable, 4.6.8) | nspawn container | **377.3 s** | 422 s |

Where the time goes in the VM log:

| Kernel timestamp | Event |
|---|---|
| ~18 s | `Starting NetBox WSGI Service` |
| ~40 s | `Running migrations` |
| ~335 s | Migrations done (~295 s) |
| ~348 s | `collectstatic` done |
| ~377 s | `reindex` starts |
| ~396 s | gunicorn `Listening` |

`createsuperuser` plus the password change took about 23 s, because each
`netbox-manage` call starts Django cold.

What this means for our check:

- **Budget about 6–7 minutes per check run**, almost all of it in first-boot migrations. Moving to a container saves little (377 s vs 397 s). Extra cores may help a little, but migrations run in one process, so treat that as unverified.
- **Memory: use 2048 MiB.** It is the value upstream picked for the VM variant, and the test passes with it. Hydra doesn't report peak RSS, so I have no measured peak. PostgreSQL + Redis + gunicorn (default workers) + rqworker all fit in 2 GiB. 1 GiB is not known to work.
- The timeout is not a problem: `TimeoutStartSec = 10min` already covers `preStart`.
- Merge every Django step into one `netbox-manage shell` call to save ~20 s.

## 5. Creating a superuser and API token without prompts

All of this runs in the test script (or a oneshot unit) as root through
`netbox-manage`:

1. **Superuser.** Use Django's `createsuperuser --noinput`. If we need a password (the MCP only needs the token, so we may not), Django reads `DJANGO_SUPERUSER_PASSWORD` from the environment in `--noinput` mode. This is standard Django behaviour and was not re-checked here. `netbox-manage` passes env through (`runuser --preserve-environment`). Upstream's pipe-a-script-to-`shell` approach also works.
2. **Token: use v2 with a fixed key.** Source is NetBox [`users/models/tokens.py`](https://github.com/netbox-community/netbox/blob/v4.6.8/netbox/users/models/tokens.py) and [`users/constants.py`](https://github.com/netbox-community/netbox/blob/v4.6.8/netbox/users/constants.py).
   - `Token.objects.create(user=u, version=2, key="<12 chars>", token="<40 chars>")`. The `token` setter keeps a given `key` (`self.key = self.key or self.generate_key()`) and hashes the plaintext with the pepper. That means the test can **hard-code the full credential** and skip parsing stdout.
   - `TOKEN_PREFIX = "nbt_"`, `TOKEN_KEY_LENGTH = 12`, `TOKEN_DEFAULT_LENGTH = 40`, charset `[A-Za-z0-9]`.
   - Header: `Authorization: Bearer nbt_<key>.<plaintext>` ([REST API docs](https://github.com/netbox-community/netbox/blob/v4.6.8/docs/integrations/rest-api.md)). The server accepts either `Token` or `Bearer` as the keyword and works out the token version from the `nbt_` prefix ([`netbox/api/authentication.py`](https://github.com/netbox-community/netbox/blob/v4.6.8/netbox/netbox/api/authentication.py)).
   - v2 needs `API_TOKEN_PEPPERS`. On unstable the module generates one automatically. On 26.05 we must set `apiTokenPeppersFile`. `Token.clean()` raises "Unable to save v2 tokens: API_TOKEN_PEPPERS is not defined" when it is missing.
   - **Avoid v1 tokens.** They are "deprecated as of NetBox v4.6 and will be removed in NetBox v5.0" (REST API docs). The MCP should speak v2 `Bearer` from the start.
3. **Login endpoint: don't use it.** `/api/users/tokens/provision/` with username and password is the old route. The upstream test skips it for NetBox ≥ 4.5.2, and its comment says that getting a session or token without proper CSRF tokens "is no longer possible". The shell route is the only reliable one.
4. **Fixtures (`loaddata`): not needed.** Seed test objects through the REST API (the way upstream does) or through the MCP under test.

## 6. Recommended shape for our check (planning only)

- Pin the flake to nixos-unstable, like netskope-mcp. Build a `nodes.machine` VM with `virtualisation.memorySize = 2048` and:
  - `services.netbox = { enable = true; package = pkgs.netbox; bind = "127.0.0.1:8001"; };`
  - nginx off.
  - Our `services.netbox-mcp` pointed at `http://127.0.0.1:8001`, with a token file created by the test script. Or write a known token with a oneshot that runs `After=netbox.service`.
- Test script:
  1. `wait_for_unit("netbox.target")` and wait for the `Listening` line.
  2. Run one `netbox-manage shell` script that creates the user and a v2 token with a fixed key and plaintext.
  3. Write that token into the MCP's credential file and start or restart the MCP.
  4. Drive MCP tools and assert against NetBox (`/api/status/` version, then CRUD on a site).
- Keep the fast Go and eval checks separate from this ~7-minute VM check. We may want it out of the default `nix flake check`, or at least first in line for caching. Decide in the CI ticket.

## Open / unverified

- Peak memory in the VM. Nobody has measured it, and 2048 MiB is upstream's choice.
- Whether `ubuntu-latest` runners give the builder the `uid-range` feature. This only matters if we copy the nspawn variant, and the recommendation above avoids it.
- Whether `virtualisation.cores > 1` shortens the migration phase noticeably.
