/**
 * Proves a seller can reach the phone numbers without knowing a request id.
 *
 * The page used to ask them to paste one, which only existed in the address bar of
 * another page - so this checks the whole path from signing in to a number on screen.
 */
import { chromium } from 'playwright'

const APP = process.env.APP_URL ?? 'http://localhost:5173'
const EMAIL = process.env.SELLER_EMAIL
if (!EMAIL) throw new Error('set SELLER_EMAIL')

const browser = await chromium.launch()
const page = await browser.newPage()
const problems = []
page.on('pageerror', (e) => problems.push(`pageerror: ${e.message.slice(0, 140)}`))
page.on('response', (r) => {
  if (r.status() >= 400) problems.push(`HTTP ${r.status()} ${new URL(r.url()).pathname}`)
})

try {
  await page.goto(`${APP}/login`, { waitUntil: 'networkidle' })
  await page.getByLabel('Email').fill(EMAIL)
  await page.getByLabel('Password').fill('password123')
  await page.getByRole('button', { name: /sign in/i }).click()
  await page.getByRole('heading', { name: /open demand/i }).waitFor({ timeout: 10_000 })
  console.log('  ok   signed in as the seller')

  await page.getByRole('link', { name: /contacts/i }).click()
  await page.getByRole('heading', { name: /customer contacts/i }).waitFor()
  console.log('  ok   opened Contacts — no id typed')

  await page.getByRole('button', { name: /show contacts/i }).first().click()
  const phone = page.locator('a[href^="tel:"]').first()
  await phone.waitFor({ timeout: 10_000 })
  const rows = await page.locator('li:has(a[href^="tel:"])').allTextContents()
  console.log('  ok   contacts shown:')
  for (const row of rows) console.log(`         ${row.replace(/\s+/g, ' ').trim()}`)
  // The seller is calling a person, so the row has to name one - an id alone was the
  // original defect this test exists to prevent.
  if (rows.some((r) => /^\s*\+?\d/.test(r.trim()))) throw new Error('a row has no name')
  console.log('\nCONTACTS FLOW OK')
} catch (err) {
  console.log(`  FAIL ${String(err).split('\n')[0].slice(0, 160)}`)
  await page.screenshot({ path: 'e2e/failure-contacts.png' })
  process.exitCode = 1
} finally {
  if (problems.length) console.log('\nproblems seen:\n  ' + problems.join('\n  '))
  await browser.close()
}
