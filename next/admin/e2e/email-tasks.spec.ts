import { expect, test } from '@playwright/test'

const enabled = process.env.E2E_EMAIL_TASKS === '1'
const email = process.env.E2E_ADMIN_EMAIL ?? 'admin@example.com'
const password = process.env.E2E_ADMIN_PASSWORD ?? 'Admin1!x'
const mailpitURL = process.env.E2E_MAILPIT_API_URL

async function signIn(page: import('@playwright/test').Page): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/$/)
}

async function saveSMTP(page: import('@playwright/test').Page, options: { host: string; port: number; retryCount: number }): Promise<void> {
  await page.goto('/settings')
  const section = page.locator('section[aria-labelledby="email-settings-title"]')
  await expect(section.getByRole('heading', { name: 'Email delivery', exact: true })).toBeVisible()
  await section.getByLabel('SMTP host').fill(options.host)
  await section.getByLabel('Port').fill(String(options.port))
  await section.getByRole('combobox', { name: 'Security' }).click()
  await page.getByRole('option', { name: 'None', exact: true }).click()
  await section.getByLabel('From address').fill('no-reply@example.com')
  await section.getByLabel('From name').fill('Temvia')
  await section.getByRole('combobox', { name: 'Default email language' }).click()
  await page.getByRole('option', { name: 'English', exact: true }).click()
  await section.getByLabel('Automatic retry count').fill(String(options.retryCount))
  await section.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText('Email settings saved.', { exact: true })).toBeVisible()
}

