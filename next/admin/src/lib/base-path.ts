/** The path the app is served under, from Vite's base: '' at the root, '/bind-plane' behind a shared proxy. */
export const basePath = import.meta.env.BASE_URL.replace(/\/+$/, '')

/**
 * Puts an absolute path on this origin ('/api/…', '/login') under the base path. Paths already
 * under it, full URLs and anything else stay as they are, so applying it twice changes nothing.
 */
export function withBase(path: string): string {
  if (!path.startsWith('/') || path.startsWith('//')) return path
  if (basePath && (path === basePath || path.startsWith(`${basePath}/`))) return path
  return `${basePath}${path}`
}
