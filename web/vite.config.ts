import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const dirname = path.dirname(fileURLToPath(import.meta.url))

// Cartograph's web UI: built with `npm run build` into dist/, then
// internal/httpserver embeds that directory via go:embed (see
// internal/httpserver/httpserver.go) — the same "one binary, no runtime
// Node dependency" property every other Cartograph interface has, even
// though building it now needs Node (see docs/adr/0015-react-web-ui.md
// for that tradeoff, revisited from ADR-0013's original no-Node choice).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(dirname, './src'),
    },
  },
  server: {
    // Pinned, not left to Vite's own default-then-auto-increment
    // behavior: with no explicit port, `npm run dev` silently picks
    // 5173, or the next free port after that if something (e.g. a
    // leftover dev server from an earlier session) is already listening
    // on it — a real, recurring annoyance ("no me gusta que cambie
    // siempre"), not a hypothetical one. `strictPort: true` turns that
    // silent drift into a clear, actionable startup error instead
    // ("Port 5173 is already in use") — the fix is "free the port",
    // never "wonder which port it landed on this time".
    port: 5173,
    strictPort: true,
    // In `npm run dev`, proxy API calls to a real ctxd instance (started
    // separately, e.g. `./bin/ctxd --web 127.0.0.1:7420 <path>`) so the
    // Vite dev server only ever serves the frontend, never duplicates the
    // Go backend.
    proxy: {
      '/api': 'http://127.0.0.1:7420',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
