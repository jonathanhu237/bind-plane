## Context

Bind Plane now has routed React pages, TanStack Query, shadcn tables, and admin/operator list surfaces, but those pages still call list APIs that return bare arrays. That forces the browser to load all jobs, audit logs, switches, imports, users, credentials, and command profiles before the user can narrow or order data.

The backend already owns the correct authorization boundary for each list endpoint, so pagination, filtering, and sorting should happen there instead of in frontend-only table state. The frontend should remain a thin operational UI that reflects backend totals and stores table state in route query parameters.

## Goals / Non-Goals

**Goals:**

- Add a consistent paginated response shape for supported operational/admin list endpoints.
- Support page number, page size, search, endpoint-specific filters, and allowlisted sorting on the backend.
- Keep non-admin job history scoped to the current operator before pagination and totals are calculated.
- Drive frontend table controls from URL query parameters and TanStack Query keys.
- Use shadcn primitives for controls, loading/empty states, and pagination.
- Preserve existing mutation workflows by invalidating/refetching affected paginated lists.

**Non-Goals:**

- No server-side cursor pagination; page-number pagination is sufficient for the MVP.
- No saved views, column visibility persistence, bulk actions, exports, or advanced query language.
- No backend schema migration is expected.
- No full TanStack Table adoption unless the implementation needs column metadata beyond simple sortable headers.
- The release console forced-switch selector remains a selector, not a full table.

## Decisions

### Decision: Use offset pagination with a shared envelope

List endpoints will accept `page` and `page_size`, calculate `total`, and return `items`, `total`, `page`, `page_size`, and `page_count`.

Rationale: It is simple, testable, and fits admin/operator lists where users mostly inspect recent jobs, audit rows, and named admin records. Cursor pagination would make arbitrary sorting and page-number controls more complex without enough benefit for the MVP.

Alternative considered: keep legacy arrays and add headers for totals. That is harder for the frontend to type and cache consistently, and it keeps pagination metadata outside the JSON API contract.

### Decision: Allowlist sorting per endpoint

Each endpoint will map allowed `sort_by` values to SQLAlchemy columns or expressions and reject unknown sort keys. `sort_order` will be `asc` or `desc`.

Rationale: It prevents arbitrary column access or SQL injection and keeps the UI aligned with indexed or cheap sort fields.

Alternative considered: pass raw column names through. That is unsafe and creates an unstable API surface.

### Decision: Keep filters endpoint-specific but consistently named

All supported endpoints may use `search`. Each endpoint may add constrained filters such as `status`, `kind`, `force`, `role`, `is_active`, `is_enabled`, `action`, `target_type`, and `import_status`.

Rationale: A shared query envelope is useful, but each table has different domain filters. Keeping filters explicit makes validation and tests clearer.

Alternative considered: a generic `filters` JSON parameter. That would be less ergonomic in URLs and harder to validate with FastAPI.

### Decision: Frontend table state lives in URL search params

List pages will read and write page, page size, search, filters, and sorting through React Router search params. TanStack Query keys will include the normalized table params.

Rationale: URLs become shareable and browser back/forward behavior works naturally. TanStack Query will fetch the correct page whenever table controls change.

Alternative considered: local component state. That is simpler initially but loses deep-linking and tends to desynchronize with navigation.

### Decision: Preserve helper reads with explicit page sizes

Pages that need data for selectors, such as the forced-switch selector, will request the first page with an explicit larger `page_size` and supported filters instead of relying on legacy array endpoints.

Rationale: The list API contract remains consistent while avoiding a second legacy endpoint for the same resource.

Alternative considered: add separate `/options` endpoints. That may be useful later, but it is unnecessary for current MVP data sizes.

## Risks / Trade-offs

- [Risk] Changing list responses from arrays to envelopes can break frontend call sites that were not updated. -> Mitigation: update shared API types/hooks and add frontend tests for all list pages touched by the change.
- [Risk] Counting totals adds extra database work on large audit/job tables. -> Mitigation: keep filters simple, calculate counts after authorization/filter predicates, and restrict page size.
- [Risk] Searching JSON payloads portably across SQLite/PostgreSQL is limited. -> Mitigation: audit search will cover stable scalar fields such as action and target type; payload remains displayed but not a primary search target for MVP.
- [Risk] Joined eager loads can inflate counts for switches with networks. -> Mitigation: count against the base entity select and use loader options only for the paginated item query.
