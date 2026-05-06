# frontend-application-shell Specification

## Purpose
Define the routed authenticated frontend shell, client/server state boundaries, reusable UI primitives, and validated form behavior for the Bind Plane operator and admin workflows.
## Requirements
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

### Requirement: Query-managed server state
The frontend SHALL use a server-state layer for API reads, mutations, invalidation, and release job polling.

#### Scenario: Release job detail polling
- **WHEN** a release job detail route displays a non-terminal job
- **THEN** the frontend periodically refreshes that job until it reaches a terminal status

#### Scenario: Mutation refreshes dependent data
- **WHEN** a user creates, updates, confirms, retries, imports, or resets data through the frontend
- **THEN** the frontend invalidates or refreshes the dependent server-state queries

### Requirement: Client auth state boundary
The frontend SHALL keep client auth token state separate from server-owned user and workflow data.

#### Scenario: User logs in
- **WHEN** a user successfully logs in
- **THEN** the frontend stores the access token in client auth state and loads the current user through the API

#### Scenario: User logs out
- **WHEN** a user logs out
- **THEN** the frontend clears the token, clears cached server state, and returns to the login route

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

### Requirement: Validated frontend forms
The frontend SHALL validate required form fields before submitting workflow and admin mutations while still surfacing backend validation errors.

#### Scenario: Required login field missing
- **WHEN** a user submits the login form with missing required fields
- **THEN** the frontend displays validation feedback before sending the request

#### Scenario: Backend rejects submitted form
- **WHEN** the backend rejects a submitted form
- **THEN** the frontend displays the backend error message to the user

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
The frontend SHALL preserve the existing Vite, React Router, TanStack Query, Zustand, and FastAPI API boundary while using routed shadcn table controls backed by server-side list queries.

#### Scenario: API request from frontend
- **WHEN** the frontend calls an existing backend workflow API that is not a supported list endpoint
- **THEN** it uses the existing API contract without requiring unrelated backend route or response shape changes

#### Scenario: Server-side data table behavior
- **WHEN** a list route such as job history, users, credentials, command profiles, switches, imports, or audit logs renders
- **THEN** it requests paginated, filtered, and sorted data from the backend rather than loading an unbounded array and filtering in the browser

### Requirement: Client theme mode preference
The frontend SHALL support a client-side theme mode preference with `light`, `dark`, and `system` modes, defaulting to `system` and persisting the selected mode locally.

#### Scenario: Default theme follows system preference
- **WHEN** a user opens the frontend without a stored theme preference
- **THEN** the frontend uses `system` mode and applies the resolved browser color-scheme preference to the document root

#### Scenario: User selects dark mode
- **WHEN** a user selects `dark` theme mode
- **THEN** the frontend persists `dark` locally and applies the shadcn `.dark` class to the document root

#### Scenario: User selects light mode
- **WHEN** a user selects `light` theme mode
- **THEN** the frontend persists `light` locally and removes the shadcn `.dark` class from the document root

#### Scenario: System preference changes
- **WHEN** the selected theme mode is `system` and the browser color-scheme preference changes
- **THEN** the frontend updates the document root theme without requiring a page reload

### Requirement: Theme mode controls
The frontend SHALL expose the theme mode control on both unauthenticated and authenticated application surfaces using shadcn/ui primitives.

#### Scenario: Login page renders theme control
- **WHEN** a user opens `/login`
- **THEN** the login surface displays a theme mode control that can select light, dark, or system mode

#### Scenario: Authenticated shell renders theme control
- **WHEN** an authenticated user opens a protected application route
- **THEN** the authenticated shell header displays a theme mode control that can select light, dark, or system mode

#### Scenario: Theme control reuses global preference
- **WHEN** a user changes the theme mode from either the login page or authenticated shell
- **THEN** the selected preference is shared across frontend routes and remains active after reload

