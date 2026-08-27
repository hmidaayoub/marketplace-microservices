import path from 'node:path'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    // The `@/` alias shadcn/ui components import each other through.
    alias: { '@': path.resolve(import.meta.dirname, './src') },
  },
  server: {
    // One of the origins the gateway's CORS allowlist names, so the API is reachable
    // from a cold start with no proxy in between.
    port: 5173,
  },
})
