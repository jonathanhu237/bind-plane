# ipv4-binding-release Specification

## Purpose
Define the end-to-end Bind Plane workflow for releasing a single IPv4 static IP-MAC binding through authorized users, switch resolution, command-profile-driven worker execution, result classification, retry behavior, and audit review.
## Requirements
### Requirement: Authorized users can access the release workflow
The system SHALL allow only authenticated users with the `operator` or `admin` role to access the IPv4 binding release workflow.

#### Scenario: Authorized operator opens release console
- **WHEN** an authenticated `operator` opens the release console
- **THEN** the system displays the single-IP release workflow

#### Scenario: Unauthenticated user opens release console
- **WHEN** an unauthenticated user opens the release console
- **THEN** the system denies access and requires login

### Requirement: Admin-created accounts
The system SHALL bootstrap the first `admin` account from startup configuration when no admin exists, SHALL require later user accounts to be created by an `admin`, and MUST NOT provide public registration or account creation CLI paths.

#### Scenario: Initial admin is bootstrapped
- **WHEN** the system starts for the first time with valid initial admin configuration and no existing `admin` user
- **THEN** the system creates the first `admin` account from startup configuration

#### Scenario: Admin creates operator account
- **WHEN** an `admin` creates a user with the `operator` role
- **THEN** the system stores the account and permits that user to authenticate

#### Scenario: Public registration attempted
- **WHEN** a user attempts to register without admin action
- **THEN** the system provides no public registration path

#### Scenario: Account creation CLI attempted
- **WHEN** an operator attempts to use an installed Bind Plane account creation CLI
- **THEN** the system provides no account creation CLI path

### Requirement: Single IPv4 input
The system SHALL accept exactly one valid IPv4 address as the release target.

#### Scenario: Valid IPv4 submitted
- **WHEN** an authorized user submits one syntactically valid IPv4 address
- **THEN** the system begins release preparation for that address

#### Scenario: Invalid target submitted
- **WHEN** an authorized user submits an invalid IP address, IPv6 address, or multiple addresses
- **THEN** the system rejects the request before switch resolution

### Requirement: Switch resolution from validated network data
The system SHALL resolve the responsible switch from enabled and validated local switch/network records.

#### Scenario: One switch matches target IP
- **WHEN** the target IPv4 address matches one enabled validated network record
- **THEN** the system selects that network's responsible switch

#### Scenario: No switch matches target IP
- **WHEN** the target IPv4 address matches no enabled validated network record
- **THEN** the system stops the normal release workflow

#### Scenario: Ambiguous switch match
- **WHEN** the target IPv4 address matches multiple equally specific enabled validated network records
- **THEN** the system stops the normal release workflow and requires admin data correction

### Requirement: Longest-prefix switch matching
The system SHALL use the most specific enabled validated network match when overlapping networks match the target IPv4 address.

#### Scenario: More specific network exists
- **WHEN** the target IPv4 address matches both a broader network and a more specific network
- **THEN** the system resolves the switch from the more specific network

### Requirement: Pre-release query
The system SHALL query the resolved switch for the target IPv4 state before creating a normal release job.

#### Scenario: Static binding found before release
- **WHEN** the pre-release query finds a static binding for the target IPv4 address
- **THEN** the system allows the user to continue to confirmation

#### Scenario: Dynamic entry found before release
- **WHEN** the pre-release query finds only a dynamic ARP entry for the target IPv4 address
- **THEN** the system allows the user to continue to confirmation and preserves the observed state

#### Scenario: No record found before release
- **WHEN** the pre-release query finds no record for the target IPv4 address
- **THEN** the system stops the normal operator release workflow

### Requirement: Forced release by admin
The system SHALL allow only `admin` users to force release when the pre-release query finds no record, and only after a switch is resolved or explicitly selected.

#### Scenario: Admin forces release after no record
- **WHEN** an `admin` chooses forced release after no pre-release record is found and a switch is available
- **THEN** the system allows confirmation and marks the job with `force = true`

#### Scenario: Operator attempts forced release
- **WHEN** an `operator` attempts forced release after no pre-release record is found
- **THEN** the system denies forced release

