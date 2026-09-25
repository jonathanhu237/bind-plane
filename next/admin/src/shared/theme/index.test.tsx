import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeProvider, useTheme } from './index'

function ThemeProbe() {
  const { theme, resolvedTheme, setTheme } = useTheme()
  return <div><span data-testid="theme">{theme}</span><span data-testid="resolved">{resolvedTheme}</span><button type="button" onClick={() => setTheme('dark')}>Dark</button></div>
}

describe('theme preference', () => {
  beforeEach(() => {
    window.localStorage.clear()
    document.documentElement.className = ''
    document.documentElement.removeAttribute('data-theme')
    document.documentElement.style.colorScheme = ''
    vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() }))
  })

  it('defaults to the system preference and applies its resolved class', () => {
    render(<ThemeProvider><ThemeProbe /></ThemeProvider>)
    expect(screen.getByTestId('theme')).toHaveTextContent('system')
    expect(screen.getByTestId('resolved')).toHaveTextContent('dark')
    expect(document.documentElement).toHaveClass('dark')
    expect(window.localStorage.getItem('temvia.theme')).toBe('system')
  })

  it('persists an explicit theme locally', async () => {
    render(<ThemeProvider><ThemeProbe /></ThemeProvider>)
    await userEvent.setup().click(screen.getByRole('button', { name: 'Dark' }))
    expect(screen.getByTestId('theme')).toHaveTextContent('dark')
    expect(document.documentElement).toHaveClass('dark')
    expect(window.localStorage.getItem('temvia.theme')).toBe('dark')
  })

  it('updates resolved theme when the system preference changes', async () => {
    let systemIsDark = true
    let onChange: (() => void) | undefined
    vi.stubGlobal('matchMedia', vi.fn().mockImplementation(() => ({
      get matches() { return systemIsDark },
      addEventListener: (_event: string, listener: () => void) => { onChange = listener },
      removeEventListener: vi.fn(),
    })))
    render(<ThemeProvider><ThemeProbe /></ThemeProvider>)
    expect(screen.getByTestId('resolved')).toHaveTextContent('dark')
    await waitFor(() => expect(onChange).toBeTypeOf('function'))
    systemIsDark = false
    onChange?.()
    await waitFor(() => {
      expect(screen.getByTestId('resolved')).toHaveTextContent('light')
      expect(document.documentElement).toHaveClass('light')
    })
  })
})
