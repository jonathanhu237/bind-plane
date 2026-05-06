## ADDED Requirements

### Requirement: Initial admin bootstrap
The FastAPI application SHALL create an initial admin user from environment-backed settings when no admin user exists.

#### Scenario: Startup creates initial admin
- **WHEN** the FastAPI application starts with no existing user holding the `admin` role and complete initial admin settings are configured
- **THEN** the system creates a user with the configured username, password hash, display name, active status, and `admin` role

#### Scenario: Existing admin prevents bootstrap synchronization
- **WHEN** the FastAPI application starts and at least one user already holds the `admin` role
- **THEN** the system does not create, update, or reset any user from the initial admin settings

### Requirement: Initial admin configuration
The system SHALL require complete and valid initial admin settings before FastAPI startup can continue when no admin exists.

#### Scenario: Missing initial admin settings
- **WHEN** the FastAPI application starts with no existing `admin` role user and missing initial admin username or password
- **THEN** startup fails with an explicit configuration error

#### Scenario: Invalid initial admin password
- **WHEN** the FastAPI application starts with no existing `admin` role user and an initial admin password shorter than the user password policy
- **THEN** startup fails with an explicit configuration error

#### Scenario: Username belongs to non-admin
- **WHEN** the FastAPI application starts with no existing `admin` role user and the configured initial admin username already belongs to a non-admin user
- **THEN** startup fails without promoting or modifying that user

### Requirement: Startup scope boundaries
The system SHALL keep initial admin bootstrap limited to FastAPI startup and MUST NOT perform unrelated startup side effects.

#### Scenario: Worker starts
- **WHEN** the background worker process starts
- **THEN** it does not create or validate the initial admin user

#### Scenario: Database schema is not migrated
- **WHEN** FastAPI startup attempts initial admin bootstrap before the user tables exist
- **THEN** startup fails and indicates that migrations must be applied first

### Requirement: Bootstrap credential handling
The system SHALL treat the configured initial admin password as sensitive credential input.

#### Scenario: Initial admin is created
- **WHEN** the system creates the initial admin from settings
- **THEN** it stores only a one-way password hash and does not log the plaintext password

#### Scenario: Initial admin login
- **WHEN** the initial admin logs in with the configured password after bootstrap
- **THEN** the system permits authentication without requiring a password change
