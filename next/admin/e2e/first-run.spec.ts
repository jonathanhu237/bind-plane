import { expect, test } from '@playwright/test'

// This suite is intentionally a required release gate. Unlike the broader
// acceptance suite, it never skips when the setup authority is missing: the
// caller must provide a link read from the freshly started API logs.
const setupURL = process.env.E2E_SETUP_URL
const email = process.env.E2E_ADMIN_EMAIL ?? 'acceptance-admin@example.com'
const password = process.env.E2E_ADMIN_PASSWORD ?? 'Admin1!x'
const name = process.env.E2E_ADMIN_NAME ?? 'Acceptance Admin'

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

test('creates the first administrator and explicitly signs in', async ({ page }) => {
  test.setTimeout(60_000)
  if (!setupURL) throw new Error('E2E_SETUP_URL must contain the setup link from the fresh API log')

  const origin = new URL(setupURL).origin
  await page.goto(setupURL)
  await expect(page.getByRole('heading', { name: /create your administrator account/i })).toBeVisible()
  await expect.poll(() => new URL(page.url()).hash).toBe('')

  await page.getByLabel('Name').fill(name)
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByLabel('Confirm password').fill(password)
  await page.getByRole('button', { name: /create administrator/i }).click()

  // Setup deliberately creates no session. Reaching this page proves the
  // browser completed the real initialization request through the gateway.
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
  await expect(page.getByLabel('Email')).toBeVisible()

  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`^${escapeRegExp(origin)}/$`))

  // Do not couple the gate to a particular home-page heading. The account
  // control is part of the protected shell and only renders after /me has
  // resolved with a valid session.
  const accountButton = page.getByRole('button', { name: new RegExp(escapeRegExp(name)) })
  await expect(accountButton).toBeVisible()
  await expect(page.locator('[data-sidebar="sidebar"]')).toBeVisible()

  // Reload to prove the session cookie, API proxy, and protected route work
  // after the initial navigation rather than only during the login response.
  await page.reload()
  await expect(page).toHaveURL(new RegExp(`^${escapeRegExp(origin)}/$`))
  await expect(accountButton).toBeVisible()
})
