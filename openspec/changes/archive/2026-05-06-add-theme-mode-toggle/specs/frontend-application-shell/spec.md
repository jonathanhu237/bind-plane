## ADDED Requirements

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
