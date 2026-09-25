import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

const setupURL = process.env.E2E_SETUP_URL
const mailpitAPIURL = process.env.E2E_MAILPIT_API_URL
const email = process.env.E2E_ADMIN_EMAIL ?? 'admin@example.com'
const password = process.env.E2E_ADMIN_PASSWORD ?? 'Admin1!x'
const name = process.env.E2E_ADMIN_NAME ?? 'Admin User'
const resetPassword = process.env.E2E_RESET_PASSWORD ?? 'NewAdmin1!x'
const mailpitPollTimeout = 30_000

type MailpitMessage = Record<string, unknown>

function mailpitURL(path: string): string {
  if (!mailpitAPIURL) throw new Error('E2E_MAILPIT_API_URL is not configured')
  const base = new URL(mailpitAPIURL)
  const [pathname, search = ''] = path.split('?', 2)
  let prefix = base.pathname.replace(/\/+$/, '')
  if (!prefix.endsWith('/api/v1')) prefix += '/api/v1'
  base.pathname = `${prefix}/${pathname.replace(/^\/+/, '')}`
  base.search = search ? `?${search}` : ''
  return base.toString()
}

function mailpitMessageID(message: MailpitMessage): string | undefined {
  const id = message.ID ?? message.id
  return typeof id === 'string' ? id : undefined
}

async function listMailpitMessages(request: APIRequestContext): Promise<MailpitMessage[]> {
  const response = await request.get(mailpitURL('messages?limit=200'), { timeout: 5_000 })
  if (!response.ok()) throw new Error(`Mailpit message list failed (${response.status()})`)
  const payload = (await response.json()) as unknown
  if (Array.isArray(payload)) return payload.filter(isMailpitMessage)
  if (typeof payload !== 'object' || payload === null) return []
  const messages = (payload as { messages?: unknown }).messages
  return Array.isArray(messages) ? messages.filter(isMailpitMessage) : []
}

async function getMailpitMessage(request: APIRequestContext, id: string): Promise<MailpitMessage | undefined> {
  const response = await request.get(mailpitURL(`message/${encodeURIComponent(id)}`), { timeout: 5_000 })
  if (!response.ok()) return undefined
  const payload = (await response.json()) as unknown
  return isMailpitMessage(payload) ? payload : undefined
}

function isMailpitMessage(value: unknown): value is MailpitMessage {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

async function waitForNewMail(
  request: APIRequestContext,
  previousIDs: Set<string>,
  predicate: (message: MailpitMessage) => boolean,
): Promise<MailpitMessage> {
  const deadline = Date.now() + mailpitPollTimeout
  let lastError = 'no matching message'
  while (Date.now() < deadline) {
    try {
      const summaries = await listMailpitMessages(request)
      for (const summary of summaries) {
        const id = mailpitMessageID(summary)
        if (!id || previousIDs.has(id)) continue
        const message = await getMailpitMessage(request, id)
        if (message && predicate(message)) return message
      }
    } catch (error) {
      lastError = error instanceof Error ? error.message : 'Mailpit request failed'
    }
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  throw new Error(`Timed out waiting for Mailpit message: ${lastError}`)
}

function mailpitBody(message: MailpitMessage): string {
  const text = typeof message.Text === 'string' ? message.Text : ''
  const html = typeof message.HTML === 'string' ? message.HTML : ''
  return `${text}\n${html}`
}

function mailpitContainsRecipient(message: MailpitMessage, recipient: string): boolean {
  return JSON.stringify(message.To ?? '').toLowerCase().includes(recipient.toLowerCase())
}

function extractResetLink(message: MailpitMessage): { link: string; token: string } {
  const tokenPattern = 'v1\\.[A-Za-z0-9_-]{22}\\.[A-Za-z0-9_-]{43}'
  const match = mailpitBody(message).match(new RegExp(`https?://[^\\s<>"']+/reset-password#token=(${tokenPattern})`))
  if (!match) throw new Error('Mailpit reset message did not contain a canonical fragment link')
  return { link: match[0], token: match[1] }
}

async function signIn(page: Page, userEmail: string, userPassword: string, origin?: string): Promise<void> {
  await page.goto(origin ? `${origin}/login` : '/login')
  await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
  await page.getByLabel('Email').fill(userEmail)
  await page.getByLabel('Password', { exact: true }).fill(userPassword)
  await page.getByRole('button', { name: /sign in/i }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: 'Home' })).toBeVisible()
}

