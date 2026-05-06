## Context

The frontend already uses React/Vite, React Router, TanStack Query, Zustand, Tailwind, and shadcn/ui `new-york`/`neutral` tokens. The global stylesheet includes both default and `.dark` CSS variables, but no client preference currently applies `.dark` or exposes a user-facing theme control.

The theme change is purely frontend behavior. It must not affect release job semantics, backend auth, API payloads, audit records, or server-side table contracts.

## Goals / Non-Goals

**Goals:**

- Add a persisted client theme preference with `light`, `dark`, and `system` modes.
- Resolve `system` from `prefers-color-scheme` and keep it responsive when the OS preference changes.
- Apply theme globally through the existing shadcn `.dark` class mechanism.
- Provide one reusable shadcn-styled theme control in both `/login` and the authenticated shell header.
- Cover the preference behavior and both placement surfaces with focused frontend tests.

**Non-Goals:**

- Do not store theme preferences in the backend or user profile.
- Do not add a new routing convention, URL locale/theme segment, or API contract.
- Do not redesign the application shell or change release workflow copy.
- Do not introduce `next-themes`; this is a Vite app and the existing Zustand pattern is enough.

## Decisions

- Use a new Zustand preference store for theme mode, matching the existing auth-token client state pattern. This keeps local-only UI preferences separate from TanStack Query server state and avoids backend changes.
- Persist the selected mode in `localStorage`, using `system` when no valid value exists. Storage read/write failures should be ignored so constrained browser or test environments still render.
- Apply theme in a small React effect component/hook mounted at the app root. The effect resolves `system` with `window.matchMedia("(prefers-color-scheme: dark)")`, toggles `.dark` on `document.documentElement`, and subscribes to media-query changes only when needed.
- Build a reusable `ThemeModeToggle` from existing shadcn primitives, preferably `DropdownMenu` plus icon button labels for light, dark, and system. This keeps the control compact in the shell header and usable on the login page.
- Keep all theme labels in English for this change. The following i18n change will move user-visible copy into translation resources.

## Risks / Trade-offs

- System theme can only be resolved in a browser-like environment → guard `window`, `document`, `matchMedia`, and storage access so tests and non-browser render paths do not crash.
- A root effect can briefly render before the class is applied → mount it at the top of `App` and keep the implementation minimal; this is acceptable for the MVP frontend.
- Tests can become brittle if they depend on visual colors → assert class application, storage updates, and control presence rather than computed color values.
