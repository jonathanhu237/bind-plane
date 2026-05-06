## ADDED Requirements

### Requirement: Paginated list response contract
The system SHALL return supported operational and admin list endpoints as paginated response envelopes containing `items`, `total`, `page`, `page_size`, and `page_count`.

#### Scenario: Default paginated request
- **WHEN** a client requests a supported list endpoint without pagination query parameters
- **THEN** the API returns the first page using the endpoint default page size and includes the total number of records visible to the current user

#### Scenario: Specific page requested
- **WHEN** a client requests a supported list endpoint with `page` and `page_size`
- **THEN** the API returns only records for that page and includes pagination metadata for the complete filtered result set

#### Scenario: Page outside result range
- **WHEN** a client requests a page beyond the available filtered result set
- **THEN** the API returns an empty `items` list and still returns the accurate total and page metadata

### Requirement: Authorized totals before pagination
The system SHALL apply authorization filters before calculating totals and returning paginated list items.

#### Scenario: Operator lists release jobs
- **WHEN** an operator requests the release job history endpoint
- **THEN** the API returns and counts only jobs initiated by that operator

#### Scenario: Admin lists release jobs
- **WHEN** an admin requests the release job history endpoint
- **THEN** the API may return and count release jobs across operators

### Requirement: Allowlisted server-side sorting
The system SHALL support sorting only by endpoint-specific allowlisted fields and SHALL reject unsupported sort fields.

#### Scenario: Valid sort requested
- **WHEN** a client requests a supported list endpoint with an allowed `sort_by` and `sort_order`
- **THEN** the API returns records ordered by that field and direction

#### Scenario: Unknown sort requested
- **WHEN** a client requests a supported list endpoint with an unsupported `sort_by`
- **THEN** the API rejects the request with a validation error instead of silently sorting by an arbitrary field

### Requirement: Server-side search and filters
The system SHALL support endpoint-specific search and filter query parameters for table list endpoints.

#### Scenario: Job history filtered
- **WHEN** a client requests release jobs with job filters such as status, kind, force, or search text
- **THEN** the API returns and counts only jobs matching those filters and the current user's authorization scope

#### Scenario: Admin list filtered
- **WHEN** an admin requests users, credentials, switches, imports, command profiles, or audit logs with supported filters
- **THEN** the API returns and counts only records matching those filters

#### Scenario: Filter changes page
- **WHEN** a frontend table filter or search value changes
- **THEN** the frontend requests page 1 for the new filtered result set

### Requirement: Frontend table controls
The frontend SHALL render shadcn-based table controls for supported list pages and request data from server-side pagination, filtering, and sorting parameters.

#### Scenario: User changes page size
- **WHEN** a user changes the page size on a supported table page
- **THEN** the frontend updates the route query parameters and fetches data using the selected page size

#### Scenario: User sorts a column
- **WHEN** a user activates a sortable table header
- **THEN** the frontend updates `sort_by` and `sort_order` query parameters and fetches the sorted server-side page

#### Scenario: User searches a table
- **WHEN** a user submits or edits table search text
- **THEN** the frontend updates query parameters, resets to page 1, and fetches filtered server-side results

### Requirement: Mutation refreshes paginated lists
The frontend SHALL invalidate or refresh affected paginated list queries after mutations that create, update, retry, import, or reset records.

#### Scenario: Admin creates a record
- **WHEN** an admin creates or updates a user, credential, command profile, switch/network import, or password reset
- **THEN** the frontend refreshes the affected paginated list query without relying on stale cached array data

#### Scenario: Operator retries a job
- **WHEN** an operator retries a release job
- **THEN** the frontend refreshes job list queries so the new retry job can appear in history
