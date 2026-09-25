import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PersonalAccountSettingsPage, avatarCropTransform } from './personal-account-settings-page'
import type { ApiClient } from '@/shared/api/client'
import { i18n, initializeI18n } from '@/shared/i18n'

const user = { id: '00000000-0000-4000-8000-000000000001', name: 'Ada', email: 'ada@example.com', locale: 'en' as const, hasAvatar: false }

function mockApi(overrides: Partial<ApiClient> = {}): ApiClient {
  return {
    getSetupStatus: vi.fn(),
    setup: vi.fn(),
    login: vi.fn(),
    me: vi.fn(),
    logout: vi.fn(),
    requestPasswordReset: vi.fn(),
    completePasswordReset: vi.fn(),
    getPersonalProfile: vi.fn().mockResolvedValue({ user, emailChange: null }),
    updatePersonalName: vi.fn().mockResolvedValue({ ...user, name: 'Grace' }),
    updatePersonalLocale: vi.fn().mockResolvedValue({ ...user, locale: 'zh-CN' }),
    savePersonalAvatar: vi.fn(),
    removePersonalAvatar: vi.fn(),
    changePersonalPassword: vi.fn(),
    requestPersonalEmailChange: vi.fn(),
    resendPersonalEmailChange: vi.fn(),
    verifyPersonalEmailChange: vi.fn(),
    ...overrides,
  }
}

function renderPage(api: ApiClient) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}><PersonalAccountSettingsPage api={api} userID={user.id} /></QueryClientProvider>)
}

describe('personal account settings page', () => {
  beforeEach(async () => {
    HTMLElement.prototype.hasPointerCapture ??= () => false
    HTMLElement.prototype.setPointerCapture ??= () => undefined
    HTMLElement.prototype.releasePointerCapture ??= () => undefined
    HTMLElement.prototype.scrollIntoView ??= () => undefined
    await initializeI18n()
    await i18n.changeLanguage('en')
  })

  it('renders one page title and independently saves the profile name and account language', async () => {
    const updatePersonalName = vi.fn().mockResolvedValue({ ...user, name: 'Grace' })
    const updatePersonalLocale = vi.fn().mockResolvedValue({ ...user, locale: 'zh-CN' })
    const api = mockApi({ updatePersonalName, updatePersonalLocale })
    const person = userEvent.setup()
    renderPage(api)

    expect(await screen.findByLabelText('Display name')).toHaveValue('Ada')
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    await person.clear(screen.getByLabelText('Display name'))
    await person.type(screen.getByLabelText('Display name'), 'Grace')
    await person.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(updatePersonalName).toHaveBeenCalledWith('Grace'))

    await person.click(screen.getByRole('combobox', { name: /Account language/ }))
    await person.click(screen.getByRole('option', { name: '简体中文' }))
    await waitFor(() => expect(updatePersonalLocale).toHaveBeenCalledWith('zh-CN'))
  })

  it('keeps an unsaved name draft when the independent language save completes', async () => {
    const updatePersonalLocale = vi.fn().mockResolvedValue({ ...user, locale: 'zh-CN' })
    const api = mockApi({ updatePersonalLocale })
    const person = userEvent.setup()
    renderPage(api)
    const nameInput = await screen.findByLabelText('Display name')
    await person.clear(nameInput)
    await person.type(nameInput, 'Unsaved name')
    await person.click(screen.getByRole('combobox', { name: /Account language/ }))
    await person.click(screen.getByRole('option', { name: '简体中文' }))
    await waitFor(() => expect(updatePersonalLocale).toHaveBeenCalledWith('zh-CN'))
    expect(nameInput).toHaveValue('Unsaved name')
  })

  it('uses aspect-aware bounds so wide and tall sources reach both edges', () => {
    const wideLeft = avatarCropTransform({ width: 400, height: 200 }, 1, { x: 48, y: 0 })
    const wideRight = avatarCropTransform({ width: 400, height: 200 }, 1, { x: -48, y: 0 })
    expect(wideLeft.sourceLeft).toBeCloseTo(0)
    expect(wideRight.sourceLeft).toBeCloseTo(200)
    expect(wideLeft.maxPanX).toBeCloseTo(48)
    expect(wideLeft.sourceSide).toBeCloseTo(200)

    const tallTop = avatarCropTransform({ width: 200, height: 400 }, 1, { x: 0, y: 48 })
    const tallBottom = avatarCropTransform({ width: 200, height: 400 }, 1, { x: 0, y: -48 })
    expect(tallTop.sourceTop).toBeCloseTo(0)
    expect(tallBottom.sourceTop).toBeCloseTo(200)
    expect(tallTop.maxPanY).toBeCloseTo(48)
    expect(tallTop.sourceSide).toBeCloseTo(200)

    const zoomed = avatarCropTransform({ width: 400, height: 200 }, 1.75, { x: -20, y: 3 })
    // Independent source-space calculation: scale = 96 / 200 * 1.75.
    expect(zoomed.sourceSide).toBeCloseTo(114.2857, 3)
    expect(zoomed.sourceLeft).toBeCloseTo(166.6667, 3)
    expect(zoomed.sourceTop).toBeCloseTo(39.2857, 3)
  })

  it('keeps an avatar crop draft until it is explicitly cancelled', async () => {
    const savePersonalAvatar = vi.fn().mockRejectedValue(new Error('offline'))
    const api = mockApi({ savePersonalAvatar })
    const person = userEvent.setup()
    renderPage(api)
    await screen.findByLabelText('Display name')

    const input = screen.getByLabelText('Choose avatar') as HTMLInputElement
    const png = Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII='), (character) => character.charCodeAt(0))
    const file = new File([png], 'avatar.png', { type: 'image/png' })
    await person.upload(input, file)
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument()
  })
})
