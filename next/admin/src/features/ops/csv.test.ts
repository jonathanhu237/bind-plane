import { describe, expect, it } from 'vitest'
import { decodeCSV } from './csv'

describe('decodeCSV', () => {
  it('reads UTF-8, and falls back to GBK for spreadsheets saved on Chinese Windows', () => {
    const utf8 = new TextEncoder().encode('设备名称,IP地址\n核心-01,10.0.0.1\n')
    expect(decodeCSV(utf8)).toBe('设备名称,IP地址\n核心-01,10.0.0.1\n')
    // "设备名称,IP" in GBK.
    const gbk = new Uint8Array([0xc9, 0xe8, 0xb1, 0xb8, 0xc3, 0xfb, 0xb3, 0xc6, 0x2c, 0x49, 0x50])
    expect(decodeCSV(gbk)).toBe('设备名称,IP')
  })
})
