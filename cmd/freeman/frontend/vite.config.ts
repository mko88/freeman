import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import path from 'path'

// mode is 'web' for `vite build --mode web` / `vite --mode web` (see
// package.json's build:web/dev:web scripts), the default Wails-targeting
// build otherwise. Selects which $backend implementation gets bundled —
// see src/backend.wails.ts vs src/backend.http.ts.
export default defineConfig(({mode}) => ({
  plugins: [svelte()],
  resolve: {
    alias: {
      '$backend': path.resolve(import.meta.dirname, mode === 'web' ? 'src/backend.http.ts' : 'src/backend.wails.ts'),
    },
  },
  build: {
    outDir: mode === 'web' ? 'dist-web' : 'dist',
  },
  server: mode === 'web' ? {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  } : undefined,
}))
