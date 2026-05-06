## 1. Frontend Foundation

- [x] 1.1 Add React Router, TanStack Query, Zustand, Tailwind, shadcn support libraries, react-hook-form, zod, and resolver dependencies
- [x] 1.2 Configure Tailwind, path aliases, shadcn utilities, and global stylesheet tokens
- [x] 1.3 Create shared API types, request helpers, query client, auth store, and route guard foundations
- [x] 1.4 Add reusable shadcn-style UI primitives needed by current workflows

## 2. Routes and Application Shell

- [x] 2.1 Replace local view navigation with React Router route objects and protected routes
- [x] 2.2 Implement login route, authenticated shell, sidebar navigation, logout behavior, and admin-only 403 handling
- [x] 2.3 Preserve current-user loading via `/auth/me` and query-cache clearing on logout

## 3. Release and Job Workflows

- [x] 3.1 Migrate release console to feature modules using react-hook-form, zod, and TanStack Query mutations
- [x] 3.2 Preserve pre-release query polling and forced-release confirmation behavior
- [x] 3.3 Migrate job history and job detail routes with non-terminal job polling
- [x] 3.4 Preserve retry behavior by navigating to the new retry job detail route

## 4. Admin Workflows

- [x] 4.1 Migrate user management with create and password reset flows
- [x] 4.2 Migrate credential management
- [x] 4.3 Migrate switch/network import management
- [x] 4.4 Migrate command profile create/update management with full profile fields
- [x] 4.5 Migrate admin audit review

## 5. Verification

- [x] 5.1 Update frontend tests for router/query providers, auth guards, release workflow, retry navigation, admin user reset, and command profile create/update
- [x] 5.2 Run frontend tests and lint
- [x] 5.3 Run project test suite and OpenSpec validation
