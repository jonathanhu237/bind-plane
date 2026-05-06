## MODIFIED Requirements

### Requirement: Routed authenticated frontend shell
The frontend SHALL provide routed authenticated navigation for the existing Bind Plane release, job, and admin workflows using shadcn/ui Sidebar primitives for the authenticated application shell.

#### Scenario: Authenticated user opens release route
- **WHEN** an authenticated operator or admin opens `/release`
- **THEN** the frontend displays the release console inside a shadcn Sidebar-based authenticated application shell

#### Scenario: Unauthenticated user opens protected route
- **WHEN** an unauthenticated user opens a protected route
- **THEN** the frontend redirects the user to `/login`

#### Scenario: Admin route opened by operator
- **WHEN** an authenticated operator opens an admin-only route
- **THEN** the frontend displays an access-denied view instead of the admin page

### Requirement: Reusable operator-focused UI primitives
The frontend SHALL use official shadcn/ui registry components for shared UI primitives when shadcn provides an equivalent component, while preserving a restrained operator-focused interface.

#### Scenario: Admin form rendered
- **WHEN** an admin opens a management page
- **THEN** the frontend renders forms and tables using official shadcn/ui shared primitives where available

#### Scenario: Release confirmation rendered
- **WHEN** a release preparation is ready for confirmation
- **THEN** the frontend displays target IP, switch, observed state, reason, and force state using official shadcn/ui shared primitives where available

#### Scenario: shadcn component exists
- **WHEN** the frontend needs a reusable primitive such as button, card, input, dialog, dropdown menu, tooltip, sheet, or sidebar
- **THEN** the implementation uses the official shadcn/ui component instead of a hand-maintained equivalent

## ADDED Requirements

### Requirement: shadcn visual baseline
The frontend SHALL use the standard shadcn/ui `new-york` style with `neutral` base color for global UI tokens.

#### Scenario: Application renders shared surfaces
- **WHEN** login, shell, release, job, or admin surfaces render
- **THEN** their colors, borders, radius, and typography derive from the shadcn `new-york`/`neutral` CSS variable baseline

### Requirement: shadcn login surface
The frontend SHALL render the login route using a shadcn auth/login block visual baseline while preserving existing authentication behavior.

#### Scenario: User logs in
- **WHEN** a user successfully logs in from `/login`
- **THEN** the frontend stores the access token in client auth state, loads the current user through the API, and navigates to the authenticated shell

#### Scenario: Backend rejects login
- **WHEN** the backend rejects submitted login credentials
- **THEN** the frontend displays the backend error message inside the shadcn-styled login surface

### Requirement: Frontend architecture preservation
The frontend SHALL preserve the existing Vite, React Router, TanStack Query, Zustand, and FastAPI API boundary while standardizing shadcn UI components.

#### Scenario: API request from frontend
- **WHEN** the frontend calls an existing backend workflow API
- **THEN** it uses the existing API contract without requiring backend route or response shape changes

#### Scenario: Server-side data table behavior
- **WHEN** a list route such as job history, users, credentials, command profiles, switches, or audit logs renders in this change
- **THEN** it preserves the current list API contract without adding server-side pagination, filtering, or sorting
