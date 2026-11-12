/**
 * Copies text to the clipboard. Browsers offer the Clipboard API only on HTTPS (or localhost), so
 * over plain HTTP this falls back to copying a selected, invisible text area. Pass the open dialog
 * as `within`: a dialog keeps focus inside itself, and the text area must be in it to be selected.
 */
export async function copyText(text: string, within: HTMLElement = document.body): Promise<boolean> {
  if (window.isSecureContext && navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Denied or unavailable after all; try the fallback.
    }
  }
  const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
  const area = document.createElement('textarea')
  area.value = text
  area.readOnly = true
  area.setAttribute('aria-hidden', 'true')
  Object.assign(area.style, { position: 'fixed', top: '0', left: '0', width: '1px', height: '1px', opacity: '0' })
  within.append(area)
  try {
    area.select()
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    area.remove()
    previous?.focus()
  }
}
