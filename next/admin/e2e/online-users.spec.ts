import { expect, test, type Page } from '@playwright/test'

const enabled = process.env.E2E_ONLINE_USERS === '1'
const managerEmail = process.env.E2E_ONLINE_MANAGER_EMAIL ?? 'online-manager@example.com'
const managerPassword = process.env.E2E_ONLINE_MANAGER_PASSWORD ?? 'Manager1!x'
const targetEmail = process.env.E2E_ONLINE_TARGET_EMAIL ?? 'online-super-admin@example.com'
const targetPassword = process.env.E2E_ONLINE_TARGET_PASSWORD ?? 'Target1!x'
const readOnlyEmail = process.env.E2E_ONLINE_READONLY_EMAIL
const readOnlyPassword = process.env.E2E_ONLINE_READONLY_PASSWORD
const noReadEmail = process.env.E2E_ONLINE_NOREAD_EMAIL
const noReadPassword = process.env.E2E_ONLINE_NOREAD_PASSWORD
const revokeTimeout = Number(process.env.E2E_ONLINE_REVOKE_TIMEOUT_MS ?? 60_000)
const idleTimeout = Number(process.env.E2E_ONLINE_IDLE_TIMEOUT_MS ?? 0)

async function signIn(page: Page, email: string, password: string): Promise<void> {
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: 'Home', exact: true })).toBeVisible()
}

async function openOnlineUsers(page: Page): Promise<void> {
  await page.goto('/online-users')
  await expect(page.getByRole('heading', { name: 'Online users', exact: true })).toHaveCount(1)
  await expect(page.getByText('Users with at least one valid sign-in session', { exact: false })).toHaveCount(0)
  await expect(page.getByRole('status').filter({ hasText: 'Loading' })).toHaveCount(0)
}

function onlineUserRow(page: Page, email: string) {
  return page.getByRole('row').filter({ hasText: email })
}

async function setLanguage(page: Page, locale: 'en' | 'zh-CN'): Promise<void> {
  const languageButton = page.getByRole('button', { name: /Language settings|语言设置/, exact: true })
  await languageButton.click()
  await page.getByRole('menuitemradio', { name: locale === 'en' ? 'English' : '简体中文', exact: true }).click()
}

async function expectExpired(page: Page, locale: 'en' | 'zh-CN' = 'en'): Promise<void> {
  const notice = locale === 'en' ? 'Your session has expired. Sign in again.' : '登录已失效，请重新登录。'
  await Promise.all([
    expect(page).toHaveURL(/\/login$/, { timeout: revokeTimeout }),
    expect(page.getByText(notice, { exact: true })).toBeVisible({ timeout: revokeTimeout }),
  ])
  await expect(page.getByRole('heading', { name: locale === 'en' ? 'Sign in' : '登录', exact: true })).toBeVisible()
}

async function closeContexts(contexts: Array<{ close: () => Promise<void> }>): Promise<void> {
  await Promise.all(contexts.map((context) => context.close()))
}

