/**
 * Seeds a scenario for testing the UI by hand.
 *
 * Testing this platform means being three people at once - a customer who opens demand,
 * a seller who offers against it, an admin who approves - and registering all of them
 * through the forms every time is most of the effort of a test run. This creates them,
 * takes the scenario as far as you ask, and prints the credentials to sign in with.
 *
 *   node scripts/seed-ui.mjs             # accounts, a request with two buyers, an offer
 *   node scripts/seed-ui.mjs --stage=request   # stop before the offer
 *   node scripts/seed-ui.mjs --stage=accounts  # accounts and profiles only
 *
 * Every run makes new accounts: email and phone are both unique-constrained, so reusing
 * them is the 409 that looks like a broken signup.
 */

const BASE = process.env.API_URL ?? 'http://localhost:8080'
const PASSWORD = 'password123'
const STAGE = (process.argv.find((a) => a.startsWith('--stage=')) ?? '--stage=offer').split('=')[1]
const n = Date.now().toString().slice(-9)

async function call(path, { method = 'GET', body, token } = {}) {
  const headers = {}
  if (body) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = `Bearer ${token}`
  const res = await fetch(BASE + path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  if (!res.ok) {
    throw new Error(`${method} ${path} -> ${res.status} ${text.slice(0, 160)}`)
  }
  return text ? JSON.parse(text) : null
}

async function account(kind, index, profile) {
  const email = `${kind}${index}-${n}@test.com`
  const phone = `+216${index}${n.slice(0, 7)}`
  await call(`/api/auth/register/${kind}`, {
    method: 'POST',
    body: { email, password: PASSWORD, phoneNumber: phone },
  })
  const { accessToken } = await call('/api/auth/login', {
    method: 'POST',
    body: { email, password: PASSWORD },
  })
  // The profile is a separate step: registering makes a user, and every business write
  // answers 403 until the role profile behind it exists.
  await call(kind === 'seller' ? '/api/sellers' : '/api/customers', {
    method: 'POST',
    body: profile,
    token: accessToken,
  })
  return { email, phone, token: accessToken }
}

try {
  const customer = await account('customer', 1, { firstName: 'Amine', lastName: 'Hmida' })
  const customer2 = await account('customer', 2, { firstName: 'Second', lastName: 'Buyer' })
  const seller = await account('seller', 3, {
    storeName: 'Amine Store',
    description: 'Electronics',
    city: 'Tunis',
    address: '12 Rue Example',
  })

  let request = null
  let offer = null

  if (STAGE !== 'accounts') {
    request = await call('/api/requests', {
      method: 'POST',
      body: {
        // Unique per run, like the emails above. Only one OPEN request per item may
        // exist and this script deliberately stops before any approval, so its own
        // request stays OPEN - a fixed name would 409 on the second run and take
        // scripts/smoke.sh down with it.
        itemName: `Espresso Machine ${n}`,
        description: 'bar grade, for a small cafe',
        category: 'kitchen',
        quantity: 3,
      },
      token: customer.token,
    })
    // A second buyer, so the aggregated demand on screen is not just one person's.
    await call(`/api/requests/${request.requestId}/participants`, {
      method: 'POST',
      body: { quantity: 5 },
      token: customer2.token,
    })
  }

  if (STAGE === 'offer' && request) {
    offer = await call('/api/offers', {
      method: 'POST',
      body: {
        requestId: request.requestId,
        availableQuantity: 8,
        pricePerUnit: '149.99',
        currency: 'EUR',
        description: 'Sealed, 24 month warranty',
      },
      token: seller.token,
    })
  }

  const row = (label, value) => console.log(`  ${label.padEnd(12)} ${value}`)
  console.log('\n  Sign in at http://localhost:5173 — password for all three:', PASSWORD, '\n')
  row('CUSTOMER', customer.email)
  row('CUSTOMER 2', customer2.email)
  row('SELLER', seller.email)
  row('ADMIN', 'admin@marketplace.local  (password: admin-dev-password)')
  if (request) {
    console.log()
    row('request', `${request.requestId}`)
    console.log(`  ${''.padEnd(12)} 2 buyers, ${request.totalQuantity ?? 8} units — /requests/${request.requestId}`)
  }
  if (offer) {
    console.log()
    row('offer', offer.offerId)
    console.log('               waiting in the admin queue — approve it to release the contacts')
  }
  console.log(
    `\n  Next: ${
      STAGE === 'offer'
        ? 'sign in as ADMIN, approve the offer, then sign in as SELLER and open Contacts.'
        : STAGE === 'accounts'
          ? 'sign in as CUSTOMER and create a request.'
          : 'sign in as SELLER and make an offer on the request.'
    }\n`,
  )
} catch (error) {
  console.error('\n  seeding failed:', error.message)
  console.error('  is the stack up?  docker compose ps   /   ./scripts/smoke.sh\n')
  process.exit(1)
}
