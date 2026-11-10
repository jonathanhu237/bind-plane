import { describe, expect, it } from 'vitest'
import { highlightParts } from './config-search'

describe('highlightParts', () => {
  it('marks every plain match regardless of case, taking the query literally', () => {
    expect(highlightParts(' local-user Admin password', 'ADMIN', false)).toEqual([
      { text: ' local-user ', hit: false }, { text: 'Admin', hit: true }, { text: ' password', hit: false },
    ])
    expect(highlightParts('ip route 1.1.1.1', '1.1', false).filter((part) => part.hit).map((part) => part.text)).toEqual(['1.1', '1.1'])
    expect(highlightParts('a+b', 'a+b', false)).toEqual([{ text: 'a+b', hit: true }])
  })

  it('marks pattern matches, and leaves a line plain when JavaScript cannot read the pattern', () => {
    expect(highlightParts('interface Vlanif10', '^interface Vlanif\\d+$', true)).toEqual([{ text: 'interface Vlanif10', hit: true }])
    expect(highlightParts('interface Vlanif10', '(?i)vlanif', true)).toEqual([{ text: 'interface Vlanif10', hit: false }])
    expect(highlightParts('anything', 'x*', true)).toEqual([{ text: 'anything', hit: false }])
  })
})
