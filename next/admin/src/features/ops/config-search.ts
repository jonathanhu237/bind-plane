/** A line cut into the parts a search matched and the parts it did not, for highlighting. */
export function highlightParts(line: string, query: string, regex: boolean): { text: string; hit: boolean }[] {
  const parts: { text: string; hit: boolean }[] = []
  let pattern: RegExp
  try {
    // Patterns are RE2 on the server; one JavaScript cannot read is shown without highlighting.
    pattern = regex ? new RegExp(query, 'g') : new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi')
  } catch {
    return [{ text: line, hit: false }]
  }
  let last = 0
  for (const match of line.matchAll(pattern)) {
    if (!match[0]) continue
    const start = match.index
    if (start > last) parts.push({ text: line.slice(last, start), hit: false })
    parts.push({ text: match[0], hit: true })
    last = start + match[0].length
  }
  if (last < line.length || !parts.length) parts.push({ text: line.slice(last), hit: false })
  return parts
}
