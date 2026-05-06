## MODIFIED Requirements

### Requirement: Frontend architecture preservation
The frontend SHALL preserve the existing Vite, React Router, TanStack Query, Zustand, and FastAPI API boundary while using routed shadcn table controls backed by server-side list queries.

#### Scenario: API request from frontend
- **WHEN** the frontend calls an existing backend workflow API that is not a supported list endpoint
- **THEN** it uses the existing API contract without requiring unrelated backend route or response shape changes

#### Scenario: Server-side data table behavior
- **WHEN** a list route such as job history, users, credentials, command profiles, switches, imports, or audit logs renders
- **THEN** it requests paginated, filtered, and sorted data from the backend rather than loading an unbounded array and filtering in the browser
