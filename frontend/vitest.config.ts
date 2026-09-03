/**
 * Two suites, run and reported separately because they answer different questions and
 * fail for different reasons.
 *
 *   unit      - the logic modules: reducers, selectors, the API client, the refresh
 *               hook. No React, no rendering.
 *   component - components mounted with Testing Library and asserted through what a
 *               user can see.
 *
 * The convention is the file extension, which is also what the code under test uses:
 * *.test.ts is logic, *.test.tsx is a component. There is no glob to keep in sync with
 * a directory layout, and a test cannot end up in the wrong suite without changing the
 * language it is written in.
 *
 * Both run in jsdom rather than one in node, because "unit" here does not mean "pure".
 * The units are browser modules: authSlice reads and writes localStorage on import,
 * and the API client builds FormData and calls fetch. Giving the logic suite a node
 * environment would mean hand-stubbing the four browser APIs jsdom already provides,
 * and testing the stubs as much as the code.
 */

import path from 'node:path'

import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  resolve: {
    // The same `@/` the app and shadcn/ui import through. Duplicated from vite.config
    // rather than shared, because vitest loads this file instead of that one.
    alias: { '@': path.resolve(import.meta.dirname, './src') },
  },
  test: {
    // No globals. `describe` and `expect` are imported in every test file, which costs
    // one line and buys the ability to read a test without knowing what the runner
    // injected - and lets ESLint check the names rather than being told to ignore them.
    globals: false,
    projects: [
      {
        extends: true,
        test: {
          name: 'unit',
          environment: 'jsdom',
          include: ['src/**/*.test.ts'],
        },
      },
      {
        extends: true,
        test: {
          name: 'component',
          environment: 'jsdom',
          include: ['src/**/*.test.tsx'],
          // Only this suite needs the DOM matchers and the between-tests unmount.
          setupFiles: ['src/test/setup.ts'],
        },
      },
    ],
  },
})
