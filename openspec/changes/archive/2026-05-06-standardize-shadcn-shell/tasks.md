## 1. shadcn Foundation

- [x] 1.1 Add standard `components.json` for Vite/React with `new-york` style, `neutral` base color, CSS variables, and existing `@/*` alias
- [x] 1.2 Install/generate official shadcn/ui components needed for shell, login, forms, tables, and current admin/release workflows
- [x] 1.3 Replace global CSS variables with shadcn `new-york`/`neutral` token baseline and verify Tailwind builds

## 2. Official Primitive Replacement

- [x] 2.1 Replace hand-maintained `frontend/src/components/ui/*` primitives with official shadcn/ui implementations where available
- [x] 2.2 Update existing feature call sites for official component APIs without changing backend API contracts
- [x] 2.3 Preserve local code only for business composition components rather than shadcn-provided primitives

## 3. Sidebar Application Shell

- [x] 3.1 Add shadcn Sidebar dependencies and primitives
- [x] 3.2 Rebuild the authenticated shell with `SidebarProvider`, `AppSidebar`, `SidebarInset`, `SidebarTrigger`, and shadcn navigation/menu primitives
- [x] 3.3 Preserve protected routing, admin-only access denied behavior, logout, and current-user loading

## 4. Login Surface

- [x] 4.1 Rework `LoginPage` toward a shadcn auth/login block visual baseline
- [x] 4.2 Preserve login mutation, token storage, current user loading, backend error display, and redirect behavior

## 5. Verification

- [x] 5.1 Update frontend tests for shadcn shell navigation, login behavior, route guards, release workflow, retry navigation, and admin forms
- [x] 5.2 Run frontend tests, frontend lint, and frontend production build
- [x] 5.3 Run project test suite, project lint, and OpenSpec validation
- [x] 5.4 Verify in browser screenshots that `/login` and authenticated shell render with shadcn styling instead of browser-default or hand-written shell styling
