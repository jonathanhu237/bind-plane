import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'
import { AuthPage } from './auth-page'
import { i18n, initializeI18n } from '@/shared/i18n'
import { ThemeProvider } from '@/shared/theme'

describe('authentication page shell', () => {
  beforeEach(async () => {
    await initializeI18n()
    await i18n.changeLanguage('en')
    window.localStorage.removeItem('temvia.theme')
    document.documentElement.className = ''
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.style.colorScheme = ''
  })

  it('exposes separate appearance and language controls in the auth header', () => {
    render(
      <AuthPage title="Create your administrator account">
        <form aria-label="Create administrator" />
      </AuthPage>,
    )

    expect(screen.getByRole('heading', { name: 'Create your administrator account' })).toBeVisible()
    expect(screen.getAllByRole('heading')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Appearance settings' })).toHaveClass('max-sm:size-11', 'max-sm:shrink-0')
    expect(screen.getByRole('button', { name: 'Language settings' })).toHaveClass('max-sm:size-11', 'max-sm:shrink-0')
    expect(screen.getByRole('button', { name: 'Appearance settings' })).toHaveClass('h-10', 'w-10')
    expect(screen.getByRole('button', { name: 'Language settings' })).toHaveClass('h-10', 'w-10')
    expect(screen.getByRole('button', { name: 'Appearance settings' }).parentElement).toHaveClass('gap-3')
    expect(screen.queryByText('Appearance')).not.toBeInTheDocument()
    expect(screen.queryByText('English')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Language' })).not.toBeInTheDocument()
    expect(screen.getByText('Temvia')).toBeVisible()
  })

  it('keeps the authentication shell to one page heading', () => {
    render(
      <AuthPage title="Example page">
        <p>Page content</p>
      </AuthPage>,
    )

    expect(screen.getByText('Page content')).toBeVisible()
    expect(screen.getAllByRole('heading')).toHaveLength(1)
  })

  it('opens each authentication preference menu directly and keeps selections independent', async () => {
    const user = userEvent.setup()
    render(
      <ThemeProvider>
        <AuthPage title="Sign in">
          <form />
        </AuthPage>
      </ThemeProvider>,
    )

    await user.click(screen.getByRole('button', { name: 'Appearance settings' }))
    expect(document.body).not.toHaveStyle({ overflow: 'hidden' })
    expect(document.body).not.toHaveStyle({ pointerEvents: 'none' })
    expect(screen.getByRole('menuitemradio', { name: 'Follow system' })).toBeVisible()
    expect(screen.getByRole('menuitemradio', { name: 'Light' })).toBeVisible()
    expect(screen.getByRole('menuitemradio', { name: 'Dark' })).toBeVisible()
    expect(screen.queryByRole('menuitemradio', { name: 'English' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('menuitemradio', { name: 'Dark' }))

    expect(document.documentElement).toHaveClass('dark')
    expect(window.localStorage.getItem('temvia.theme')).toBe('dark')
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Appearance settings' }))

    await user.click(screen.getByRole('button', { name: 'Language settings' }))
    expect(screen.getByRole('menuitemradio', { name: 'English' })).toBeVisible()
    expect(screen.getByRole('menuitemradio', { name: '简体中文' })).toBeVisible()
    expect(screen.queryByRole('menuitemradio', { name: 'Dark' })).not.toBeInTheDocument()
    expect(document.documentElement).toHaveClass('dark')
  })
})
