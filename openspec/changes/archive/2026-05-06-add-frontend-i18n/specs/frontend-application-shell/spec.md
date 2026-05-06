## ADDED Requirements

### Requirement: Frontend locale preference
The frontend SHALL support a client-side locale preference for `zh-CN` and `en-US`, defaulting to `zh-CN` and persisting the selected locale locally.

#### Scenario: Default locale is Chinese
- **WHEN** a user opens the frontend without a stored locale preference
- **THEN** the frontend renders user-visible UI copy in Simplified Chinese

#### Scenario: User selects English
- **WHEN** a user selects `en-US`
- **THEN** the frontend persists `en-US` locally and renders user-visible UI copy in English without a page reload

#### Scenario: User selects Chinese
- **WHEN** a user selects `zh-CN`
- **THEN** the frontend persists `zh-CN` locally and renders user-visible UI copy in Simplified Chinese without a page reload

### Requirement: Locale controls
The frontend SHALL expose the locale control on both unauthenticated and authenticated application surfaces using shadcn/ui primitives.

#### Scenario: Login page renders locale control
- **WHEN** a user opens `/login`
- **THEN** the login surface displays a locale control that can select Chinese or English

#### Scenario: Authenticated shell renders locale control
- **WHEN** an authenticated user opens a protected application route
- **THEN** the authenticated shell header displays a locale control that can select Chinese or English

#### Scenario: Locale preference is shared across routes
- **WHEN** a user changes the locale from either the login page or authenticated shell
- **THEN** the selected locale is shared across frontend routes and remains active after reload

### Requirement: Translated frontend UI copy
The frontend SHALL render current user-visible frontend-owned UI copy and local validation messages from translation resources for both supported locales.

#### Scenario: Frontend-owned form validation displays
- **WHEN** a user submits a frontend form with missing required fields
- **THEN** the frontend displays the locally generated validation messages in the selected locale

#### Scenario: Navigation and workflow UI renders
- **WHEN** a user opens login, release console, job history/detail, or admin routes
- **THEN** frontend-owned labels, headings, buttons, navigation items, helper text, and table controls render in the selected locale

#### Scenario: Source data remains unchanged
- **WHEN** the frontend displays backend enum values, raw audit payloads, raw switch transcripts, job records, or API error messages
- **THEN** the frontend does not mutate those source values for storage or API requests
