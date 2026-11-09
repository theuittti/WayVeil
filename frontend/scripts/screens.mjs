// Screenshots of the mock UI at 380x640. Usage (see README in the task notes):
//   BASE=http://localhost:4173 OUT=./screens node scripts/screens.mjs
// Needs `playwright` and a browser; run it in the official Playwright container.
import { chromium } from 'playwright'

const base = process.env.BASE ?? 'http://localhost:4173'
const out = process.env.OUT ?? './screens'
const browser = await chromium.launch()
const page = await (await browser.newContext({ viewport: { width: 380, height: 640 } })).newPage()
const shot = (name) => page.screenshot({ path: `${out}/${name}.png` })
const click = (text) => page.getByRole('button', { name: text }).first().click()
const sleep = (ms) => page.waitForTimeout(ms)

async function toAccess(host) {
  await page.goto(base)
  await page.getByText('Paste a connection link').waitFor()
  await click(/own server/)
  await page.getByLabel('IP or address').fill(host)
  await page.getByLabel('Password').fill('secret')
}

await page.goto(base)
await page.getByText('Paste a connection link').waitFor()
await shot('01-paste')

await toAccess('203.0.113.7')
await shot('02-access')
await click(/^Connect$/)
await page.getByText('Is this your server?').waitFor()
await shot('03-access-fingerprint')
await click('Yes, continue')
await page.getByText('Step 2 of 3').waitFor()
await sleep(1200)
await shot('04-target-scanning')
await sleep(3500)
await page.getByPlaceholder('Your own domain…').fill('bad.example')
await click('Check and use')
await page.getByText('Use anyway').waitFor()
await shot('05-target-warning')
await click('Use anyway')
await sleep(200)
await shot('06-target-selected')
await click('Next')
await sleep(1900)
await click(/Show detailed log/)
await shot('07-installing')
await page.getByText('Server is ready').waitFor({ timeout: 10000 })
await shot('08-done')
await click(/^Connect$/)
await page.getByText('Connected', { exact: true }).waitFor({ timeout: 5000 })
await sleep(2200)
await shot('09-main-connected')
await click('Menu')
await shot('10-main-menu')

await toAccess('fail.example')
await click(/^Connect$/); await click('Yes, continue')
await page.getByText('Step 2 of 3').waitFor()
await click('Next')
await page.getByText('Clean up and install again').waitFor({ timeout: 10000 })
await shot('11-installing-failed')

await toAccess('wayveil.example')
await click(/^Connect$/); await click('Yes, continue')
await page.getByText(/already installed/).waitFor()
await shot('12-existing')

await toAccess('partial.example')
await click(/^Connect$/); await click('Yes, continue')
await page.getByText(/did not finish/).first().waitFor()
await shot('13-existing-partial')

await toAccess('busy.example')
await click(/^Connect$/); await click('Yes, continue')
await page.getByText(/Port 443/).waitFor()
await shot('14-refused')

await browser.close()
