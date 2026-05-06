## Why

Bind Plane is an internal operator tool, and the frontend currently hard-codes English copy across login, release, jobs, admin, and shared controls. Adding frontend i18n lets the app default to Chinese while retaining English for operators who prefer it, without changing backend workflow semantics.

## What Changes

- Add frontend internationalization using `i18next` and `react-i18next`.
- Support `zh-CN` and `en-US`, defaulting to `zh-CN`.
- Persist the selected locale locally so it survives reloads.
- Expose a locale switcher on both the login route and authenticated shell header.
- Move current frontend user-visible UI copy and validation messages into translation resources.
- Keep backend enum values, API payloads, raw audit data, raw switch transcripts, and stored job data untranslated.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `frontend-application-shell`: add frontend locale preference behavior, locale controls, and translated UI copy.

## Impact

- Frontend dependencies: add `i18next` and `react-i18next`.
- Frontend state: extend local client preferences with locale selection.
- Frontend UI: add a reusable locale control and update visible copy to use translation keys.
- Frontend tests: cover default Chinese, English switching, persistence, and login/shell controls.
- Backend/API: no changes.
