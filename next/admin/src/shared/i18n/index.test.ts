import { beforeEach, describe, expect, it, vi } from 'vitest'
import { changeAccountLocale, changeLocale, i18n, initializeI18n, LOCALE_STORAGE_KEY, restoreGuestLocale, selectInitialLocale } from './index'

describe('locale selection', () => {
  beforeEach(() => window.localStorage.clear())

  it('prefers an explicit saved locale, then browser languages, then English', () => {
    expect(selectInitialLocale('zh-CN', ['en-US'])).toBe('zh-CN')
    expect(selectInitialLocale(null, ['fr-FR', 'zh-TW'])).toBe('zh-CN')
    expect(selectInitialLocale(null, ['fr-FR'])).toBe('en')
    expect(LOCALE_STORAGE_KEY).toBe('temvia.locale')
  })

  it('persists only an explicit change and synchronizes document metadata', async () => {
    await initializeI18n()
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBeNull()

    await changeLocale('zh-CN')
    expect(i18n.language).toBe('zh-CN')
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-CN')
    expect(document.documentElement).toHaveAttribute('lang', 'zh-CN')
    expect(document.documentElement).toHaveAttribute('dir', 'ltr')

    await changeLocale('en')
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('en')
    expect(document.documentElement).toHaveAttribute('lang', 'en')
    expect(document.documentElement).toHaveAttribute('dir', 'ltr')
  })

  it('keeps account locale out of guest storage and restores the guest locale after logout', async () => {
    await initializeI18n()
    await changeLocale('en')
    await changeAccountLocale('zh-CN')
    expect(i18n.language).toBe('zh-CN')
    expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('en')
    window.dispatchEvent(new StorageEvent('storage', { key: LOCALE_STORAGE_KEY, newValue: 'zh-CN' }))
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(i18n.language).toBe('zh-CN')
    await restoreGuestLocale()
    expect(i18n.language).toBe('en')
    expect(document.documentElement).toHaveAttribute('lang', 'en')
  })

  it('applies a locale changed in another same-origin tab', async () => {
    await initializeI18n()
    const change = vi.spyOn(i18n, 'changeLanguage')
    window.dispatchEvent(new StorageEvent('storage', { key: LOCALE_STORAGE_KEY, newValue: 'zh-CN' }))
    await vi.waitFor(() => expect(i18n.language).toBe('zh-CN'))
    expect(change).toHaveBeenCalledWith('zh-CN')
    change.mockRestore()
  })
})
