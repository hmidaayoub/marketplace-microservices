/**
 * Drives the real UI in a real browser: register -> profile -> create a request.
 *
 * Exists because "account creation did not work" was diagnosed three times from the
 * outside - the API passed, the store passed, and the fault was still in front of the
 * user. This is the only check that sees what they see.
 */
import { chromium } from 'playwright'

const APP = process.env.APP_URL ?? 'http://localhost:5173'
const n = Date.now().toString().slice(-9)
const email = `e2e-${n}@test.com`
const phone = `+216${n}`
const item = `Espresso Machine ${n}`

const browser = await chromium.launch()
const page = await browser.newPage()

const problems = []
page.on('console', (m) => {
  // The text of a failed-request console error names no URL, so the filter below could
  // never match one and every expected 404 was reported twice as a problem. The
  // location carries the URL the message is about; keep it.
  if (m.type() !== 'error') return
  const from = m.location()?.url
  problems.push(`console: ${m.text().slice(0, 160)}${from ? ` (${from})` : ''}`)
})
page.on('pageerror', (e) => problems.push(`pageerror: ${e.message.slice(0, 160)}`))
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
    await page.screenshot({ path: `e2e/failure-${label.replace(/\W+/g, '-')}.png` })
    throw err
  }
}

try {
  await step('open /register', async () => {
    await page.goto(`${APP}/register`, { waitUntil: 'networkidle' })
    await page.getByRole('heading', { name: /create an account/i }).waitFor()
  })

  await step('fill and submit the form', async () => {
    await page.getByLabel('Email').fill(email)
    await page.getByLabel('Password').fill('password123')
    await page.getByLabel('Phone number').fill(phone)
    await page.getByRole('button', { name: /create account/i }).click()
  })

  // The registration path deliberately lands on the profile form, not the request
  // list: the account exists but the profile every business write needs does not.
  await step('lands on the profile form', async () => {
    await page.getByRole('heading', { name: /complete your profile/i }).waitFor({ timeout: 10_000 })
  })

  await step('submit the profile', async () => {
    await page.getByLabel('First name').fill('Amine')
    await page.getByLabel('Last name').fill('Hmida')
    await page.getByRole('button', { name: /continue/i }).click()
    await page.getByRole('heading', { name: /open demand/i }).waitFor({ timeout: 10_000 })
  })

  // The form asks for an item and a quantity and nothing else: category and description
  // went when one open request per item did, and demand pools on the name alone.
  //
  // The name has to be unique per run. An item that is already open demand is the case
  // the dialog withholds "Create request" for entirely - it offers joining instead - so
  // a fixed name would pass once and then test the wrong branch forever.
  await step('create a request', async () => {
    await page.getByRole('button', { name: /new request/i }).click()
    await page.getByLabel('Item').fill(item)
    await page.getByLabel('How many you want').fill('3')
    // The submit button relabels to "Create a separate request" as soon as the name
    // matches open demand, which a unique name still does loosely - earlier runs of this
    // very script are what it looks like. Either label is the same button.
    await page.getByRole('button', { name: /^create (a separate )?request$/i }).click()
    await page.getByText(item).first().waitFor({ timeout: 10_000 })
  })

  console.log(`\nSIGNUP FLOW OK  (${email})`)
} finally {
  // 404 on /api/customers/me before the profile exists is the expected probe, not a fault.
  const real = problems.filter((p) => !p.includes('/api/customers/me'))
  if (real.length) console.log('\nproblems seen:\n  ' + real.join('\n  '))
  await browser.close()
}
