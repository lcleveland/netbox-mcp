# REST vs GraphQL for read tools

Research for #4 (part of #1). Informs the decision in #6; this doc only recommends.

**Sources.** NetBox `main` at commit [`251458b`](https://github.com/netbox-community/netbox/tree/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c), which is release **v4.7.2** (2026-09-29, per `netbox/release.yaml`). Links below are pinned to that commit. `NB` = `https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c`.

- Docs: `docs/integrations/graphql-api.md`, `docs/integrations/rest-api.md`, `docs/reference/filtering.md`, `docs/configuration/graphql-api.md`, `docs/release-notes/version-4.*.md`
- Source: `netbox/netbox/graphql/{schema,views,types,pagination,filter_lookups}.py`, `netbox/{core,extras}/graphql/mixins.py`, `netbox/netbox/api/{viewsets/__init__,authentication}.py`, `netbox/utilities/querysets.py`, `netbox/ipam/api/urls.py`

Not measured: this doc has no byte or token counts from a live NetBox. Size claims below come from the serializer and schema definitions, not from benchmarks. If #6 needs numbers, run the same query both ways against a real instance.

## TL;DR

| Dimension | REST (`brief` / `fields` / `omit`) | GraphQL (Strawberry) |
|---|---|---|
| Read-only? | No. Read-only only if the tool sends GET, or the token isn't write-enabled | **Yes.** The schema has only `query=`, no mutations |
| Response size | Coarse. `fields` works on top-level fields only; each FK still comes back as a brief nested object with `url`, `display`, … | Fine-grained. Returns exactly the fields selected, nested too |
| Request / prompt cost | Small, easy to template (`?name__ic=x&fields=id,name`) | The model must write a valid query against a large schema (about 130 `*_list` roots in core); it needs introspection or a curated schema in context |
| Model coverage | Everything, plus special endpoints | All models, but **no** available-IPs/prefixes/VLANs/ASNs, no top-level object-change list |
| Custom fields | `cf_<name>=` filter, `custom_fields` in response | `custom_field_data: {path, lookup}` filter (4.3+), `custom_fields` JSON |
| Tags | `tag=`, `tag__n=`, `tag__any=` | `tags: {…TagFilter}` nested filter |
| Changelog | `/api/core/object-changes/?changed_object_type=…&changed_object_id=…` | Only per object: `changelog` field on each type |
| Journal | `/api/extras/journal-entries/` | `journal_entry_list` + `journal_entries` per object |
| Pagination | `limit`/`offset`, `count`, `next`; cursor `start` (4.6+) | `pagination: {offset\|start, limit}` (4.3+, cursor 4.5.2+). **No total count, no next link** |
| Page cap | `MAX_PAGE_SIZE` (default 1000) | `MAX_PAGE_SIZE` enforced only since **4.5.5**; applies to each nested list |
| Permissions | Same `restrict(user,'view')`. **403** if the model view perm is missing | Same `restrict()`. Missing perm gives **silent empty list** |
| Complexity limits | n/a (each call is one bounded queryset + nested briefs) | `GRAPHQL_MAX_ALIASES=10`; `GRAPHQL_MAX_QUERY_DEPTH` defaults to **None (unlimited)**; no cost/complexity limiter |
| Filter-syntax churn across 4.x | Stable (django-filter lookups) | Breaking changes in 4.0, 4.3, 4.5, 4.7 |

**Recommendation (not the decision; see #6):** build the resource-table read tools on **REST**, defaulting to `?brief=true` for list/search and `?fields=` for projection. Don't expose raw GraphQL to the model. If GraphQL is added at all, make it one optional, off-by-default `graphql_query` tool with the guards listed at the end. Reasons: REST covers what an LLM needs (available-IPs, changelog search, counts), fails loudly on missing permissions, uses filter syntax that has stayed stable across 4.x, and bounds the cost of each call. GraphQL saves response tokens, but the model pays for them again on the query side, and it breaks across minor versions.

## Findings

### 1. Is NetBox GraphQL read-only?

Yes. The docs call it "a read-only GraphQL API" (`docs/integrations/graphql-api.md`). In source, `strawberry.Schema(query=Query, …)` is built with no `mutation=` and no `subscription=` ([NB/netbox/netbox/graphql/schema.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/graphql/schema.py)). Plugins can register extra *query* classes (`registry['plugins']['graphql_schemas']` is mixed into `Query`), but they can't add mutations through that path.

It's the only endpoint (`path('graphql/', …)`, [NB/netbox/netbox/urls.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/urls.py)). `GRAPHQL_DEFAULT_VERSION` and `/graphql/v2/` are in `docs/configuration/graphql-api.md` and `settings.py`, but nothing in `urls.py` or `graphql/` reads the setting at this commit. Treat versioned GraphQL URLs as not present.

GraphQL can be switched off: `GRAPHQL_ENABLED` is a dynamic config parameter, and when it's false the view returns 404 ([views.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/graphql/views.py)). **A tool that depends on GraphQL can't assume it exists** and must probe for it.

### 2. Payload size and token cost

**REST response shape.** 
- `?brief=true` returns each model's `Meta.brief_fields`, e.g. Site `('id','url','display','name','description','slug')` ([NB/netbox/dcim/api/serializers_/sites.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/dcim/api/serializers_/sites.py)), Device `('id','url','display','name','description')` ([devices.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/dcim/api/serializers_/devices.py)). Brief mode is small, but you can't pick its fields.
- `?fields=a,b,c` (4.0+, #15087) and `?omit=` (4.5.2+, #21244) are split on commas and handed to the serializer ([NB/netbox/netbox/api/viewsets/__init__.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/api/viewsets/__init__.py) L125–144). They select **top-level fields only**. A selected FK still comes back as a full brief nested object. The docs' own `?fields=id,name,status,region` example returns `region` with `id,url,display,name,slug,description,site_count,_depth` (`docs/integrations/rest-api.md` "Specifying Fields"). Choice fields come back as `{value,label}`. Since 4.7, selection custom fields also come back as `{value,label}` (4.7 release notes, #20897).
- `fields` also narrows the DB query (4.0 notes), so it's a performance win as well as a size win.
- Every list response adds a `count`/`next`/`previous` envelope. `url` fields repeat the absolute base URL in every object. That wastes tokens, and the MCP server could strip them after fetch for free.

**GraphQL response shape.** Only the selected fields come back, nested to any depth (`device_list { name site { name } }`). Choice/enum fields return the bare enum value. For "give me names and sites of 200 devices", GraphQL's response is clearly smaller than REST `?fields=name,site`, because REST inlines the whole brief site object.

**Where GraphQL's savings go.** An LLM can only write GraphQL if it knows the schema. At this commit there are about 130 `*_list` root fields in core alone (`grep '_list: list' */graphql/schema.py`), each with a typed filter input. Most filter types were renamed in 4.5 (#20926) and many filter shapes changed in 4.3/4.5/4.7 (see §7), so model priors from training data are unreliable. Getting correct queries means putting introspection output or curated type snippets in context, which costs input tokens on every call. The model also pays for a retry every time it gets a query wrong. REST needs only the endpoint name plus django-filter parameter names, and the OpenAPI schema at `/api/schema/` can give the MCP server the valid parameters per endpoint.

**Net, as an assessment (not measured):** GraphQL wins on response tokens for wide, nested reads. REST with `brief`/`fields`, plus server-side stripping of `url` and similar noise, gets most of that back for the flat, per-resource-table reads this server plans.

### 3. Coverage of models and filters

| Need | REST | GraphQL |
|---|---|---|
| Custom field filter | `?cf_foo=123`, lookups apply (`docs/reference/filtering.md` "Filtering by Custom Field") | `custom_field_data: {path: "foo", lookup: {...}}` via `JSONFilter` ([filter_lookups.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/graphql/filter_lookups.py) L207), since 4.3 (#7598) |
| Custom field values in output | `custom_fields` object | `custom_fields` JSON field; emits every assigned CF key like REST ([extras/graphql/mixins.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/extras/graphql/mixins.py)) |
| Tags | `tag=` (AND), `tag__any=` (OR), `tag__n=` (NOR) (`filtering.md` "Tags") | nested `tags: TagFilter` ([extras/graphql/filter_mixins.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/extras/graphql/filter_mixins.py) L36) plus `AND/OR/NOT` |
| Boolean logic | Repeating a param means OR, except tags (AND) | Full `AND`/`OR`/`NOT` (4.3+) |
| Available IPs / prefixes / VLANs / ASNs | `prefixes/<pk>/available-ips/`, `ip-ranges/<pk>/available-ips/`, `prefixes/<pk>/available-prefixes/`, `vlan-groups/<pk>/available-vlans/`, `asn-ranges/<pk>/available-asns/` ([ipam/api/urls.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/ipam/api/urls.py) L33–55) | **Not available**, no matches in `ipam/graphql/` |
| Changelog | `/api/core/object-changes/`, filterable by object type/id, user, time, action ([core/api/urls.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/core/api/urls.py) L13) | No root query (`CoreQuery` exposes only `data_file`/`data_source`, [core/graphql/schema.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/core/graphql/schema.py)). Per-object `changelog` field from `ChangelogMixin` ([core/graphql/mixins.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/core/graphql/mixins.py)); it's a custom resolver returning a restricted queryset with no filter or pagination argument, so it looks unbounded (inferred from source, not tested) |
| Journal | `/api/extras/journal-entries/` | `journal_entry` / `journal_entry_list` roots plus per-object `journal_entries` ([extras/graphql/schema.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/extras/graphql/schema.py) L39–40) |
| Config context, render-config, trace, paths, scripts, reports | REST special actions | `config_context` field only |

### 4. Pagination

**REST** (`docs/integrations/rest-api.md` "Pagination"): `limit`/`offset`, default `PAGINATE_COUNT=50`, cap `MAX_PAGE_SIZE=1000`. Responses include `count` and `next`. Cursor mode `?start=<pk>&limit=` was added in **4.6.0** for all list endpoints; in that mode `count` is `null`. `start` and `offset` together return a 400. If `MAX_PAGE_SIZE` is 0/None, `?limit=0` returns every row.

**GraphQL** (`docs/integrations/graphql-api.md` "Pagination", [pagination.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/graphql/pagination.py)): `pagination: {offset, limit}` (added 4.3.0, #16224), `pagination: {start, limit}` cursor (4.5.2, #21110). The response has **no count and no has-next**; the client has to infer the last page from a short page. `MAX_PAGE_SIZE` is enforced only since **4.5.5** (#20385), and it's applied to every list field, nested ones too. Before 4.3 there was no pagination at all, and 4.3–4.5.4 had no cap. When `MAX_PAGE_SIZE` is 0/None, leaving out `pagination` returns everything, and `limit: 0` returns **zero** rows, the opposite of REST.

An LLM usually wants to know how many rows matched, and REST's `count` answers that in one call.

### 5. Permission enforcement

Both APIs authenticate with the same `TokenAuthentication` class, so token expiry, allowed-IP ranges and v1/v2 tokens behave the same ([graphql/views.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/graphql/views.py)). Both filter rows through `RestrictedQuerySet.restrict(user, 'view')` ([utilities/querysets.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/utilities/querysets.py) L116).

- GraphQL applies `restrict` in `BaseObjectType.get_queryset` ([types.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/graphql/types.py) L44–52), and on related lists through `RestrictedPrefetch` for journal entries, images, etc. `changelog` restricts explicitly.
- **Behavioural difference:** if the user lacks the model-level view permission, REST's `TokenPermissions` maps GET to `view_<model>` and returns **403** ([api/authentication.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/api/authentication.py) L131–161). GraphQL's `restrict()` returns `self.none()`, so the answer is an **empty list with no error**. An LLM will read that as "there are none". The same thing happens with a malformed custom field `path`: `JSONFilter` swallows the `ValueError` and returns `Q()`, so the filter is **silently dropped** and matches every row (filter_lookups.py L222–225).
- Nested permission enforcement in GraphQL has needed repeated fixes: 4.0.x #17310 (restricted querysets for related objects), #16228 (users/groups), 4.6.3 #22427 (ORM operator injection via `JSONFilter.path`), 4.6.4 #22507 (`is_active` check in the superuser bypass). REST's nested briefs have a smaller surface. Pin a minimum NetBox version if GraphQL is used.
- Write-enabled tokens don't matter for GraphQL, because the schema has no mutations.

### 6. Query depth and complexity limits

From [schema.py](https://github.com/netbox-community/netbox/blob/251458b89a5eb2f5fe0d20ecd1140ba08f141a9c/netbox/netbox/graphql/schema.py) and `docs/configuration/graphql-api.md`:

- `GRAPHQL_MAX_ALIASES`, default **10** (4.1.0, #17288), enforced by Strawberry's `MaxAliasesLimiter`. It counts aliases only. It does **not** limit how many distinct root fields a query selects without aliases.
- `GRAPHQL_MAX_QUERY_DEPTH`, default **None = unlimited** (4.6.1, #22060). When set, Strawberry's `QueryDepthLimiter` enforces it.
- No cost or complexity analysis, no timeout, no rate limiting at this layer. `DjangoOptimizerExtension` reduces N+1 queries but doesn't bound total work.
- Worst case with defaults: nested lists each capped at `MAX_PAGE_SIZE` (1000) multiply per level. `site_list { devices { interfaces { ip_addresses … } } }` can ask for 1000³+ rows in a single request. REST can't express that fan-out in one call.
- Introspection stays on (no `DisableIntrospection` extension). That helps the model discover the schema, and it also costs tokens.

### 7. GraphQL filter-syntax changes across 4.x

| Version | Change | Source |
|---|---|---|
| 4.0.0 | Engine moved from Graphene-Django to **Strawberry-Django**; "format for GraphQL query filters and lookups has changed" | 4.0 notes, #9856 |
| 4.1.0 | `GRAPHQL_MAX_ALIASES` added | #17288 |
| 4.3.0 | **Breaking:** "advanced syntax for filtering" with `AND/OR/NOT` and custom-field lookups; pagination added | #7598, #16224 |
| 4.5.0 | **Breaking:** ID and enum filters need a lookup: `id: 123` becomes `id: {exact: 123}`; filter type names standardized | #19338, #20926 |
| 4.5.2 | Cursor pagination (`start`) | #21110 |
| 4.5.5 | `MAX_PAGE_SIZE` enforced on GraphQL | #20385 |
| 4.6.1 | `GRAPHQL_MAX_QUERY_DEPTH` added | #22060 |
| 4.6.3 | `JSONFilter.path` validated (operator-injection fix) | #22427 |
| 4.6.4 | Errant `changelog` relation removed from schema; exceptions return JSON instead of HTML | #22440, #22501 |
| 4.7.0 | Selection CFs return `{value,label}`; `ServiceFilter` `ports` became flat `port`, `port__gt`…; `ServiceProtocolEnum` `ROLE_TCP` became `TCP`; plugins can extend core types/filters | 4.7 notes, #20897, #22592 |

An MCP tool that emits GraphQL has to track this per NetBox version. REST filter parameters (`name__ic`, `cf_x`, `tag`, `site_id`) stayed stable over the same range.

## If a GraphQL read tool is built: what it needs to be safe

Recommended guards, all enforced in the Go server before anything reaches NetBox:

1. **Probe availability and version.** Read `GET /api/status/` for `netbox-version`. Refuse GraphQL below a floor (suggest **≥ 4.6.4**, for depth limits, JSON-path validation and JSON errors), and when `/graphql/` returns 404 (`GRAPHQL_ENABLED=false`).
2. **Parse and validate the query in Go** (e.g. `github.com/vektah/gqlparser/v2`) against a cached introspected schema. Reject anything that isn't one `query` operation. The server has no mutations, but rejecting them keeps the tool honest if a plugin adds them.
3. **Enforce limits client-side**, since the server defaults are weak: max depth (e.g. 4–5), max aliases, max distinct root fields, and a required `pagination.limit` on **every** list field with a small cap (e.g. 100 at root, 20 nested). That caps the fan-out product.
4. **Response budget:** cap response bytes and tokens, and truncate with an explicit "truncated" marker.
5. **Warn about silent-empty results.** On an empty list, tell the model that missing permission or a bad `custom_field_data.path` gives an empty or unfiltered result with no error. Optionally cross-check with a REST `?limit=1` call, which returns 403 on a missing permission.
6. **Read-only token.** Use a token without write permission even though GraphQL is read-only, so the same credential is safe for the REST read tools. The per-verb write flags stay REST-only.
7. **Handle GraphQL errors.** A 200 response can carry an `errors` array; surface it verbatim.
8. **Server-side timeout** on the HTTP call. NetBox doesn't bound query cost.

## Recommendation (for #6 to decide)

- **Primary read path: REST.** Resource-table tools map directly to `/api/<app>/<model>/` with `?brief=true` by default, `?fields=` when the model asks for projection, `limit`/`offset` with `count` returned, and cursor `start` on 4.6+. Dedicated tools cover `available-ips`/`-prefixes`/`-vlans`/`-asns`, `core/object-changes`, and `extras/journal-entries`. Strip `url` (and optionally `display` duplicates) from responses server-side to recover most of GraphQL's size advantage.
- **GraphQL: optional, off by default.** Add it later if a real need for deep nested reads shows up. Build it only with the guards above, and gate it on version and `GRAPHQL_ENABLED`.
- **Open questions for #6:** minimum supported NetBox version (drives both cursor pagination and GraphQL safety); whether to measure real token deltas on a representative instance before deciding.
