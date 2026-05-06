## ADDED Requirements

### Requirement: Routed authenticated frontend shell
The frontend SHALL provide routed authenticated navigation for the existing Bind Plane release, job, and admin workflows.

#### Scenario: Authenticated user opens release route
- **WHEN** an authenticated operator or admin opens `/release`
- **THEN** the frontend displays the release console inside the authenticated application shell

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
The frontend SHALL use reusable UI primitives for forms, controls, tables, status indicators, dialogs, and panels while preserving a restrained operator-focused interface.

#### Scenario: Admin form rendered
- **WHEN** an admin opens a management page
- **THEN** the frontend renders forms and tables using the shared UI primitives

#### Scenario: Release confirmation rendered
- **WHEN** a release preparation is ready for confirmation
- **THEN** the frontend displays target IP, switch, observed state, reason, and force state using the shared UI primitives

### Requirement: Validated frontend forms
The frontend SHALL validate required form fields before submitting workflow and admin mutations while still surfacing backend validation errors.

#### Scenario: Required login field missing
- **WHEN** a user submits the login form with missing required fields
- **THEN** the frontend displays validation feedback before sending the request

#### Scenario: Backend rejects submitted form
- **WHEN** the backend rejects a submitted form
- **THEN** the frontend displays the backend error message to the user
