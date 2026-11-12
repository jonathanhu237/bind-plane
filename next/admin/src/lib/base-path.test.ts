import { afterEach, describe, expect, it, vi } from 'vitest'

async function load(base: string) {
  vi.resetModules()
  vi.stubEnv('BASE_URL', base)
  return import('./base-path')
}

afterEach(() => {
  vi.unstubAllEnvs()
  vi.resetModules()
})

describe('withBase', () => {
  it('leaves paths as they are at the root', async () => {
    const { basePath, withBase } = await load('/')
    expect(basePath).toBe('')
    expect(withBase('/api/releases')).toBe('/api/releases')
  })

  it('puts paths on this origin under the base path, once', async () => {
    const { basePath, withBase } = await load('/bind-plane/')
    expect(basePath).toBe('/bind-plane')
    expect(withBase('/api/releases')).toBe('/bind-plane/api/releases')
    expect(withBase(withBase('/login'))).toBe('/bind-plane/login')
    expect(withBase('/bind-plane')).toBe('/bind-plane')
    expect(withBase('/bind-planes')).toBe('/bind-plane/bind-planes')
    for (const other of ['https://example.com/x', '//example.com/x', 'data:image/png;base64,AA', 'relative/path']) {
      expect(withBase(other)).toBe(other)
    }
  })
})
