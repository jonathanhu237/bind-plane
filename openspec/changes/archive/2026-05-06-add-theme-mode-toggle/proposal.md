## Why

Bind Plane already uses the shadcn/ui token baseline, but users cannot choose light, dark, or system appearance. A theme mode control makes the operator console usable across different workstation preferences without changing backend workflow behavior.

## What Changes

- Add a frontend-only theme preference for `light`, `dark`, and `system` modes.
- Default the app to `system`, resolving from the browser color-scheme preference.
- Persist the selected theme locally so the choice survives reloads.
- Expose the theme control on both the login route and the authenticated application shell.
- Keep the existing FastAPI API contract unchanged.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `frontend-application-shell`: add global theme mode preference behavior and shell/login controls.

## Impact

- Frontend state: add a small persisted client preference boundary for theme mode.
- Frontend UI: add a reusable theme mode control and render it in unauthenticated and authenticated surfaces.
- Frontend styling: continue using existing shadcn `new-york`/`neutral` CSS variables and the `.dark` class.
- Backend/API: no changes.