async function selectPreference(page: Page, buttonName: string, option: string): Promise<void> {
  await page.getByRole('button', { name: buttonName, exact: true }).click()
  const optionItem = page.getByRole('menuitemradio', { name: option, exact: true })
  await expect(optionItem).toBeVisible()
  await optionItem.click()
}

async function expectResetAuthorityNotPersisted(page: Page, token: string): Promise<void> {
  await expect.poll(() => new URL(page.url()).hash).toBe('')
  const browserState = await page.evaluate(() => ({
    hash: window.location.hash,
    localStorage: JSON.stringify(localStorage),
    sessionStorage: JSON.stringify(sessionStorage),
    cookie: document.cookie,
    historyState: JSON.stringify(window.history.state),
  }))
  const contextCookies = await page.context().cookies()
  const serializedState = `${JSON.stringify(browserState)}${JSON.stringify(contextCookies)}`
  expect(browserState.hash).toBe('')
  expect(serializedState).not.toContain(token)
}

function collectBrowserErrors(page: Page, options: { allowExpectedLogoutFailure?: boolean } = {}): string[] {
  const errors: string[] = []
  page.on('console', (message) => {
    if (message.type() !== 'error') return

    const sourceURL = message.location().url
    const isExpectedAuth401 = message.text().includes('401')
      && sourceURL.length > 0
      && ['/api/auth/me', '/api/auth/login'].includes(new URL(sourceURL).pathname)
    const isExpectedSetup403 = message.text().includes('403')
      && sourceURL.length > 0
      && new URL(sourceURL).pathname === '/api/setup'
    const isExpectedLogout503 = options.allowExpectedLogoutFailure === true
      && message.text().includes('503')
      && sourceURL.length > 0
      && new URL(sourceURL).pathname === '/api/auth/logout'

    if (!isExpectedAuth401 && !isExpectedSetup403 && !isExpectedLogout503) errors.push(message.text())
  })
  page.on('pageerror', (error) => errors.push(error.message))
  return errors
}

async function expectNoA11yViolations(page: Page): Promise<void> {
  const accessibility = await new AxeBuilder({ page }).analyze()
  expect(accessibility.violations).toEqual([])
}

