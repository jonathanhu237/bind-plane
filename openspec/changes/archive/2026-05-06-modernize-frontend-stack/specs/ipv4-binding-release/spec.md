## ADDED Requirements

### Requirement: Routed release workflow parity
The frontend SHALL preserve the existing IPv4 binding release workflow behavior while exposing release, job history, and job detail screens through routed navigation.

#### Scenario: User confirms release from routed console
- **WHEN** an authenticated user prepares and confirms a release from `/release`
- **THEN** the frontend creates the same backend release job as the previous release console flow and navigates to that job's detail route

#### Scenario: User retries failed job from routed detail
- **WHEN** an authorized user retries a failed release job from `/jobs/:jobId`
- **THEN** the frontend creates a retry job and navigates to the new retry job detail route

### Requirement: Routed admin workflow parity
The frontend SHALL preserve existing admin management behavior while exposing admin sections through admin-only routed navigation.

#### Scenario: Admin resets user password
- **WHEN** an admin resets a user password from the routed user management page
- **THEN** the frontend calls the existing password reset API and refreshes the user data

#### Scenario: Admin creates or updates command profile
- **WHEN** an admin creates or updates a command profile from the routed command profile page
- **THEN** the frontend submits the existing command profile payload contract and refreshes the command profile list
