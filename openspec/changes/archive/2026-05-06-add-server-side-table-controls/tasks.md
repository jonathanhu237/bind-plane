## 1. Backend List Contract

- [x] 1.1 Add shared pagination/list-query schemas and helper utilities for page metadata, allowlisted sorting, and filtered count queries
- [x] 1.2 Update release job history to support paginated envelope responses, operator/admin authorization-aware totals, search, status/kind/force filters, and allowlisted sorting
- [x] 1.3 Update audit log listing to support paginated envelope responses, search/action/target filters, and allowlisted sorting
- [x] 1.4 Update admin user, credential, switch, import batch, and command-profile list endpoints to support paginated envelope responses, endpoint filters, search, and allowlisted sorting

## 2. Frontend Table Infrastructure

- [x] 2.1 Add shared frontend pagination types, query-string builders, and URL-backed table-state helpers
- [x] 2.2 Add reusable shadcn table controls for search, filters, sortable headers, page-size selection, pagination buttons, loading, and empty states
- [x] 2.3 Update query keys and API hooks/call sites to use paginated list envelopes and invalidate paginated query families after mutations

## 3. Frontend List Pages

- [x] 3.1 Update job history to use server-side pagination, search, filters, and sorting while preserving row navigation and retry refresh behavior
- [x] 3.2 Update user, credential, command profile, switch, and import admin pages to use server-side table controls
- [x] 3.3 Update audit log page to use server-side pagination, search, filters, and sorting
- [x] 3.4 Preserve release console forced-switch selection by requesting explicit paginated switch options with enabled filtering

## 4. Verification

- [x] 4.1 Add or update backend API tests for pagination metadata, authorization-scoped totals, sorting validation, and representative filters
- [x] 4.2 Add or update frontend tests for URL-backed table controls, paginated fetches, filter reset behavior, and mutation invalidation behavior
- [x] 4.3 Run frontend lint/tests/build, backend tests, project lint/tests, and OpenSpec validation
