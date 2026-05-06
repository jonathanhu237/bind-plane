## Context

The current frontend already uses React Router, TanStack Query, Zustand, Tailwind, Radix primitives, and local `components/ui/*` files. However, those UI primitives were mostly hand-written shadcn-compatible approximations rather than registry-managed shadcn/ui components. The application shell also uses a custom sidebar instead of shadcn's official Sidebar component, and the login page does not follow a shadcn auth/login block visual baseline.

The project owner wants shadcn/ui to be the actual UI component source of truth: if shadcn provides the component, Bind Plane should use the official component instead of maintaining a custom equivalent.

## Goals / Non-Goals

**Goals:**
- Add standard shadcn `components.json` for the existing Vite + React + TypeScript app.
- Use shadcn `new-york` style with `neutral` base color and default CSS variable tokens.
- Replace existing local primitives with official shadcn/ui registry components where available.
- Add shadcn Sidebar and rebuild the authenticated shell using shadcn sidebar primitives.
- Rework the login page toward shadcn auth/login block styling.
- Preserve current React Router routes, TanStack Query behavior, Zustand auth token state, and backend API contracts.
- Keep operator/admin pages usable while moving their base controls to official components.

**Non-Goals:**
- Do not migrate to Next.js.
- Do not change backend APIs, database schema, worker behavior, auth semantics, or deployment ports.
- Do not implement server-side pagination/filtering/sorting or shadcn Data Table in this change.
- Do not redesign product workflows beyond shell/login/component standardization.
- Do not add marketing/landing pages.

## Decisions

### Decision: Standard shadcn configuration
Add `components.json` using `style: "new-york"`, `baseColor: "neutral"`, CSS variables, and the existing `@/*` path alias. This makes the project compatible with `shadcn` CLI/registry commands and gives future agents one clear source for component generation.

Alternative considered: continue hand-maintaining shadcn-like primitives. That caused the current mismatch between expected and actual shadcn behavior.

### Decision: Official component first
When shadcn/ui provides a component that matches a foundation need, use the official registry implementation. Bind Plane should only maintain local business composition components such as `AppSidebar`, `ReleaseConsole`, `JobDetailPage`, and admin pages.

Components expected in scope include at least:
- `button`
- `card`
- `input`
- `textarea`
- `label`
- `badge`
- `alert`
- `table`
- `checkbox`
- `dropdown-menu`
- `dialog`
- `separator`
- `tabs`
- `skeleton`
- `tooltip`
- `sheet`
- `collapsible`
- `avatar`
- `sidebar`

### Decision: Sidebar component, not just a block
Use shadcn's official Sidebar component as the layout foundation. Create a Bind Plane `AppSidebar` composition around shadcn primitives and render main routes in `SidebarInset`.

Alternative considered: using a dashboard block wholesale. Blocks are useful visual references, but the reusable Sidebar component better fits the existing React Router shell.

### Decision: Auth block visual baseline
Rework `LoginPage` toward the shadcn auth/login block style while keeping the existing API calls, Zustand token storage, query invalidation, backend error display, and route redirect behavior.

### Decision: Keep frontend/backend separation
Retain Vite dev server, `/api` proxy, React Router, FastAPI backend, and existing API types. shadcn does not require Next.js, and migrating framework would add unrelated risk.

### Decision: Data tables are separate
Do not introduce server-side Data Table behavior in this change. That is explicitly deferred to `add-server-side-data-tables`, which can modify list API query parameters and response shapes with its own OpenSpec.

## Risks / Trade-offs

- [Risk] shadcn registry output may differ from current hand-written component APIs. -> Update call sites deliberately and keep tests around login, navigation, release preparation, retry, and admin actions.
- [Risk] Sidebar dependencies can expand frontend package surface. -> Add only official components required by the shell/login/current pages.
- [Risk] Visual changes can disrupt dense operator workflows. -> Use shadcn dashboard/auth baseline while keeping workflow layout restrained and task-focused.
- [Risk] Component overwrite can lose local tweaks. -> Treat current primitives as temporary; preserve behavior in feature components and tests, not in hand-written primitive internals.

## Migration Plan

1. Add shadcn config and install/generate official components needed by the shell, login page, and existing forms/tables.
2. Replace global CSS variables with shadcn `new-york`/`neutral` tokens and verify Tailwind still builds.
3. Rebuild the authenticated layout with `SidebarProvider`, `AppSidebar`, `SidebarInset`, and `SidebarTrigger`.
4. Rework login page to the shadcn auth block visual baseline.
5. Update affected feature components to match official component APIs.
6. Update tests for login, protected routes, admin route access, release workflow, retry navigation, and admin forms.
7. Run frontend tests/lint/build, project tests/lint, OpenSpec validation, and required subagent verification.

Rollback strategy: revert the change branch; backend contracts and data remain unchanged.

## Open Questions

No open product questions remain for this phase. The project owner selected official shadcn components, allowed replacement of current UI primitives, selected `new-york`/`neutral`, retained Vite + React Router + FastAPI separation, included login page standardization, and deferred server-side Data Tables to a later change.
