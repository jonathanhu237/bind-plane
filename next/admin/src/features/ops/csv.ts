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