async function submitTestEmail(page: import('@playwright/test').Page, recipient: string): Promise<void> {
  const section = page.locator('section[aria-labelledby="email-settings-title"]')
  await section.getByRole('button', { name: 'Send test email', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Send test email' })
  await dialog.getByLabel('Recipient email').fill(recipient)
  await dialog.getByRole('button', { name: 'Send test email', exact: true }).click()
  await expect(page.getByLabel('Notifications alt+T').getByText('Test email submitted. Delivery is still in progress.', { exact: true })).toBeVisible()
}

async function expectMailpitDelivery(request: import('@playwright/test').APIRequestContext, recipient: string): Promise<void> {
  if (!mailpitURL) throw new Error('E2E_MAILPIT_API_URL is required for Mailpit delivery assertions')
  await expect.poll(async () => {
    const response = await request.get(`${mailpitURL}/api/v1/search?query=to:${encodeURIComponent(recipient)}`)
    if (!response.ok()) return 0
    const payload = await response.json() as { messages_count?: number }
    return payload.messages_count ?? 0
  }, { timeout: 60_000 }).toBeGreaterThan(0)
}

test.use({ trace: 'off', screenshot: 'off', video: 'off' })
test.describe('email tasks', () => {
  test.describe.configure({ mode: 'serial' })
  test('saves SMTP, submits an asynchronous test task, and observes delivery', async ({ page, request }) => {
    test.skip(!enabled || !mailpitURL, 'Set E2E_EMAIL_TASKS=1 and E2E_MAILPIT_API_URL against an isolated installation.')
    test.setTimeout(120_000)
    await signIn(page)

    await saveSMTP(page, { host: process.env.E2E_SMTP_HOST ?? 'mailpit', port: Number(process.env.E2E_SMTP_PORT ?? 1025), retryCount: Number(process.env.E2E_SMTP_RETRY_COUNT ?? 9) })
    const recipient = `mail-task-success-${Date.now()}@example.com`
    await submitTestEmail(page, recipient)

    await page.goto('/email-tasks')
    await expect(page.getByRole('heading', { name: 'Email tasks', exact: true })).toBeVisible()
    await expect(page.getByText(recipient, { exact: true })).toBeVisible()
    const row = page.locator('tbody tr').filter({ hasText: recipient })
    await expect(row.getByText('Sent by SMTP', { exact: true })).toBeVisible({ timeout: 60_000 })
    await expectMailpitDelivery(request, recipient)
  })

  test('records a terminal failure, retries after a saved SMTP repair, reports bulk conflicts, and deletes physically', async ({ page, request }) => {
    test.skip(!enabled || !mailpitURL, 'Set E2E_EMAIL_TASKS=1 and E2E_MAILPIT_API_URL against an isolated installation.')
    test.setTimeout(240_000)
    await signIn(page)

    const goodHost = process.env.E2E_SMTP_HOST ?? 'mailpit'
    const goodPort = Number(process.env.E2E_SMTP_PORT ?? 1025)
    const badHost = process.env.E2E_BAD_SMTP_HOST ?? '127.0.0.1'
    const badPort = Number(process.env.E2E_BAD_SMTP_PORT ?? 1)
    const failedRecipient = `mail-task-failed-${Date.now()}@example.com`
    const secondFailedRecipient = `mail-task-failed-bulk-${Date.now()}@example.com`
    const bulkSentRecipient = `mail-task-bulk-sent-${Date.now()}@example.com`

    await saveSMTP(page, { host: goodHost, port: goodPort, retryCount: 0 })
    await page.goto('/settings')
    const settingsSection = page.locator('section[aria-labelledby="email-settings-title"]')
    await settingsSection.getByLabel('SMTP host').fill('unsaved-mailpit-host')
    await settingsSection.getByRole('button', { name: 'Send test email', exact: true }).click()
    await expect(page.getByText('Save your SMTP changes before submitting a test email.', { exact: true })).toBeVisible()

    await saveSMTP(page, { host: badHost, port: badPort, retryCount: 0 })
    await submitTestEmail(page, failedRecipient)
    await page.goto('/email-tasks')
    let failedRow = page.locator('tbody tr').filter({ hasText: failedRecipient })
    await expect(failedRow.getByText('Failed', { exact: true })).toBeVisible({ timeout: 90_000 })
    await expect(failedRow.getByRole('button', { name: 'Retry', exact: true })).toBeVisible()

    await saveSMTP(page, { host: goodHost, port: goodPort, retryCount: 0 })
    await page.goto('/email-tasks')
    failedRow = page.locator('tbody tr').filter({ hasText: failedRecipient })
    await failedRow.getByRole('button', { name: 'Retry', exact: true }).click()
    await expect(page.getByText('Email task retry submitted.', { exact: true })).toBeVisible()
    await expect(failedRow.getByText('Sent by SMTP', { exact: true })).toBeVisible({ timeout: 60_000 })
    await expectMailpitDelivery(request, failedRecipient)

    await saveSMTP(page, { host: badHost, port: badPort, retryCount: 0 })
    await submitTestEmail(page, secondFailedRecipient)
    await page.goto('/email-tasks')
    const secondFailedRow = page.locator('tbody tr').filter({ hasText: secondFailedRecipient })
    await expect(secondFailedRow.getByText('Failed', { exact: true })).toBeVisible({ timeout: 90_000 })

    await saveSMTP(page, { host: goodHost, port: goodPort, retryCount: 0 })
    await submitTestEmail(page, bulkSentRecipient)
    await page.goto('/email-tasks')
    const bulkSentRow = page.locator('tbody tr').filter({ hasText: bulkSentRecipient })
    await expect(bulkSentRow.getByText('Sent by SMTP', { exact: true })).toBeVisible({ timeout: 60_000 })
    await expectMailpitDelivery(request, bulkSentRecipient)

    await secondFailedRow.getByRole('checkbox', { name: `Select ${secondFailedRecipient}` }).check()
    await bulkSentRow.getByRole('checkbox', { name: `Select ${bulkSentRecipient}` }).check()
    await page.getByRole('button', { name: 'Retry selected', exact: true }).click()
    await expect(page.getByText(/1 succeeded, 1 skipped, 0 failed\./)).toBeVisible()

    await bulkSentRow.getByRole('button', { name: 'Delete', exact: true }).click()
    const confirmation = page.getByRole('dialog', { name: 'Delete email task?' })
    await expect(confirmation).toBeVisible()
    await confirmation.getByRole('button', { name: 'Delete', exact: true }).click()
    await expect(page.getByText('Email task deleted.', { exact: true })).toBeVisible()
    await expect(page.locator('tbody tr').filter({ hasText: bulkSentRecipient })).toHaveCount(0)
  })
})
