## 1. Preference State

- [x] 1.1 Add a frontend preference store for `light`, `dark`, and `system` theme modes with guarded `localStorage` persistence.
- [x] 1.2 Add a root-mounted theme effect that resolves `system` via `matchMedia`, toggles `.dark` on `document.documentElement`, and reacts to system preference changes.

## 2. Theme Control UI

- [x] 2.1 Add a reusable shadcn-styled `ThemeModeToggle` control using existing UI primitives and lucide icons.
- [x] 2.2 Render the theme control on the login page.
- [x] 2.3 Render the theme control in the authenticated shell header without disrupting sidebar navigation or page titles.

## 3. Tests

- [x] 3.1 Add frontend tests for default `system` behavior, explicit dark/light selection, persistence, and system preference changes.
- [x] 3.2 Add route tests proving the theme control is available on both `/login` and authenticated shell routes.

## 4. Verification

- [x] 4.1 Run the focused frontend test suite.
- [x] 4.2 Run frontend lint/build checks.
- [x] 4.3 Run project-level checks required by the change and `openspec validate add-theme-mode-toggle --strict --no-interactive`.
