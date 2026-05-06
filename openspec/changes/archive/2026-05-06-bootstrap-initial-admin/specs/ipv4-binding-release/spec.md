## MODIFIED Requirements

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
