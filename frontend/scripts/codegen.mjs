/**
 * Regenerates src/api/schema/ from the seven OpenAPI documents the gateway serves.
 *
 * Run through npx rather than a local dependency: openapi-typescript pins TypeScript
 * ^5 as a peer and this project is on 6, so installing it would either fail or force a
 * resolution nobody wants. It is a build-time tool that runs when a service changes -
 * the same shape as `swag init` on the Go side - so fetching it on demand is right.
 */

import { execFileSync } from 'node:child_process'

const BASE = process.env.API_URL ?? 'http://localhost:8080'
const SERVICES = ['auth', 'customer', 'seller', 'request', 'offer', 'admin', 'notification']

try {
  await fetch(`${BASE}/health`)
} catch {
  console.error(`\n  ${BASE} is not answering — start the stack first:\n`)
  console.error('    docker compose up -d\n')
  process.exit(1)
}

for (const service of SERVICES) {
  execFileSync(
    'npx',
    ['-y', 'openapi-typescript@7', `${BASE}/openapi/${service}.json`, '-o', `src/api/schema/${service}.ts`],
    { stdio: 'inherit' },
  )
}