### Requirement: Reason and confirmation
The system SHALL require a reason selection and explicit confirmation before creating a release job.

#### Scenario: User confirms with reason
- **WHEN** an authorized user selects a reason and confirms the target IP, switch, observed state, and reason
- **THEN** the system creates a release job

#### Scenario: Reason missing
- **WHEN** an authorized user attempts to confirm without a reason
- **THEN** the system refuses to create a release job

### Requirement: Background release job
The system SHALL execute switch release commands in a background worker rather than in the HTTP request that creates the job.

#### Scenario: Release job created
- **WHEN** an authorized user confirms a release
- **THEN** the system creates a queued job and returns a job identifier

#### Scenario: Worker processes job
- **WHEN** a worker receives a queued release job
- **THEN** the worker connects to the switch, executes the command profile steps, stores outputs, and updates job status

### Requirement: Command profile driven execution
The system SHALL use the resolved switch's command profile for query commands, release commands, prompt handling, pagination handling, success patterns, error patterns, and output parsing.

#### Scenario: Worker executes command profile
- **WHEN** the worker processes a release job
- **THEN** the worker uses the switch's assigned command profile instead of hard-coded vendor commands

#### Scenario: Command profile missing
- **WHEN** the resolved switch has no usable command profile
- **THEN** the system marks release preparation or job execution as failed before sending undefined commands

### Requirement: Credential isolation
The system SHALL store switch credentials separately from switch records, encrypt credentials at rest, and decrypt them only during worker execution.

#### Scenario: Worker connects to switch
- **WHEN** the worker needs to connect to a switch
- **THEN** the worker decrypts the referenced credential for that execution and does not expose plaintext credentials in logs or API responses

### Requirement: Result classification
The system SHALL classify release results based on command errors and post-release query state.

#### Scenario: Static binding removed
- **WHEN** the release command returns no error and the post-release query shows no static binding for the target IPv4 address
- **THEN** the system marks the release as successful

#### Scenario: Dynamic entry remains
- **WHEN** the release command returns no error and the post-release query shows only a dynamic ARP entry for the target IPv4 address
- **THEN** the system marks the static binding release as successful and reports the dynamic entry

#### Scenario: Static binding remains
- **WHEN** the post-release query still shows a static binding for the target IPv4 address
- **THEN** the system marks the release as failed or requiring manual confirmation

#### Scenario: Output cannot be parsed
- **WHEN** the worker cannot reliably parse the command output or post-release state
- **THEN** the system stores raw output and marks the result as `needs_manual_confirmation`

### Requirement: Job status visibility
The system SHALL expose release job status and phase to authorized users.

#### Scenario: User views own job
- **WHEN** an `operator` requests the status of a release job they created
- **THEN** the system returns the current status, phase, structured result, and permitted output

#### Scenario: Admin views any job
- **WHEN** an `admin` requests the status of any release job
- **THEN** the system returns the job status, phase, structured result, and stored troubleshooting output

### Requirement: Retry creates new job
The system SHALL create a new release job when retrying a failed release and link it to the original job.

#### Scenario: Failed job retried
- **WHEN** an authorized user retries a failed release job
- **THEN** the system creates a new job that references the original job and preserves the original job record

### Requirement: Audit trail
The system SHALL record an audit trail for every release job and every command attempt.

#### Scenario: Release job finishes
- **WHEN** a release job reaches a terminal status
- **THEN** the system records the operator, target IP, switch, command profile, reason, force flag, before state, after state, raw output, status, and timestamps

#### Scenario: Forced release audited
- **WHEN** a forced release job is recorded
- **THEN** the audit trail includes `force = true` and the selected reason

### Requirement: Minimal admin data management
The system SHALL provide admin-only paths to manage users, credentials, switch/network imports, command profiles, and audit review needed for the MVP workflow.

#### Scenario: Operator accesses admin management
- **WHEN** an `operator` attempts to access admin data management
- **THEN** the system denies access

#### Scenario: Admin updates command profile
- **WHEN** an `admin` updates a command profile used by switches
- **THEN** the system persists the change for future release preparation and job execution

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
