## Context

The frontend uses React/Vite with routed pages, TanStack Query for server state, Zustand for local preferences, and shadcn/ui primitives. Theme mode is already a local preference, but all visible application copy is still inline English text. The next change should introduce a frontend-only i18n layer without changing backend payloads or persisted release/audit data.

The agreed scope is `zh-CN` and `en-US`, defaulting to `zh-CN`. Backend enum values, command outputs, raw switch transcripts, stored audit payloads, and API error payloads remain source data; the frontend may translate known UI labels and local validation messages, but must not mutate stored domain records.

## Goals / Non-Goals

**Goals:**

- Add `i18next` and `react-i18next` for React translation hooks and runtime locale switching.
- Support `zh-CN` and `en-US`, defaulting to `zh-CN`.
- Persist locale locally through the existing client preference boundary.
- Add a reusable locale switcher to `/login` and the authenticated shell header.
- Move current user-visible frontend UI text and local validation messages into translation resources.
- Keep technical and operator-critical source values visible where they represent backend/job/switch data.
- Cover default locale, switching, persistence, and control placement with focused frontend tests.

**Non-Goals:**

- Do not translate backend enum values, API payload shapes, raw audit output, raw switch command transcripts, or stored release job data.
- Do not add backend user-profile preferences for locale.
- Do not change route paths or introduce locale URL prefixes.
- Do not add automated type-safe translation key generation in this change.
- Do not redesign pages while moving copy into translation resources.

## Decisions

- Use `i18next` with `react-i18next` because it is the established React runtime i18n library and supports in-memory TS resources, interpolation, and runtime language switching without a backend service.
- Store resources as TypeScript objects under the frontend source tree, organized by feature-oriented key namespaces such as `auth`, `nav`, `release`, `jobs`, `admin`, `tables`, `common`, and `validation`. This keeps the MVP simple and reviewable without adding extraction tooling.
- Extend the existing preference store with `locale`, using a guarded localStorage key and defaulting to `zh-CN` when storage is unavailable or invalid.
- Initialize i18n at app startup and sync language from the preference store through a small root-mounted effect, matching the existing theme sync pattern.
- Use a reusable `LocaleSwitcher` built from shadcn primitives. It should be compact enough for the authenticated header and available on the login page.
- Keep frontend display mapping separate from backend data. Known UI labels and local status/reason display names can use translation keys, but raw API values and transcripts remain unchanged in data structures and requests.

## Risks / Trade-offs

- Translating all visible UI copy can produce a large mechanical diff → keep changes scoped to frontend display text and avoid unrelated component refactors.
- Missing a hard-coded string is possible → use targeted text search during implementation and add route-level tests for representative Chinese and English strings.
- API errors may arrive in English or structured backend text → display them as received unless the frontend owns the validation/error message.
- Translation keys can drift without key typing → keep resources colocated and covered by TypeScript object access; defer codegen until the copy surface stabilizes.