test.describe('online user acceptance', () => {
  test.skip(!enabled, 'Set E2E_ONLINE_USERS=1 with an isolated acceptance stack to run real online-user flows.')
  test.describe.configure({ mode: 'serial' })

  test('aggregates two target sessions, signs out a Super Admin, and permits relogin', async ({ browser, page }) => {
    test.setTimeout(150_000)
    const targetContextA = await browser.newContext()
    const targetContextB = await browser.newContext()
    const targetPageA = await targetContextA.newPage()
    const targetPageB = await targetContextB.newPage()
    try {
      await signIn(targetPageA, targetEmail, targetPassword)
      await signIn(targetPageB, targetEmail, targetPassword)
      await signIn(page, managerEmail, managerPassword)
      await openOnlineUsers(page)

      const targetRow = onlineUserRow(page, targetEmail)
      await expect(targetRow).toHaveCount(1)
      await expect(targetRow).toContainText(targetEmail)
      await expect(targetRow.locator('td').nth(2)).toHaveText('2')

      const search = page.getByRole('textbox', { name: /Search users by name or email/ })
      await search.fill(targetEmail)
      await expect(targetRow).toBeVisible()
      await expect(page.getByRole('row').filter({ hasText: managerEmail })).toHaveCount(0)
      await search.fill('')

      await targetRow.getByRole('button', { name: 'Force sign out', exact: true }).click()
      const dialog = page.getByRole('alertdialog')
      await expect(dialog).toContainText('all devices')
      await expect(dialog).toContainText(targetEmail)
      await dialog.getByRole('button', { name: 'Cancel', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      await expect(targetPageA).toHaveURL(/\/$/)
      await expect(targetPageB).toHaveURL(/\/$/)

      await onlineUserRow(page, targetEmail).getByRole('button', { name: 'Force sign out', exact: true }).click()
      await page.getByRole('alertdialog').getByRole('button', { name: 'Force sign out', exact: true }).click()
      await Promise.all([expectExpired(targetPageA), expectExpired(targetPageB)])
      await expect(page).toHaveURL(/\/online-users$/)

      // Session revocation is temporary: a fresh login creates a valid session.
      await signIn(targetPageA, targetEmail, targetPassword)
      await openOnlineUsers(page)
      await expect(onlineUserRow(page, targetEmail)).toHaveCount(1)
    } finally {
      await closeContexts([targetContextA, targetContextB])
    }
  })

  test('keeps read-only and no-read permission boundaries visible in the browser', async ({ browser }) => {
    test.skip(!readOnlyEmail || !readOnlyPassword || !noReadEmail || !noReadPassword, 'Set read-only and no-read credentials for the permission acceptance flow.')
    test.setTimeout(90_000)
    const readOnlyContext = await browser.newContext()
    const noReadContext = await browser.newContext()
    const readOnlyPage = await readOnlyContext.newPage()
    const noReadPage = await noReadContext.newPage()
    try {
      await signIn(readOnlyPage, readOnlyEmail!, readOnlyPassword!)
      await openOnlineUsers(readOnlyPage)
      await expect(readOnlyPage.getByRole('button', { name: 'Force sign out', exact: true })).toHaveCount(0)
      await expect(onlineUserRow(readOnlyPage, readOnlyEmail!)).toHaveCount(1)

      const listRequests: string[] = []
      noReadPage.on('request', (request) => {
        if (new URL(request.url()).pathname === '/api/online-users') listRequests.push(request.url())
      })
      await signIn(noReadPage, noReadEmail!, noReadPassword!)
      await noReadPage.goto('/online-users')
      await expect(noReadPage.getByRole('heading', { name: 'Access denied', exact: true })).toBeVisible()
      await expect(noReadPage.locator('section[role="status"]').getByText('Your account does not have permission to view this page.', { exact: true })).toBeVisible()
      expect(listRequests).toEqual([])
    } finally {
      await closeContexts([readOnlyContext, noReadContext])
    }
  })

  test('localizes the online page and self-sign-out confirmation', async ({ browser }) => {
    test.setTimeout(150_000)
    const firstContext = await browser.newContext()
    const secondContext = await browser.newContext()
    const firstPage = await firstContext.newPage()
    const secondPage = await secondContext.newPage()
    try {
      await signIn(firstPage, managerEmail, managerPassword)
      await signIn(secondPage, managerEmail, managerPassword)
      const page = secondPage
      await openOnlineUsers(page)
      await setLanguage(page, 'zh-CN')
      await expect(page.getByRole('heading', { name: '在线用户', exact: true })).toHaveCount(1)
      await expect(page.getByRole('textbox', { name: '按姓名或邮箱搜索用户…', exact: true })).toBeVisible()

      const selfRow = onlineUserRow(page, managerEmail)
      await expect(selfRow).toHaveCount(1)
      await selfRow.getByRole('button', { name: '强制退出', exact: true }).click()
      const dialog = page.getByRole('alertdialog')
      await expect(dialog).toContainText('包括当前页面')
      await dialog.getByRole('button', { name: '取消', exact: true }).click()
      await expect(dialog).toHaveCount(0)
      await expect(page).toHaveURL(/\/online-users$/)

      await selfRow.getByRole('button', { name: '强制退出', exact: true }).click()
      await page.getByRole('alertdialog').getByRole('button', { name: '强制退出', exact: true }).click()
      await Promise.all([expectExpired(page, 'zh-CN'), expectExpired(firstPage)])
    } finally {
      await closeContexts([firstContext, secondContext])
    }
  })

  test('lets background checks and list refreshes observe idle expiry without extending it', async ({ browser }) => {
    test.skip(idleTimeout <= 0, 'Set E2E_ONLINE_IDLE_TIMEOUT_MS to the isolated API idle timeout to run expiry acceptance.')
    test.setTimeout(Math.max(90_000, idleTimeout + 60_000))
    const targetContext = await browser.newContext()
    const managerContext = await browser.newContext()
    const targetPage = await targetContext.newPage()
    const managerPage = await managerContext.newPage()
    let successfulTargetChecks = 0
    let successfulListReads = 0
    targetPage.on('response', (response) => {
      if (new URL(response.url()).pathname === '/api/auth/session-status' && response.status() === 200) successfulTargetChecks++
    })
    managerPage.on('response', (response) => {
      if (new URL(response.url()).pathname === '/api/online-users' && response.status() === 200) successfulListReads++
    })
    try {
      await signIn(targetPage, targetEmail, targetPassword)
      await signIn(managerPage, managerEmail, managerPassword)
      await openOnlineUsers(managerPage)
      await expect(onlineUserRow(managerPage, targetEmail)).toHaveCount(1)

      const expiryTimeout = Math.max(revokeTimeout, idleTimeout + 45_000)
      await expect(targetPage).toHaveURL(/\/login$/, { timeout: expiryTimeout })
      await expect(managerPage).toHaveURL(/\/login$/, { timeout: expiryTimeout })
      if (idleTimeout > 30_000) {
        expect(successfulTargetChecks).toBeGreaterThanOrEqual(2)
        expect(successfulListReads).toBeGreaterThanOrEqual(2)
      }
    } finally {
      await closeContexts([targetContext, managerContext])
    }
  })
})
