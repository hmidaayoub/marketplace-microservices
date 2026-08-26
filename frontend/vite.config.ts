import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // One of the origins the gateway's CORS allowlist names, so the API is reachable
    // from a cold start with no proxy in between.
    port: 5173,
  },
})
