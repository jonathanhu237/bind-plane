## Why

The current frontend uses hand-written shadcn-compatible primitives and a custom shell, which does not match the project owner's expectation of a standard shadcn/ui application. The frontend should use official shadcn/ui components wherever available so the UI has a recognizable shadcn dashboard/auth baseline and the project stops maintaining homegrown equivalents.

## What Changes

- Add standard shadcn configuration for the Vite/React frontend (`components.json`) using `new-york` style and `neutral` base color.
- Replace hand-maintained UI primitives with official shadcn/ui registry components where those components exist.
- Add official shadcn Sidebar component and rewrite the authenticated application shell around `SidebarProvider`, `Sidebar`, `SidebarInset`, `SidebarTrigger`, and related sidebar primitives.
- Rewrite the login page using shadcn auth/login block styling while preserving the existing login API and auth state behavior.
- Update global CSS variables to the shadcn `new-york`/`neutral` token baseline.
- Preserve the current Vite + React Router + TanStack Query + Zustand + FastAPI architecture and existing backend API contract.
- Leave server-side data table pagination/filter/sort for a separate later change.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `frontend-application-shell`: Standardize the shell, login surface, and reusable UI primitives on official shadcn/ui components while preserving existing route/auth/server-state behavior.

## Impact

- Affected frontend code: shadcn config, Tailwind/global CSS tokens, `frontend/src/components/ui/**`, `features/layout/AppShell.tsx`, `features/auth/LoginPage.tsx`, and tests that assert shell/login behavior.
- New frontend dependencies may include shadcn component requirements such as tooltip, collapsible, avatar, sheet/drawer-related Radix packages, and command-style helpers only when used by selected official components.
- No backend API, worker, database, auth semantics, or server-side list query changes are in scope.
