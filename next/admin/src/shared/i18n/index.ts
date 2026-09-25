import i18next, { type i18n as I18nInstance } from 'i18next'
import { initReactI18next } from 'react-i18next'
import { resources, type Locale } from './resources'

// This key is intentionally guest-only. Authenticated account locale is
// server-owned and is applied in memory, so a browser's guest preference can
// never overwrite an account setting.
export const LOCALE_STORAGE_KEY = 'temvia.locale'
export const GUEST_LOCALE_STORAGE_KEY = LOCALE_STORAGE_KEY
export const supportedLocales: readonly Locale[] = ['zh-CN', 'en']

function asLocale(value: string | null | undefined): Locale | undefined {
  if (!value) return undefined
  const normalized = value.toLowerCase()
  if (normalized === 'zh' || normalized.startsWith('zh-')) return 'zh-CN'
  if (normalized === 'en' || normalized.startsWith('en-')) return 'en'
  return undefined
}

export function selectInitialLocale(
  stored: string | null | undefined,
  languages: readonly string[] = typeof navigator === 'undefined' ? [] : navigator.languages,
): Locale {
  return asLocale(stored) ?? languages.map(asLocale).find((locale): locale is Locale => locale !== undefined) ?? 'en'
}

function syncDocumentLanguage(locale: Locale): void {
  if (typeof document === 'undefined') return
  document.documentElement.lang = locale
  document.documentElement.dir = 'ltr'
}

export const i18n = i18next.createInstance()
let localeStorageListenerAttached = false
let accountLocaleActive = false
let localeOperation = 0

function attachLocaleStorageListener(): void {
  if (typeof window === 'undefined' || localeStorageListenerAttached) return
  window.addEventListener('storage', (event) => {
    if (event.key !== LOCALE_STORAGE_KEY || (event.newValue !== 'en' && event.newValue !== 'zh-CN')) return
    if (!accountLocaleActive) void changeGuestLocale(event.newValue)
  })
  localeStorageListenerAttached = true
}

export async function initializeI18n(): Promise<I18nInstance> {
  attachLocaleStorageListener()
  const stored = (() => {
    try {
      return window.localStorage.getItem(LOCALE_STORAGE_KEY)
    } catch {
      return null
    }
  })()
  const locale = selectInitialLocale(stored)
  await i18n.use(initReactI18next).init({
    resources,
    lng: locale,
    fallbackLng: 'en',
    defaultNS: 'common',
    interpolation: { escapeValue: false },
  })
  syncDocumentLanguage(locale)
  return i18n
}

export async function changeGuestLocale(locale: Locale): Promise<void> {
  const operation = ++localeOperation
  accountLocaleActive = false
  await i18n.changeLanguage(locale)
  if (operation !== localeOperation) return
  syncDocumentLanguage(locale)
  try {
    window.localStorage.setItem(GUEST_LOCALE_STORAGE_KEY, locale)
  } catch {
    // A blocked storage implementation should not prevent a language change.
  }
}

// changeLocale remains the guest-facing compatibility API used by auth pages.
export async function changeLocale(locale: Locale): Promise<void> {
  await changeGuestLocale(locale)
}

export async function changeAccountLocale(locale: Locale): Promise<void> {
  const operation = ++localeOperation
  accountLocaleActive = true
  await i18n.changeLanguage(locale)
  if (operation !== localeOperation) return
  syncDocumentLanguage(locale)
}

export async function restoreGuestLocale(): Promise<void> {
  const operation = ++localeOperation
  const stored = (() => {
    try {
      return window.localStorage.getItem(GUEST_LOCALE_STORAGE_KEY)
    } catch {
      return null
    }
  })()
  accountLocaleActive = false
  const locale = selectInitialLocale(stored)
  await i18n.changeLanguage(locale)
  if (operation !== localeOperation) return
  syncDocumentLanguage(locale)
}

declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'common'
    resources: typeof resources.en
  }
}