test.describe('administrator authentication', () => {
  test.describe.configure({ mode: 'serial' })

  test('keeps the normal login form available before setup', async ({ page }) => {
    test.skip(!setupURL, 'Set E2E_SETUP_URL to a fresh setup URL from the API log.')
    const browserErrors = collectBrowserErrors(page)
    const setupStatusRequests: string[] = []
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === '/api/setup/status') setupStatusRequests.push(request.url())
    })

    await page.goto(new URL('/setup', setupURL!).toString())
    await expect(page).toHaveURL(/\/login$/)
    await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
    expect(setupStatusRequests).toEqual([])

    await page.goto(new URL('/login', setupURL!).toString())
    await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
    await expect(page.getByLabel('Email')).toBeVisible()
    await expect(page.getByLabel('Password', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
    await expect(page.getByText(/finish initialization|open the latest setup link/i)).toHaveCount(0)
    await expectNoA11yViolations(page)

    await page.getByLabel('Email').fill('unknown@example.com')
    await page.getByLabel('Password', { exact: true }).fill('not-a-real-password')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByRole('alert')).toHaveText('The email or password is incorrect.')

    await selectPreference(page, 'Language settings', '简体中文')
    await expect(page.getByRole('alert')).toHaveText('邮箱或密码不正确。')
    await expect(page.getByLabel('邮箱')).toHaveValue('unknown@example.com')
    await expect(page.getByLabel('密码', { exact: true })).toHaveValue('not-a-real-password')

    await selectPreference(page, '语言设置', 'English')
    await expect(page.getByRole('alert')).toHaveText('The email or password is incorrect.')
    expect(setupStatusRequests).toEqual([])
    expect(browserErrors).toEqual([])
  })

  test('returns to login when the setup authority is rejected', async ({ page }) => {
    test.skip(!setupURL, 'Set E2E_SETUP_URL to a fresh setup URL from the API log.')
    const browserErrors = collectBrowserErrors(page)
    const invalidToken = 'A'.repeat(43)

    await page.goto(`${new URL(setupURL!).origin}/setup#token=${invalidToken}`)
    await expect(page.getByRole('heading', { name: /create your administrator account/i })).toBeVisible()
    await page.getByLabel('Name').fill('Admin User')
    await page.getByLabel('Email').fill('invalid@example.com')
    await page.getByLabel('Password', { exact: true }).fill('Admin1!x')
    await page.getByLabel('Confirm password').fill('Admin1!x')
    await page.getByRole('button', { name: /create administrator/i }).click()

    await expect(page).toHaveURL(/\/login$/)
    await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
    await expect(page.getByText(/finish initialization|open the latest setup link/i)).toHaveCount(0)
    expect(browserErrors).toEqual([])
  })

  test('initializes, signs in, restores the session, changes locale and logs out', async ({ page }) => {
    test.skip(!setupURL, 'Set E2E_SETUP_URL to a fresh setup URL from the API log.')
    const browserErrors = collectBrowserErrors(page, { allowExpectedLogoutFailure: true })
    const setupStatusRequests: string[] = []
    page.on('request', (request) => {
      if (new URL(request.url()).pathname === '/api/setup/status') setupStatusRequests.push(request.url())
    })
    const setupToken = new URL(setupURL!).hash.slice('#token='.length)

    await page.goto(setupURL!)
    await expect.poll(() => new URL(page.url()).hash).toBe('')
    const storage = await page.evaluate(() => `${JSON.stringify(localStorage)}${JSON.stringify(sessionStorage)}${document.cookie}`)
    expect(storage).not.toContain(setupToken)
    await expect(page.getByRole('heading', { name: /create your administrator account/i })).toBeVisible()
    await expect(page.getByRole('heading')).toHaveCount(1)
    await expect(page.locator('[data-slot="field-description"]')).toHaveCount(0)
    await expect(page.locator('main > div').getByRole('button', { name: 'Appearance settings' })).toBeVisible()
    await expect(page.locator('main > div').getByRole('button', { name: 'Language settings' })).toBeVisible()
    await expectNoA11yViolations(page)

    const setupStatusRequestsBeforeReload = setupStatusRequests.length
    await page.reload()
    await expect(page).toHaveURL(/\/login$/)
    await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
    await expect(page.getByLabel('Email')).toBeVisible()
    expect(setupStatusRequests).toHaveLength(setupStatusRequestsBeforeReload)

    await page.goto(setupURL!)
    await expect.poll(() => new URL(page.url()).hash).toBe('')
    await expect(page.getByRole('heading', { name: /create your administrator account/i })).toBeVisible()
    await page.getByLabel('Name').fill(name)
    await page.getByLabel('Email').fill(email)
    await page.getByLabel('Password', { exact: true }).fill(password)
    await page.getByLabel('Confirm password').fill(password)
    await page.getByRole('button', { name: /create administrator/i }).click()
    await expect(page).toHaveURL(/\/login$/)
    await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
    await expect(page.getByRole('heading')).toHaveCount(1)
    await expect(page.locator('[data-slot="field-description"]')).toHaveCount(0)
    await expectNoA11yViolations(page)

    await page.getByLabel('Email').fill(email)
    await page.getByLabel('Password', { exact: true }).fill(password)
    await page.getByRole('button', { name: /sign in/i }).click()
    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByRole('heading', { name: 'Home' })).toBeVisible()
    const accountButton = page.getByRole('button', { name: new RegExp(name) })
    await expect(accountButton).toBeVisible()
    await accountButton.click()
    await expect(page.getByRole('menuitem', { name: /log out/i })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.locator('[data-sidebar="header"]')).toHaveCount(0)

    const header = page.locator('header')
    const appearanceButton = header.getByRole('button', { name: 'Appearance settings', exact: true })
    const languageButton = header.getByRole('button', { name: 'Language settings', exact: true })
    await expect(appearanceButton).toBeVisible()
    await expect(languageButton).toBeVisible()
    const appearanceBox = await appearanceButton.boundingBox()
    const languageBox = await languageButton.boundingBox()
    expect(appearanceBox).not.toBeNull()
    expect(languageBox).not.toBeNull()
    expect(appearanceBox!.x).toBeLessThan(languageBox!.x)
    const viewportWidthBeforeMenu = await page.evaluate(() => document.documentElement.clientWidth)
    await appearanceButton.click()
    const menuScrollState = await page.evaluate(() => ({
      bodyOverflow: getComputedStyle(document.body).overflow,
      clientWidth: document.documentElement.clientWidth,
    }))
    expect(menuScrollState.bodyOverflow).not.toBe('hidden')
    expect(menuScrollState.clientWidth).toBe(viewportWidthBeforeMenu)
    await page.getByRole('menuitemradio', { name: 'Dark', exact: true }).click()
    await expect(page.locator('html')).toHaveClass(/dark/)
    await expect(appearanceButton).toBeFocused()

    const sidebarBox = await page.locator('[data-sidebar="sidebar"]:visible').first().boundingBox()
    const headingBox = await page.getByRole('heading', { name: 'Home' }).boundingBox()
    expect(sidebarBox).not.toBeNull()
    expect(headingBox).not.toBeNull()
    expect(headingBox!.x).toBeGreaterThanOrEqual(sidebarBox!.x + sidebarBox!.width)

    await expectNoA11yViolations(page)

    await page.goto('/login')
    await expect(page).toHaveURL(/\/$/)
    await expect(page.getByRole('heading', { name: 'Home' })).toBeVisible()

    await page.reload()
    await expect(page.getByRole('heading', { name: 'Home' })).toBeVisible()
    await page.getByRole('button', { name: new RegExp(name) }).click()
    await expect(page.getByRole('menuitem', { name: /退出登录|log out/i })).toBeVisible()
    await expect(page.getByRole('menuitem', { name: /Appearance settings|Language settings/ })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await selectPreference(page, 'Language settings', '简体中文')
    await expect(page.getByRole('heading', { name: '首页' })).toBeVisible()

    await page.setViewportSize({ width: 390, height: 844 })
    await page.getByRole('button', { name: '菜单' }).click()
    await expect(page.getByRole('link', { name: '首页' })).toBeVisible()
    await page.setViewportSize({ width: 1280, height: 900 })
    await page.reload()

    let logoutAttempts = 0
    await page.route('**/api/auth/logout', async (route) => {
      logoutAttempts += 1
      if (logoutAttempts === 1) {
        await route.fulfill({
          status: 503,
          contentType: 'application/problem+json',
          body: JSON.stringify({
            type: '/problems/service-unavailable',
            title: 'Service unavailable',
            status: 503,
            code: 'service_unavailable',
          }),
        })
        return
      }
      await route.continue()
    })
    await page.getByRole('button', { name: new RegExp(name) }).click()
    await page.getByRole('menuitem', { name: /退出登录|log out/i }).click()
    await expect(page.getByText('当前仍保持登录', { exact: true })).toBeVisible()
    await expect(page.getByText('所需服务暂时不可用，请稍后重试。', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: new RegExp(name) })).toBeVisible()

    await page.keyboard.press('Escape')
    await selectPreference(page, '语言设置', 'English')
    await expect(page.getByRole('heading', { name: 'Home' })).toBeVisible()
    await expect(page.getByRole('menu')).toBeHidden()
    await expectNoA11yViolations(page)
    await page.getByRole('button', { name: new RegExp(name) }).click()
    await page.getByRole('menuitem', { name: 'Log out', exact: true }).click()
    await expect(page).toHaveURL(/\/login$/)
    expect(logoutAttempts).toBe(2)
    expect(browserErrors).toEqual([])
  })

  test('recovers the administrator password through the transactional mail flow', async ({ browser, page, request }) => {
    test.setTimeout(120_000)
    test.skip(!setupURL || !mailpitAPIURL, 'Set E2E_SETUP_URL and E2E_MAILPIT_API_URL against the generated stack.')
    const browserErrors = collectBrowserErrors(page)
    const appOrigin = new URL(setupURL!).origin
    const staleContext = await browser.newContext({ baseURL: appOrigin })
    const stalePage = await staleContext.newPage()
    const staleBrowserErrors = collectBrowserErrors(stalePage)

    try {
      // Keep two independent sessions alive across the reset. The reset page
      // also carries a session cookie so the API's cookie-clearing contract is
      // exercised by the browser flow.
      await signIn(page, email, password, appOrigin)
      await signIn(stalePage, email, password, appOrigin)
      const existingMessageIDs = new Set((await listMailpitMessages(request)).map(mailpitMessageID).filter((id): id is string => Boolean(id)))

      await page.goto(`${appOrigin}/forgot-password`)
      await expect(page.getByRole('heading', { name: 'Reset your password', exact: true })).toBeVisible()
      await page.getByLabel('Email').fill(email)
      await page.getByRole('button', { name: 'Send reset link', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'Check your email', exact: true })).toBeVisible()
      await expectNoA11yViolations(page)

      const resetMessage = await waitForNewMail(
        request,
        existingMessageIDs,
        (message) => mailpitContainsRecipient(message, email) && /reset|重置/i.test(String(message.Subject ?? '')),
      )
      const { link: resetLink, token } = extractResetLink(resetMessage)
      const requestURLs: string[] = []
      page.on('request', (browserRequest) => requestURLs.push(browserRequest.url()))
      await page.goto(resetLink)
      await expect(page.getByRole('heading', { name: 'Choose a new password', exact: true })).toBeVisible()
      await expectResetAuthorityNotPersisted(page, token)
      expect(requestURLs.some((value) => value.includes(token))).toBe(false)
      await expectNoA11yViolations(page)

      const cookiesBeforeReset = await page.context().cookies()
      expect(cookiesBeforeReset.some((cookie) => cookie.name.includes('temvia_session'))).toBe(true)
      const messagesBeforeCompletion = new Set((await listMailpitMessages(request)).map(mailpitMessageID).filter((id): id is string => Boolean(id)))

      await page.getByLabel('Password', { exact: true }).fill(resetPassword)
      await page.getByLabel('Confirm password').fill(resetPassword)
      await page.getByRole('button', { name: 'Set new password', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'Password updated', exact: true })).toBeVisible()
      const cookiesAfterReset = await page.context().cookies()
      expect(cookiesAfterReset.some((cookie) => cookie.name.includes('temvia_session'))).toBe(false)
      await expectNoA11yViolations(page)

      await stalePage.reload()
      await expect(stalePage).toHaveURL(/\/login$/)

      await page.goto(`${appOrigin}/login`)
      await page.getByLabel('Email').fill(email)
      await page.getByLabel('Password', { exact: true }).fill(password)
      await page.getByRole('button', { name: 'Sign in', exact: true }).click()
      await expect(page.getByRole('alert')).toHaveText('The email or password is incorrect.')
      await signIn(page, email, resetPassword, appOrigin)

      const changedMessage = await waitForNewMail(
        request,
        messagesBeforeCompletion,
        (message) => mailpitContainsRecipient(message, email) && /changed|修改/i.test(String(message.Subject ?? '')),
      )
      const changedBody = mailpitBody(changedMessage)
      expect(changedBody).toMatch(/\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} UTC/)
      expect(changedBody).not.toContain(password)
      expect(changedBody).not.toContain(resetPassword)
      expect(changedBody).not.toMatch(/v1\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}/)
      expect(changedBody).not.toContain('#token=')
      expect(browserErrors).toEqual([])
      expect(staleBrowserErrors).toEqual([])
    } finally {
      await staleContext.close()
    }
  })

  test('redirects unauthenticated visitors to login', async ({ page }) => {
    test.skip(!process.env.E2E_RUN_AUTH_SMOKE, 'Set E2E_RUN_AUTH_SMOKE=1 against a complete installation.')
    const browserErrors = collectBrowserErrors(page)
    await page.goto('/')
    await expect(page).toHaveURL(/\/login$/)
    expect(browserErrors).toEqual([])
  })
})
