# NetBox 4.x REST API facts for the MCP client

Research for issue #3 (map #1). It covers what a Go MCP client needs to know about the NetBox REST API. It targets **NetBox v4.7.2**, the latest release (published 2026-09-29), and notes differences back to **v4.0**.

**Sources.** The only sources are the `netbox-community/netbox` repository at tags `v4.7.2`, `v4.6.0`, `v4.5.0`, `v4.4.0`, `v4.3.0`, `v4.2.0`, `v4.1.0` and `v4.0.11`, and the docs in that repository (`docs/`, which is what netboxlabs.com/docs renders). Each citation gives a path in that repo. Unless it names another tag, the path is at `v4.7.2`. `RN x.y` means `docs/release-notes/version-x.y.md`. Some facts come from reading the code rather than from the docs. They are marked **(source-derived)**.

Short name used below: `[mixins]` = `netbox/netbox/api/viewsets/mixins.py`.

---

## 1. Authentication

### Header formats

| Token | Header | Available |
|---|---|---|
| v1 (legacy) | `Authorization: Token <40-char plaintext>` | 4.0 and later. **Deprecated in 4.6** and due for removal in 5.0 (RN 4.6 #22128). |
| v2 | `Authorization: Bearer nbt_<key>.<plaintext>` | **Added in 4.5.0** (RN 4.5, #20210) |

- The server tells the versions apart by the `nbt_` prefix, not by the keyword. It splits the value on the first `.` to get the key and the plaintext. It compares the keyword case-insensitively. If the keyword is neither `Token` nor `Bearer`, the header is ignored and the request proceeds as anonymous. (`netbox/netbox/api/authentication.py`, `TokenAuthentication.authenticate`)
- v2 tokens only work if the server has `API_TOKEN_PEPPERS` configured. Without it, NetBox still runs but v2 tokens are unavailable. (`docs/configuration/required-parameters.md#api_token_peppers`)
- Up to 4.4 the `Token` keyword is the only scheme. It is a subclass of DRF `TokenAuthentication`. (`netbox/netbox/api/authentication.py@v4.0.11`)
- A token can be limited in several ways: `write_enabled`, `expires`, `allowed_ips`, and `enabled` (added in 4.5, #20834). A token with writes disabled gets a 403 on any unsafe method. **Since 4.7**, running a custom script also needs a write-enabled token (#22411). (`docs/integrations/rest-api.md` §Tokens; `TokenPermissions._verify_write_permission`)
- **`GET /api/authentication-check/`** was added in 4.5 (#20936). It returns the serialized user, or 403 if the request is not authenticated. It is a cheap way to validate a token at startup. (`netbox/netbox/api/views.py` `AuthenticationCheckView`; `netbox/netbox/urls.py`)

### Auth failures return 403, never 401

`SessionAuthentication` is first in `DEFAULT_AUTHENTICATION_CLASSES` and does not send a `WWW-Authenticate` header. DRF therefore reports every `AuthenticationFailed` and `NotAuthenticated` error as **403**. This is true in 4.0 and 4.7 alike. (`netbox/netbox/settings.py` `REST_FRAMEWORK`) **(source-derived)**

Example 403 bodies (`detail` strings from `authentication.py`):
- `{"detail": "Authentication credentials were not provided."}`
- `{"detail": "Invalid v2 token"}` / `"Invalid v1 token"` / `"Token expired"` / `"Token disabled"` / `"User inactive"`
- `{"detail": "Invalid authorization header: Could not parse key from v2 token. Did you mean to use 'Token' instead of 'Bearer'?"}`
- `{"detail": "You do not have permission to perform this action."}`. This is DRF's message for a missing model permission or a read-only token.

A token is not needed for reads that `EXEMPT_VIEW_PERMISSIONS` exempts, or when `LOGIN_REQUIRED=False`. (`docs/integrations/rest-api.md` §Authenticating to the API)

---

## 2. Pagination

The response envelope is `{"count", "next", "previous", "results"}`. (`docs/integrations/rest-api.md` §Pagination)

- If no `limit` is given, the page size is `PAGINATE_COUNT`, which defaults to 50. The hard cap is `MAX_PAGE_SIZE`, which defaults to 1000.
- **A `limit` above `MAX_PAGE_SIZE` is silently clamped. It does not cause an error.** `limit=0` returns `MAX_PAGE_SIZE` rows, unless `MAX_PAGE_SIZE` is 0 or None, in which case it returns every row. A negative or non-integer `limit` is silently replaced by the default. Clients should therefore follow `next` rather than assume they got `limit` rows. (`netbox/netbox/api/pagination.py` `get_limit`) **(source-derived)**
- In 4.7 the default page size is `min(PAGINATE_COUNT, MAX_PAGE_SIZE)`. In 4.0, `PAGINATE_COUNT` applied even when it was larger than `MAX_PAGE_SIZE`. This was fixed in 4.4 (#20496) and 4.1 (#18150). (`pagination.py@v4.0.11`)
- **Cursor pagination was added in 4.6** (#21363). Pass `?start=<min pk>&limit=N`. Results are ordered by pk, `count` is `null`, `previous` is `null`, and `next` holds `start=<last pk + 1>`. Combining `start` with `offset` returns 400. Combining `start` with `ordering` also returns 400. On a 4.0–4.5 server, `start` is ignored. It is just an unknown query parameter there, so the server silently falls back to offset pagination. (`pagination.py`; `docs/integrations/rest-api.md` §Cursor-Based Pagination) **(source-derived for the pre-4.6 behaviour)**
- `next` and `previous` are absolute URLs built from the request host. Behind a proxy, the host in those URLs may not be the base URL the client configured.

The `available-*` endpoints are **not** paginated. See §4.

---

## 3. `?brief`, `?fields`, `?omit`

(`docs/integrations/rest-api.md` §Specifying Fields, §Brief Format; `netbox/netbox/api/viewsets/__init__.py` `BaseViewSet.field_kwargs`, `initialize_request`; `netbox/netbox/api/serializers/base.py`)

- **`?fields=a,b,c`** was added in **4.0** (RN 4.0 #15087). It returns only the listed fields, and the server also skips the prefetches and annotations for fields that are left out, so it is a real performance win. Unknown field names are silently ignored. Nested related objects are still rendered in brief form.
- **`?omit=a,b`** was added in **4.5** (#21244). If both are given, `fields` wins.
- On `core` endpoints (jobs, object-changes, object-types), `fields` only works **from 4.5** (#21139).
- **`?brief=...`** is applied **only on GET**. When it is active, the viewset uses `Meta.brief_fields`. Since 4.0 those include `description` (RN 4.0 #15238). The priority order is `fields`, then `omit`, then `brief`.
- **Gotcha: any non-empty value turns brief mode on.** The code does `request.GET.get('brief')` and tests the result for truthiness, so `?brief=false` and `?brief=0` both enable brief mode. Leave the parameter out entirely to get full output. (`viewsets/__init__.py` line ~106) **(source-derived)**
- **Gotcha: `fields` and `omit` also apply on writes, and to input as well as output.** The serializer's `fields` property removes the unlisted fields, and DRF builds its writable fields from that same property. A POST or PATCH with `?fields=id` therefore silently drops every other input field. Never forward `fields` or `omit` on a write. **(source-derived)**
- Brief and field selection are **not** applied to the stored results of background jobs (4.7). (`docs/integrations/rest-api.md` §Background Processing)

---

## 4. `available-ips`, `available-prefixes`, `available-vlans` (and `available-asns`)

The routes (`netbox/ipam/api/urls.py`):

- `/api/ipam/prefixes/{id}/available-ips/`
- `/api/ipam/ip-ranges/{id}/available-ips/`
- `/api/ipam/prefixes/{id}/available-prefixes/`
- `/api/ipam/vlan-groups/{id}/available-vlans/`
- `/api/ipam/asn-ranges/{id}/available-asns/`

The implementation is `AvailableObjectsView` and its subclasses in `netbox/ipam/api/views.py`. Its structure is the same in 4.0.11.

### GET

- The response is a **plain JSON list**, not a paginated envelope.
  - Each IP looks like `{"family", "address": "<ip>/<parent mask>", "vrf"}`.
  - Each prefix looks like `{"family", "prefix", "vrf"}`.
  - Each VLAN looks like `{"vid", "group"}`.
- For ips, vlans and asns, `?limit` means `min(limit or PAGINATE_COUNT, MAX_PAGE_SIZE)`, and `limit=0` means `MAX_PAGE_SIZE` (`get_results_limit`).
- **available-prefixes ignores `limit`** and returns every free CIDR block. **(source-derived)**
- Which IPs count as available (`netbox/ipam/models/ip.py` `Prefix.usable_ip_bounds`, `get_child_ips`):
  - An IPv4 prefix excludes its network and broadcast addresses, unless it is `is_pool` or /31–/32.
  - An IPv6 prefix excludes the subnet-router anycast address, unless it is a pool or /127–/128.
  - Child IP ranges marked `mark_populated` count as used.
  - IPs are matched within the same VRF. The exception is a global-table (`vrf=null`) prefix with status `container`, which counts IPs from any VRF.
- The VLAN list is built from the group's `vid_ranges` minus the VIDs already in use (`netbox/ipam/models/vlans.py` `get_available_vids`).

### POST (allocation)

- The body can be one object or a list, which gives **bulk allocation**.
  - If the body is one object, the response is one object.
  - If the body is a list, the response is a list.
  - Success returns **201**.
- The fields per entry depend on the endpoint:
  - **available-ips**: `{}` or any IPAddress fields (status, dns_name, description, tags, custom_fields, assigned_object_*, etc.).
    - Since **4.5** an optional `prefix_length` overrides the mask (#21144). It must be at least the parent's mask length.
    - `address` and `vrf` are always overwritten with the next free IP and the parent's VRF.
  - **available-prefixes**: `prefix_length` (int) is **required**, plus any Prefix fields.
    - The server picks the first free block of that size.
    - The VRF is inherited from the parent. Before 4.5.x the OpenAPI schema for this request body was wrong (#21658).
  - **available-vlans**: `name` is required (the VLAN model needs it), and `site`, `tenant`, `status`, `role`, `description`, `tags` and `custom_fields` are optional.
    - `vid` and `group` are set by the server (`CreateAvailableVLANSerializer`). The OpenAPI schema was corrected in 4.5.x (#21966).
- **Concurrency.** Choosing and creating the objects happens inside a **PostgreSQL advisory lock**, one lock per kind: `available-ips`, `available-prefixes`, `available-vlans`, `available-asns` (`ADVISORY_LOCK_KEYS`). The lock is global across all parents, not per prefix, so concurrent allocations are serialized and cannot hand out the same IP twice. The ordinary `ip-addresses` create, update and destroy calls take the **same** `available-ips` lock. The write itself is wrapped in `transaction.atomic`. This holds for 4.0 through 4.7.
- **All-or-nothing.** If there are fewer free objects than requested, the server returns **409** `{"detail": "Insufficient resources are available to satisfy the request"}` and creates nothing. Validation errors return **400**, reported by position as a list aligned with the request: `[{}, {"field": [...]}]` (`utilities/api.py` `get_positional_errors`).
- The server needs `add` permission on the child model and view permission on the parent. If the parent is not visible to the user, the response is 404.

---

## 5. Journal entries and object changes (the changelog)

### Journal entries: `/api/extras/journal-entries/` (full CRUD)

(`netbox/extras/api/serializers_/journaling.py`, `netbox/extras/filtersets.py` `JournalEntryFilterSet`, `netbox/extras/choices.py`)

- **Write fields:**
  - `assigned_object_type` is required, as `"app_label.model"`, for example `"dcim.device"`.
  - `assigned_object_id` is required. The target object must exist or the response is 400.
  - `comments` holds Markdown text.
  - `kind` is optional and is one of `info` (the default), `success`, `warning` or `danger`.
  - `tags` and `custom_fields` are also accepted.
  - `created_by` defaults to the requesting user and is read-only on update.
- **Filters:**
  - `assigned_object_type` (`app.model`, multi-value)
  - `assigned_object_type_id`, `assigned_object_id`
  - `created_by` (username), `created_by_id`
  - `kind` (multi-value)
  - `created_after` / `created_before` (a DateTimeFromToRange on `created`)
  - `q`, which searches `comments`
  - the standard `id`, `tag`, `created`/`last_updated` lookups

### Object changes: read-only

- The path is **`/api/core/object-changes/`** from **4.1** onward. In **4.0** it is **`/api/extras/object-changes/`**. The move in 4.1 left no alias behind. (RN 4.1 "The `/api/extras/object-changes/` endpoint has moved to `/api/core/object-changes/`"; `netbox/extras/api/urls.py@v4.0.11` vs `netbox/core/api/urls.py@v4.1.0`)
- **Fields:**
  - `id`, `url`, `display`, `time`
  - `user` (nested), `user_name`
  - `request_id`
  - `action`, as `{value: create|update|delete, label}`
  - `changed_object_type`, `changed_object_id`, `changed_object`
  - `object_repr`
  - `message`
  - `prechange_data`, `postchange_data`

  The default ordering is `-time`. (`netbox/core/api/serializers_/change_logging.py`; `netbox/core/models/change_logging.py`)
- **Filters** (`netbox/core/filtersets.py` `ObjectChangeFilterSet`):
  - `id`
  - `user` (username), `user_id`, `user_name`
  - `request_id`
  - `action`
  - `changed_object_type` (`app.model`), `changed_object_type_id`, `changed_object_id`
  - `related_object_type`, `related_object_id`
  - `object_repr`
  - `time_after` / `time_before`
  - `q`, which matches `user_name`, `object_repr` or `message`
- **The `X-Request-ID` response header** comes back on every response. You can then query `GET /api/core/object-changes/?request_id=<uuid>` to find the changelog rows a write produced, or `GET /api/<app>/<model>/?created_by_request=<uuid>`. (`docs/integrations/rest-api.md` §HTTP Headers)
- **`changelog_message`** is a write-only field you can put in any create, update or delete body, and in bulk-delete entries. It is stored as `ObjectChange.message`. It was **added in 4.4**. (`docs/integrations/rest-api.md` §Changelog Messages; `netbox/netbox/api/serializers/features.py` is present at v4.4.0 but not at v4.3.0)

---

## 6. Tags and custom-field write formats

### Tags

(`netbox/netbox/api/serializers/features.py` `TaggableModelSerializer`; `netbox/netbox/api/serializers/nested.py`; `utilities/api.py` `get_related_object_by_attrs`)

- **`tags`** is a list. Each element is either a tag **pk** (int) or a **dict of attributes** that identifies exactly one tag, such as `{"name": "prod"}` or `{"slug": "prod"}`.
  - The tag must already exist. Tags are never auto-created.
  - If no tag matches, or several do, the response is 400: `"Related object not found using the provided attributes: ..."` or `"Multiple objects match ..."`.
  - Writing `tags` **replaces the whole set**.
  - Lookups by attributes only see tags the user can view. Lookups by pk are always allowed.
- **`add_tags` and `remove_tags`** were added in **4.6** (#21771). They are write-only lists in the same format as `tags` and apply incremental changes. Three rules apply:
  - You cannot combine them with `tags`.
  - You cannot use `remove_tags` on create.
  - The same tag cannot appear in both.

  Breaking any rule returns 400. On 4.0–4.5 the server silently ignores these fields, because they are unknown to it. **(source-derived)**
- Tags restricted by `object_types` return 400 through `AbortRequest` when assigned to the wrong model.
- Reads return each tag nested as `{id, url, display_url, display, name, slug, color}`.

### Custom fields

(`netbox/extras/api/customfields.py` `CustomFieldsDataField`; `netbox/extras/models/customfields.py`)

- **`custom_fields`** is a dict of `{cf_name: value}`. Any other type returns 400.
- **Merge semantics:** on update, the submitted keys are merged over the existing `custom_field_data`. A PATCH of one key does not wipe the others. Send `null` to clear a value. **(source-derived)**
- An unknown cf name returns **400**. In 4.7 the errors are keyed per field: `{"custom_fields": {"<name>": ["Custom field '<name>' does not exist for this object type."]}}`. In 4.0 the error came from model clean and read `"Unknown field name '<name>' in custom field data."` (`netbox/netbox/models/features.py@v4.0.11`).
- Write value per type:
  - text, longtext, url, json: native JSON values. From 4.7, a url without a scheme is normalized to `https://`.
  - integer, decimal, boolean: native JSON values.
  - date: `YYYY-MM-DD`. datetime: ISO 8601.
  - select: the choice **value** string. multiselect: a list of value strings.
  - object: a pk or an attribute dict.
  - multiobject: a list of pks or dicts.
- **Read shape change in 4.7** (#20897): select and multiselect custom fields now read as `{"value", "label"}` objects, or a list of them. Before 4.7 they read as plain strings. You still **write** plain values, so a read-modify-write client must unwrap them first. Object custom fields read as nested brief objects.
- On create, the default values of custom fields are applied (RN 4.2 #18669).

---

## 7. `/api/status/`

`GET /api/status/` is open to anyone when `LOGIN_REQUIRED=False`, and otherwise needs authentication. (`netbox/netbox/api/views.py` `StatusView`)

| Key | 4.0–4.2 | 4.3 | 4.4+ |
|---|---|---|---|
| `django-version` | yes | yes | yes |
| `installed-apps` (dict of app to version) | yes | yes | **renamed `installed_apps`** |
| `netbox-version` | yes | yes | yes |
| `netbox-full-version` | – | added | yes |
| `hostname` | – | – | added (#19893) |
| `plugins` (dict of name to version) | yes | yes | yes |
| `python-version` | yes | yes | yes |
| `rq-workers-running` (int) | yes | yes | yes |

The `installed-apps` → `installed_apps` rename is not in the release notes. It was found by running `git grep` across the tags. The `API-Version` response header gives the `major.minor` version and is on every API response. It is the cheapest version probe. (`docs/integrations/rest-api.md` §HTTP Headers)

For the MCP client, `rq-workers-running > 0` shows whether `?background=true` would be accepted (see §8).

---

## 8. Bulk create, update and delete

(`docs/integrations/rest-api.md` §Creating/Updating/Deleting Multiple Objects; `[mixins]`)

- **Create:** `POST /api/<app>/<model>/` with a JSON **list**. The response is 201 with a list.
- **Update:** `PATCH` (partial) or `PUT` (full) on the **list** endpoint, with `[{"id": N, ...attrs}, ...]`. The response is 200 with a list.
- **Delete:** `DELETE` on the list endpoint, with `[{"id": N, "changelog_message"?: "..."}, ...]`. The response is 204.
- All three are **all-or-nothing**: they run in one transaction, and a single failure rolls back the whole batch. This is true in every 4.x release.
- Changes in **4.7** (RN 4.7 breaking changes; `[mixins]`):
  - Update and delete reject a batch that lists the same id twice, with 400.
  - Update and delete reject ids that are missing or not visible to the user, with 400 and `"errors":[{"id":N,...}]`. In ≤4.6 such ids were **silently skipped** (`[mixins]@v4.6.0`: `qs.filter(pk__in=...)` with no check).
  - A body that is not a list returns 400 with `{"detail": "Expected a list of objects, but got ..."}`.
  - All errors are collected, not just the first. See §9.
- **`?background=true` (4.7+)** applies only to bulk list writes.
  - The server returns **202** `{"job": {"id", "url", "status": "pending"}}`. Validation is **deferred**, so you must poll `/api/core/jobs/{id}/`. Its `data` holds `{"status_code", "data"}` mirroring the synchronous response, and `error` holds a summary.
  - It returns 503 if no RQ worker is running.
  - It returns 400 when combined with `If-Match`, and 400 on `/api/users/tokens/`.
  - On a single-object write the flag is ignored. (`docs/integrations/rest-api.md` §Background Processing; RN 4.7 #21992)
- **ETag and `If-Match` (4.6+):** detail responses carry a weak `ETag: W/"<last_updated iso>"`. A `PATCH` or `PUT` with `If-Match` gets **412** if the value is stale, and the 412 includes the current ETag. `If-Match: *` asserts that the object exists. Without the header, the last write wins. (RN 4.6 #21356; `docs/integrations/rest-api.md` §Concurrent Update Protection; `utilities/exceptions.py` `PreconditionFailed`)
- **Trailing slash is required.** Without it, a GET is redirected with a 302. Before 4.5.x, a POST or PATCH without it raised an exception (#21906). Always send the slash.

---

## 9. Error response shape by status code

Normal error bodies are DRF JSON. The one exception is the 500 handler.

| Status | When | Body |
|---|---|---|
| **400** | Serializer or model validation fails | `{"<field>": ["msg", ...], ...}`. Errors that belong to no field go under **`"__all__"`** in **4.7** (`NON_FIELD_ERRORS_KEY='__all__'` in `settings.py@v4.7.2`). In ≤4.6 serializer-level errors went under DRF's default `"non_field_errors"` and model `clean()` errors under `"__all__"`, so accept both. Nested fields give nested dicts, for example `{"custom_fields": {"x": [...]}}`. |
| **400** | `AbortRequest`, for example a protection rule or a restricted tag | `{"detail": "<message>"}` (`viewsets/__init__.py` `dispatch`) |
| **400** | Bulk create or update validation, **4.7+** | `{"detail": "N of M objects failed validation.", "errors": [{"index": i, "errors": {...}}]}` for create and malformed entries. Per-object update failures are keyed by **`"id"`** instead of `"index"`: `{"detail": "N of M objects could not be updated.", "errors": [{"id": pk, "errors": {...}}]}`. The docs only show `index`, so treat both keys as possible. (`[mixins]` `get_invalid_entries_response`, `perform_bulk_update`, `perform_bulk_destroy`) |
| **400** | Bulk validation, **≤4.6** | The first failing item stops the batch. Bulk update returns that item's plain field-error dict with no index. Bulk create in 4.0 does the same (`[mixins]@v4.0.11`). |
| **400** | available-* POST validation | A positional list: `[{}, {"prefix_length": [...]}]`, or a plain dict for a single-object body |
| **403** | No or invalid token, missing permission, or a read-only token on a write | `{"detail": "..."}` (see §1). Bulk ops in 4.7 report per-object permission failures with a 403 `errors` list. |
| **404** | The object does not exist or is not visible to the user | `{"detail": "No <Model> matches the given query."}` (DRF/Django). A missing object and a forbidden one are deliberately not told apart. |
| **405** | Wrong method | `{"detail": "Method \"X\" not allowed."}` |
| **409** | Delete blocked by dependent objects (single delete) | `{"detail": "Unable to delete object. N dependent objects were found: <repr> (<pk>), ..."}` |
| **409** | Bulk delete blocked, 4.7 | `{"detail": "N of M objects could not be deleted.", "errors": [{"id": pk, "errors": {"__all__": ["Unable to delete: N dependent object(s) prevent deletion."]}}]}`. A bulk response has one status: when a batch has mixed failures, 403 outranks 409, which outranks 400 (`BULK_ERROR_STATUSES`). |
| **409** | available-* has too little free space | `{"detail": "Insufficient resources are available to satisfy the request"}` |
| **412** | `If-Match` does not match (4.6+) | `{"detail": "Precondition failed."}`, plus an `ETag` header |
| **202** | `?background=true` accepted (4.7+) | `{"job": {...}}`. This is not an error, but a caller must not read it as success. |
| **500** | Unhandled exception (when `DEBUG=False`), **and a write attempted in maintenance mode** | **Not** DRF-shaped: `{"error": "<message>", "exception": "<ExceptionClass>", "netbox_version": "...", "python_version": "..."}`. In maintenance mode, `error` is the "operating in maintenance mode ..." message and the status is still 500. (`utilities/error_handlers.py` `handle_rest_api_exception`; `netbox/netbox/middleware.py`) |
| **503** | Background job requested or script run with no RQ worker | `{"detail": "Unable to process request: RQ worker process not running."}` (`utilities/exceptions.py`) |

For the client this means: parse `detail` if present, then `errors` (4.7 bulk), then treat the body as a field-error map (or a list, for available-*). For a 500, read `error` and `exception`.

---

## 10. Other facts relevant to the MCP design

- **Related objects on write** can be given as a pk or as an attribute dict such as `{"site": {"name": "X"}, "name": "R1"}`. The dict must match exactly one object, or the response is 400. Lookups by attributes are limited to objects the user can view. Generic relations use `<field>_type: "app.model"` and `<field>_id`. (`docs/integrations/rest-api.md` §Related Objects, §Generic Relations)
- **Path renames that affect a resource table:**
  - `extras/content-types` became `extras/object-types` in 4.0.
  - `extras/object-types` moved to `core/object-types` in 4.4 (#19829).
  - `extras/object-changes` moved to `core/object-changes` in 4.1.
  - `extras/reports` was removed in 4.0.
- Hierarchical models, such as regions, locations and tenant groups, use `ltree` instead of MPTT **in 4.7**. `_depth` is still returned. (RN 4.7 breaking changes)
- The API is versioned through the Accept header (`AcceptHeaderVersioning`, `ALLOWED_VERSIONS=[current]`). Leave out `version=` in `Accept` to avoid a 406. (`settings.py` `REST_FRAMEWORK`)
- The interactive OpenAPI docs are at `/api/schema/swagger-ui/`, and the raw schema is at `/api/schema/`. The schema is generated per instance, including plugins, so it can be a source for the resource table. It had several fixes in 4.5 and 4.6 for bulk and `available-*` bodies, so older schemas are less reliable for those endpoints.
