import react from '@vitejs/plugin-react'
import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import { defineConfig, loadEnv } from 'vite'

// BIND_PLANE_BASE_PATH is the path the app is served under, such as /bind-plane/ behind a
// shared proxy; the admin image passes the same value to Caddy. Default: the root.
function basePath(value = '/'): string {
  if (!/^\/(?:[A-Za-z0-9][A-Za-z0-9._~-]*\/)*$/.test(value)) {
    throw new Error(`BIND_PLANE_BASE_PATH must look like / or /bind-plane/, not ${JSON.stringify(value)}`)
  }
  return value
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const rootEnvironment = loadEnv(mode, path.resolve(import.meta.dirname, '..'), '')
  const base = basePath(process.env.BIND_PLANE_BASE_PATH || undefined)
  const prefix = base.slice(0, -1)

  return {
    base,
    resolve: {
      alias: {
        '@': path.resolve(import.meta.dirname, 'src'),
      },
    },
    plugins: [
      tanstackRouter({
        target: 'react',
        autoCodeSplitting: true,
        routesDirectory: './src/routes',
        generatedRouteTree: './src/routeTree.gen.ts',
      }),
      react(),
      tailwindcss(),
    ],
    server: {
      host: '127.0.0.1',
      port: 5173,
      proxy: {
        [`${prefix}/api`]: {
          target: `http://127.0.0.1:${rootEnvironment.API_PORT || '8080'}`,
          changeOrigin: false,
          rewrite: (requestPath) => requestPath.slice(prefix.length),
        },
      },
    },
  }
})
