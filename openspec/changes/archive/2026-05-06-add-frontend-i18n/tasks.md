## 1. I18n Foundation

- [x] 1.1 Add `i18next` and `react-i18next` frontend dependencies.
- [x] 1.2 Add locale types, default `zh-CN`, guarded localStorage persistence, and locale state to the frontend preferences store.
- [x] 1.3 Add i18n initialization with TypeScript translation resources for `zh-CN` and `en-US`.
- [x] 1.4 Add a root-mounted locale sync effect that keeps i18next language aligned with the stored locale.

## 2. Locale Control UI

- [x] 2.1 Add a reusable shadcn-styled `LocaleSwitcher` control for Chinese and English.
- [x] 2.2 Render the locale switcher on the login page.
- [x] 2.3 Render the locale switcher in the authenticated shell header alongside the theme mode control.

## 3. Translate Frontend Copy

- [x] 3.1 Move login, navigation, shell, common actions, table controls, and shared status/reason display copy into translation resources.
- [x] 3.2 Move release workflow UI copy and local validation messages into translation resources while preserving API request values.
- [x] 3.3 Move job history/detail UI copy into translation resources while preserving raw job values and switch transcripts.
- [x] 3.4 Move admin management page UI copy into translation resources while preserving backend data values.

## 4. Tests

- [x] 4.1 Add tests for default Chinese locale, English switching, Chinese switching, persistence, and no-reload updates.
- [x] 4.2 Add route tests proving locale controls render on `/login` and authenticated shell routes.
- [x] 4.3 Add representative workflow tests proving translated frontend validation/navigation text renders while source values remain unchanged.

## 5. Verification

- [x] 5.1 Run the focused frontend test suite.
- [x] 5.2 Run frontend lint/build checks.
- [x] 5.3 Run project-level checks required by the change and `openspec validate add-frontend-i18n --strict --no-interactive`.
