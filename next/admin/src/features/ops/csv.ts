/**
 * A CSV file's text. Spreadsheets on Chinese Windows save CSV as GBK, so a file that is not valid
 * UTF-8 is read as GB18030, which covers GBK.
 */
export function decodeCSV(bytes: AllowSharedBufferSource): string {
  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch {
    return new TextDecoder('gb18030').decode(bytes)
  }
}

/**
 * CSV text for spreadsheets: a byte-order mark so Excel reads UTF-8, CRLF line ends, and quotes only
 * where needed. A cell that starts like a formula gets a leading apostrophe, so opening an export
 * never runs anything an object name or message carried.
 */
export function encodeCSV(rows: readonly (readonly string[])[]): string {
  const cell = (value: string) => {
    const safe = /^[=+\-@\t\r]/.test(value) ? `'${value}` : value
    return /[",\r\n]/.test(safe) ? `"${safe.replaceAll('"', '""')}"` : safe
  }
  return '\ufeff' + rows.map((row) => row.map(cell).join(',')).join('\r\n') + '\r\n'
}

/** Saves text as a file in the browser. */
export function saveText(name: string, text: string, type = 'text/csv;charset=utf-8'): void {
  saveBlob(name, new Blob([text], { type }))
}

/** Saves a blob as a file in the browser. */
export function saveBlob(name: string, blob: Blob): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = name
  document.body.append(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}
