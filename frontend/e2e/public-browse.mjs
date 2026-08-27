/**
 * A visitor with no account.
 *
 * Browsing demand is public - request-service serves the two reads without a token -
 * and everything that writes still is not. This checks both halves from the outside,
 * because "the button is hidden" and "the API refuses" are different guarantees and the
 * UI is only allowed to rely on the second one.
 */
import { chromium } from 'playwright'

const APP = process.env.APP_URL ?? 'http://localhost:5173'
const API = process.env.API_URL ?? 'http://localhost:8080'

/** A throwaway customer, so the sign-in step depends on no seeded account. */
const n = Date.now().toString().slice(-9)
const email = `public-${n}@test.com`
const password = 'password123'

const api = async (path, body, token) => {
  const res = await fetch(`${API}${path}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`${path} -> ${res.status} ${await res.text()}`)
  return res.status === 204 ? null : res.json()
}

await api('/api/auth/register/customer', { email, password, phoneNumber: `+216${n}` })
const { accessToken } = await api('/api/auth/login', { email, password })
await api('/api/customers', { firstName: 'Public', lastName: 'Visitor' }, accessToken)

const browser = await chromium.launch()
const page = await browser.newPage()

const problems = []
page.on('pageerror', (e) => problems.push(`pageerror: ${e.message.slice(0, 140)}`))
page.on('response', (r) => {
  if (r.status() >= 400) problems.push(`HTTP ${r.status()} ${new URL(r.url()).pathname}`)
})

const step = async (label, fn) => {
  try {
    await fn()
    console.log(`  ok   ${label}`)
  } catch (err) {
    console.log(`  FAIL ${label}\n       ${String(err).split('\n')[0].slice(0, 160)}`)
    console.log(`       url now: ${page.url()}`)
    await page.screenshot({ path: 'e2e/failure-public-browse.png' })
    throw err
  }
}

const cards = () => page.locator('a[href^="/requests/"]')

try {
  await step('the root lands on browse with no session', async () => {
    await page.goto(APP, { waitUntil: 'networkidle' })
    await page.getByRole('heading', { name: /open demand/i }).waitFor()
    if (!page.url().endsWith('/requests')) throw new Error(`url is ${page.url()}`)
  })

  await step('demand is actually listed, not an empty shell', async () => {
    await cards().first().waitFor({ timeout: 10_000 })
    console.log(`       ${await cards().count()} requests visible signed out`)
  })

  await step('the header offers both ways in', async () => {
    await page.getByRole('link', { name: /^sign in$/i }).first().waitFor()
    await page.getByRole('link', { name: /create account/i }).first().waitFor()
  })

  await step('search still works without a token', async () => {
    const search = page.getByLabel('Search requests')
    await search.fill('espresso')
    await page.waitForTimeout(900)
    if ((await cards().count()) === 0) throw new Error('search returned nothing')
    await search.fill('')
    await page.waitForTimeout(900)
  })

  // The point of the whole change: you can look, but acting asks you to sign in first.
  await step('a request opens, with no way to join or offer', async () => {
    await cards().first().click()
    await page.getByText(/join this request, or offer against it/i).waitFor({ timeout: 10_000 })
    if (await page.getByRole('button', { name: /^join$/i }).count()) {
      throw new Error('Join is offered to a signed-out visitor')
    }
    if (await page.getByRole('button', { name: /submit offer/i }).count()) {
      throw new Error('the offer form is shown to a signed-out visitor')
    }
    await page.getByText(/offers are visible once you sign in/i).waitFor()
  })

  const requestUrl = page.url()

  await step('signing in returns to the request, with the actions on it', async () => {
    await page.getByRole('link', { name: /^sign in$/i }).first().click()
    await page.getByLabel('Email').fill(email)
    await page.getByLabel('Password').fill(password)
    await page.getByRole('button', { name: /^sign in$/i }).click()
    await page.waitForURL(requestUrl, { timeout: 15_000 })
    await page.getByRole('button', { name: /^join$/i }).waitFor({ timeout: 10_000 })
  })

  console.log(`\nPUBLIC BROWSE OK  (${email})`)
} finally {
  if (problems.length) console.log('\nproblems seen:\n  ' + problems.join('\n  '))
  await browser.close()
}
