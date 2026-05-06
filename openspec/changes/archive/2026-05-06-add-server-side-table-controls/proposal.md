## Why

Bind Plane's list pages currently load entire collections and render static tables, which is acceptable for seed data but not for real job history, audit review, imports, and admin-managed records. Now that the frontend stack is routed and shadcn-based, the next missing production behavior is server-side pagination, filtering, and sorting for operational tables.

## What Changes

- Add a shared backend list-query contract for supported table endpoints using page number, page size, search/filter parameters, and allowlisted sort fields.
- Return paginated list envelopes with `items`, `total`, `page`, `page_size`, and page-count metadata for supported list endpoints.
- Apply the contract to job history, audit logs, users, credentials, switches, import batches, and command profiles.
- Update frontend list pages to keep table controls in URL/query state and TanStack Query keys, then request filtered/sorted/paginated data from the backend.
- Add reusable shadcn-based table controls for search/filter input, page-size selection, pagination, empty/loading states, and sortable headers.
- Preserve existing create/update/import/retry workflows and invalidate the relevant paginated queries after mutations.
- Keep the release console's forced switch selector simple; it may request enough enabled switches for selection but does not become a full table in this change.

## Capabilities

### New Capabilities

- `server-side-table-controls`: Backend and frontend behavior for paginated, filterable, sortable operational/admin list surfaces.

### Modified Capabilities

- `frontend-application-shell`: Replace the previous no-server-side-table-controls constraint with routed shadcn table controls backed by server-side list queries.

## Impact

- Affected backend code: list route query parameters and response schemas for release jobs, audit logs, users, credentials, switches, import batches, and command profiles.
- Affected frontend code: API types/hooks, job history, admin list pages, reusable table components, tests, and route/query-state handling.
- API compatibility: list endpoints will return paginated envelopes instead of bare arrays for frontend-managed list pages. Non-table helper reads may either keep simple arrays or explicitly request a large first page where appropriate.
- Dependencies: may add TanStack Table only if needed for column metadata; otherwise use existing TanStack Query, React Router, and shadcn primitives.
- Verification: backend API tests, frontend workflow/table tests, lint, project tests, and OpenSpec validation.
