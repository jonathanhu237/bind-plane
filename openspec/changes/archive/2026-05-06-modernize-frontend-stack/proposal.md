## Why

The current frontend is a single-file React application that mixes routing, auth state, API fetching, polling, forms, and presentation in `App.tsx`. Bind Plane now has enough operator and admin surface area that the frontend needs a maintainable application structure before further workflow work is added.

## What Changes

- Replace the local `view` state navigation with React Router route objects and real URLs for release, job history, job detail, and admin sections.
- Introduce TanStack Query for server-state reads, mutations, cache invalidation, and release job polling.
- Introduce Zustand for lightweight client auth state, limited to token/session client state.
- Introduce Tailwind CSS and shadcn/ui primitives for a restrained operator-focused UI system.
- Introduce react-hook-form and zod for form state and validation on release/admin forms.
- Split the current single-file frontend into feature modules, API/query helpers, shared UI components, and route guards.
- Preserve the existing backend API contract and current MVP workflow behavior.
- Update frontend tests to cover route guards, release workflow behavior, retry navigation, admin access, user password reset, and command profile create/update.

## Capabilities

### New Capabilities
- `frontend-application-shell`: Routed authenticated frontend shell, server-state management, form handling, and reusable UI primitives for the existing Bind Plane workflows.

### Modified Capabilities
- `ipv4-binding-release`: Clarify that frontend release, job, and admin workflows remain behaviorally equivalent while using routed navigation and managed polling.

## Impact

- Affected code: `frontend/package.json`, Vite/Tailwind/TypeScript config, `frontend/src/**`, and frontend tests.
- New frontend dependencies: React Router, TanStack Query, Zustand, Tailwind CSS, shadcn/Radix primitives, class utilities, react-hook-form, zod, and hookform resolvers.
- Backend APIs, database schema, worker behavior, and OpenSpec domain semantics remain unchanged.
- Existing `make test` and `make lint` continue to be the project verification entry points.
