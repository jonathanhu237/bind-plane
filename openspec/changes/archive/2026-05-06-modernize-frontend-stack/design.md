## Context

The current frontend implements all authenticated navigation, API calls, polling, form state, admin views, and styling in `frontend/src/App.tsx` plus a single CSS file. This worked for the initial MVP but now makes it hard to reason about release workflow state, route access, admin-only surfaces, and server-data refresh behavior.

The backend API, worker, database model, and OpenSpec IPv4 binding semantics are already implemented and must remain stable. This change modernizes only the frontend application architecture while preserving the current MVP behavior.

## Goals / Non-Goals

**Goals:**
- Use real routes for login, release console, job history, job detail, and admin pages.
- Use TanStack Query for server state, mutations, invalidation, and job polling.
- Use Zustand only for lightweight client auth/session state.
- Use Tailwind CSS and shadcn/ui primitives for reusable, restrained UI building blocks.
- Use react-hook-form and zod for form state and validation.
- Split frontend code into feature, API, store, route, and shared UI modules.
- Preserve current backend API contracts and user-visible workflow semantics.
- Keep frontend tests focused on route behavior and existing workflow coverage.

**Non-Goals:**
- No backend endpoint changes.
- No database or worker changes.
- No new release workflow capabilities.
- No SSO, public registration, approval flow, batch release, IPv6/NDP, or binding creation.
- No broad visual rebrand or landing page.
- No MSW test dependency in this change.

## Decisions

### Decision: Route Objects With Browser History
Use React Router route objects with browser history. This replaces the current local `view` state and gives job details and admin pages stable URLs. Route guards will enforce authentication and admin access before rendering protected pages.

Alternative considered: keep view state and only refactor data fetching. That would leave refresh/share/back-button behavior unresolved and keep navigation coupled to the app shell.

### Decision: Query Owns Server State
Use TanStack Query for all API reads and mutations. Job detail and pre-release query polling will use query `refetchInterval` while the job is non-terminal. List and admin views will refresh on navigation, manual actions, or mutation invalidation instead of constant polling.

Alternative considered: put server data in Zustand. That would recreate cache invalidation, loading, retry, and polling behavior manually.

### Decision: Zustand Only Owns Client Auth State
Use Zustand for token persistence and session actions. The current user remains server state loaded through `/auth/me`, so role changes and inactive sessions are not duplicated into a client store.

Alternative considered: store user and all navigation state in Zustand. That would make role-sensitive data easier to stale and would blur server/client state boundaries.

### Decision: shadcn/ui With Tailwind
Use Tailwind CSS and shadcn-style local UI components under `frontend/src/components/ui`. Add only the components required by the current workflows: buttons, inputs, labels, textareas, selects, checkboxes, badges, tables, cards, tabs, dialogs, alerts, separators, dropdown menus, and form wrappers.

Alternative considered: keep custom CSS. That would not meet the desired frontend stack and would keep presentation patterns inconsistent as the app grows.

### Decision: Forms Use react-hook-form and zod
Use zod schemas and react-hook-form for release, login, admin user, credential, import, and command profile forms. Backend validation remains authoritative; frontend validation catches obvious missing or malformed fields before request submission.

Alternative considered: keep controlled form state. That is workable for small forms but does not scale cleanly to command profile JSON fields and admin forms.

### Decision: Preserve Fetch Mock Tests
Keep fetch mocks in frontend tests instead of adding MSW. Tests will gain provider/router/query wrappers and be split by feature where useful.

Alternative considered: introduce MSW. It is a good long-term option, but this change already adds several frontend foundations and should keep test dependencies bounded.

## Risks / Trade-offs

- [Risk] Large frontend file split can accidentally drop workflow behavior. → Mitigate by preserving existing test scenarios and adding route/auth-specific tests before archive.
- [Risk] Query cache invalidation can show stale job/admin data. → Mitigate with explicit query keys, mutation invalidation, and polling only for non-terminal job states.
- [Risk] shadcn/Tailwind migration can over-expand the visual scope. → Mitigate by using a restrained operator UI and avoiding decorative/marketing patterns.
- [Risk] form schemas can diverge from backend validation. → Mitigate by keeping schemas focused on frontend-required fields and letting backend errors still surface.
- [Risk] dependency additions increase bundle and maintenance surface. → Mitigate by adding only dependencies directly used by this migration.

## Migration Plan

1. Create the frontend foundation: Tailwind, path alias, shadcn utilities, QueryClient, auth store, router, and route guards.
2. Move shared API types and request helper out of `App.tsx`.
3. Migrate login and authenticated shell.
4. Migrate release console, pre-query polling, job detail, retry navigation, and job history.
5. Migrate admin pages and audit view.
6. Replace the old single-file app with provider/router entry points.
7. Update tests around providers, routes, fetch mocks, and retained workflows.
8. Run `make test`, `make lint`, and OpenSpec verification before archive.

Rollback strategy: revert the frontend migration commit/change branch; backend API contracts are unchanged.

## Open Questions

No open product questions remain. The project owner accepted the stack, scope, route model, state split, polling strategy, shadcn component scope, testing approach, and archive/commit/push workflow.
