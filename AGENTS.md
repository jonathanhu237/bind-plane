## Development Workflow

This project uses a structured Claude + Codex collaboration loop. All participants must follow this workflow.

### Roles

- **Claude:** planning, code review, committing
- **Codex:** implementation, execution summary

### Parallelism

Both Claude and Codex should maximize use of parallel agents whenever tasks are independent. Do not execute sequentially what can be done concurrently — spawn multiple agents in parallel for exploration, implementation, or review sub-tasks where there are no dependencies between them.

### Loop

```
Claude → PLAN.md → Codex → SUMMARY.md → Claude → REVIEW.md → Codex (fix) → Claude (verify) → commit
```

### Step-by-step

1. **Claude writes `PLAN.md`** to the project root before any implementation begins.
   - Must include: context, goal, file-level change list, verification steps.
   - Describe **intent and constraints**, not implementation details. Do not paste code snippets into the plan — let Codex decide how to implement. Overly prescriptive plans cause Codex to copy-paste rather than reason.

2. **Codex implements** according to `PLAN.md`, then writes `SUMMARY.md` to the project root.
   - `SUMMARY.md` must cover: what was done, what was verified, any blockers or deviations from the plan.

3. **Claude reviews** the implementation against `PLAN.md` and `SUMMARY.md`, then writes `REVIEW.md` to the project root.
   - `REVIEW.md` must include: verdict (LGTM / issues found), what Codex did well, and each issue with file + line reference and a concrete fix.

4. **If issues exist:** Codex reads `REVIEW.md` and fixes all items. Return to step 3.

5. **If LGTM:** Claude verifies the final state, deletes `PLAN.md`, `REVIEW.md`, and `SUMMARY.md`, then creates a conventional commit.

### Commit convention

Follow [Conventional Commits](https://www.conventionalcommits.org/):
- `feat(scope):` new feature
- `fix(scope):` bug fix
- `chore:` tooling, config, dependencies
- `docs:` documentation only

Do not commit intermediate files (`PLAN.md`, `REVIEW.md`, `SUMMARY.md`).

---

## Project Overview

BindPlane is a self-hosted IP/MAC binding management platform designed for network administrators. It provides a user-friendly interface for managing and monitoring IP-to-MAC address bindings on network devices (switches), ensuring network security and stable operation.

## Core Features

- **Authentication & Session Management**
  Admin login with session-based authentication (Redis-backed).

- **Switch Management**
  CRUD operations for managed network switches (connection info, credentials).

- **IP/MAC Binding Management**
  View, create, update, and delete IP-to-MAC bindings on switches.

- **Binding Synchronization**
  Push binding configurations to switches and pull current state from devices.

- **Audit Logging**
  Track who changed what and when for compliance and troubleshooting.

## Scope Boundaries

- Admin-only platform — no end-user self-service
- No automatic binding detection or network scanning
- No multi-tenancy — single organization per deployment

---

## Tech Stack

### Frontend

- **Framework:** React (Vite)
- **Package manager:** pnpm

### Backend

- **Framework:** Python + FastAPI (async)
- **Database:** PostgreSQL, launched via `docker-compose.yml`
- **Session store:** Redis, launched via `docker-compose.yml`
- **ORM:** SQLAlchemy (async)
- **Package manager:** uv

### Infrastructure

- **Containerization:** Docker Compose for PostgreSQL and Redis
- **Configuration:** Environment variables (`.env` file, defaults in `.env.example`)

---

## Coding Guidelines

- All code comments must be written in English.
- Use environment variables for configuration. Non-sensitive settings should have defaults in `.env.example`; sensitive settings (passwords, secrets) must be set by the user in `.env`.
- In development, default passwords are `pa55word`.
