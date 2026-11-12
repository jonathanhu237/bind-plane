import { afterEach, describe, expect, it, vi } from 'vitest'
import { copyText } from './clipboard'

function setSecure(secure: boolean, writeText?: (text: string) => Promise<void>) {
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: secure })
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: writeText ? { writeText } : undefined })
}

afterEach(() => {
  setSecure(false)
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('copyText', () => {
  it('uses the Clipboard API on HTTPS', async () => {
    const writeText = vi.fn(async () => {})
    setSecure(true, writeText)
    const execCommand = vi.fn(() => true)
    document.execCommand = execCommand
    expect(await copyText('secret')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('secret')
    expect(execCommand).not.toHaveBeenCalled()
  })

  it('over plain HTTP copies a selected text area inside the dialog, then puts focus back', async () => {
    setSecure(false)
    const dialog = document.createElement('div')
    const button = document.createElement('button')
    dialog.append(button)
    document.body.append(dialog)
    button.focus()
    let copied = ''
    document.execCommand = vi.fn((command: string) => {
      const area = dialog.querySelector('textarea')
      copied = command === 'copy' && area ? area.value.slice(area.selectionStart, area.selectionEnd) : ''
      return true
    })
    expect(await copyText('initial password', dialog)).toBe(true)
    expect(copied).toBe('initial password')
    expect(dialog.querySelector('textarea')).toBeNull()
    expect(button).toHaveFocus()
  })

  it('falls back when the Clipboard API refuses, and reports a failed copy', async () => {
    setSecure(true, vi.fn(async () => { throw new Error('denied') }))
    document.execCommand = vi.fn(() => false)
    expect(await copyText('x')).toBe(false)
    document.execCommand = vi.fn(() => { throw new Error('unsupported') })
    expect(await copyText('x')).toBe(false)
    expect(document.querySelector('textarea')).toBeNull()
  })
})
