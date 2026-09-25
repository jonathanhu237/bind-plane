import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { deflateSync } from 'node:zlib'

const enabled = process.env.E2E_PERSONAL_SETTINGS === '1'
const email = process.env.E2E_PERSONAL_EMAIL
const password = process.env.E2E_PERSONAL_PASSWORD
const newEmail = process.env.E2E_PERSONAL_NEW_EMAIL
const secondEmail = process.env.E2E_PERSONAL_SECOND_EMAIL
const secondPassword = process.env.E2E_PERSONAL_SECOND_PASSWORD
const mailpitAPIURL = process.env.E2E_MAILPIT_API_URL

test.describe.configure({ mode: 'serial' })
test.use({ trace: 'off', screenshot: 'off', video: 'off' })
test.skip(!enabled, 'set E2E_PERSONAL_SETTINGS=1 with a disposable account to run personal-settings acceptance tests')

type MailpitMessage = Record<string, unknown>

test('opens personal settings from the account menu across sidebar layouts', async ({ page }) => {
  requirePersonalCredentials()
  await signIn(page, email!, password!)

  const desktopSidebar = page.locator('[data-sidebar="sidebar"]').first()
  await expect(desktopSidebar.getByText('Personal settings', { exact: true })).toHaveCount(0)

  const accountTrigger = page.getByRole('button', { name: /, Menu$/ })
  await accountTrigger.click()
  const personalItem = page.getByRole('menuitem', { name: 'Personal settings', exact: true })
  const logoutItem = page.getByRole('menuitem', { name: 'Log out', exact: true })
  await expect(personalItem).toBeVisible()
  await expect(logoutItem).toBeVisible()
  const personalBox = await personalItem.boundingBox()
  const logoutBox = await logoutItem.boundingBox()
  expect(personalBox).not.toBeNull()
  expect(logoutBox).not.toBeNull()
  expect(personalBox!.y).toBeLessThan(logoutBox!.y)

  await personalItem.click()
  await expect(page).toHaveURL(/\/personal-settings$/)
  await expect(personalItem).toBeHidden()
  await accountTrigger.click()
  await expect(page.getByRole('menuitem', { name: 'Personal settings', exact: true })).toHaveAttribute('aria-current', 'page')
  await expect(page.getByRole('menuitem', { name: 'Personal settings', exact: true })).toHaveAttribute('data-active', 'true')
  await page.keyboard.press('Escape')

  // The account menu remains reachable when the desktop sidebar is collapsed.
  await page.getByRole('button', { name: 'Menu', exact: true }).click()
  await accountTrigger.click()
  await expect(page.getByRole('menuitem', { name: 'Personal settings', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')

  // The same menu selection closes the Sheet-backed mobile sidebar after routing.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  const mobileSidebar = page.locator('[data-sidebar="sidebar"][data-mobile="true"]')
  await page.getByRole('button', { name: 'Menu', exact: true }).click()
  await expect(mobileSidebar).toBeVisible()
  await mobileSidebar.getByRole('button', { name: /, Menu$/ }).click()
  await page.getByRole('menuitem', { name: 'Personal settings', exact: true }).click()
  await expect(page).toHaveURL(/\/personal-settings$/)
  await expect(mobileSidebar).toBeHidden()
})

test('keeps profile, preferences, avatar crop, and security controls account-scoped', async ({ page }) => {
  requirePersonalCredentials()
  await signIn(page, email!, password!)
  await page.goto('/personal-settings')
  await expect(page.locator('h1')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: 'Personal settings', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Profile', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Appearance and language', exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Security', exact: true })).toBeVisible()

  const nameInput = page.getByLabel('Display name', { exact: true })
  const originalName = await nameInput.inputValue()
  await nameInput.fill(`${originalName} browser`)
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith('/api/auth/me/profile') && response.request().method() === 'PUT' && response.ok()),
    page.getByRole('button', { name: 'Save', exact: true }).click(),
  ])
  await expect(nameInput).toBeEnabled()
  await expect(nameInput).toHaveValue(`${originalName} browser`)
  await nameInput.fill(originalName)
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith('/api/auth/me/profile') && response.request().method() === 'PUT' && response.ok()),
    page.getByRole('button', { name: 'Save', exact: true }).click(),
  ])
  await expect(nameInput).toBeEnabled()
  await expect(nameInput).toHaveValue(originalName)

  await page.getByRole('radio', { name: 'Dark', exact: true }).check()
  await expect(page.locator('html')).toHaveClass(/dark/)
  await page.getByRole('radio', { name: 'Light', exact: true }).check()
  await expect(page.locator('html')).not.toHaveClass(/dark/)

  await page.getByRole('combobox', { name: 'Account language', exact: true }).click()
  await page.getByRole('option', { name: '简体中文', exact: true }).click()
  await expect(page.getByRole('heading', { name: '个人设置', exact: true })).toBeVisible()
  await expect(page.getByRole('combobox', { name: '账户语言', exact: true })).toBeEnabled()
  await page.getByRole('combobox', { name: '账户语言', exact: true }).click()
  await page.getByRole('option', { name: 'English', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Personal settings', exact: true })).toBeVisible()
  await expect(page.getByRole('combobox', { name: 'Account language', exact: true })).toBeEnabled()

  const wideImage = splitPNG(400, 200, 'x')
  await uploadAvatar(page, wideImage, 'wide.png')
  await dragCrop(page, 'right')
  await expectPreviewEdge(page, 'left')
  await saveAvatar(page)
  const leftPixel = await avatarCenterPixel(page)
  expect(leftPixel.r).toBeGreaterThan(150)
  expect(leftPixel.g).toBeLessThan(80)
  expect(leftPixel.b).toBeLessThan(80)

  await uploadAvatar(page, wideImage, 'wide-again.png')
  await dragCrop(page, 'left')
  await expectPreviewEdge(page, 'right')
  await saveAvatar(page)
  const rightPixel = await avatarCenterPixel(page)
  expect(rightPixel.b).toBeGreaterThan(150)
  expect(rightPixel.r).toBeLessThan(80)
  expect(rightPixel.g).toBeLessThan(80)

  const tallImage = splitPNG(200, 400, 'y')
  await uploadAvatar(page, tallImage, 'tall.png')
  await dragCrop(page, 'down')
  await expectPreviewEdge(page, 'top')
  await saveAvatar(page)
  const topPixel = await avatarCenterPixel(page)
  expect(topPixel.g).toBeGreaterThan(100)
  expect(topPixel.r).toBeLessThan(80)
  expect(topPixel.b).toBeLessThan(80)

  await uploadAvatar(page, tallImage, 'tall-again.png')
  await dragCrop(page, 'up')
  await expectPreviewEdge(page, 'bottom')
  await saveAvatar(page)
  const bottomPixel = await avatarCenterPixel(page)
  expect(bottomPixel.b).toBeGreaterThan(150)
  expect(bottomPixel.r).toBeLessThan(80)
  expect(bottomPixel.g).toBeLessThan(80)

  await page.getByRole('button', { name: 'Remove avatar', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Choose avatar', exact: true })).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('Display name', { exact: true })).toHaveValue(originalName)
  await expect(page.getByRole('radio', { name: 'Light', exact: true })).toBeChecked()
})

test('keeps account preferences isolated between two browser sessions', async ({ browser }) => {
  test.skip(!secondEmail || !secondPassword, 'set E2E_PERSONAL_SECOND_EMAIL and E2E_PERSONAL_SECOND_PASSWORD for isolation coverage')
  requirePersonalCredentials()
  const first = await browser.newPage()
  const second = await browser.newPage()
  try {
    await signIn(first, email!, password!)
    await signIn(second, secondEmail!, secondPassword!)
    await first.goto('/personal-settings')
    await first.getByRole('radio', { name: 'Dark', exact: true }).check()
    await expect(first.locator('html')).toHaveClass(/dark/)
    await second.goto('/personal-settings')
    await second.getByRole('radio', { name: 'Light', exact: true }).check()
    await expect(second.locator('html')).not.toHaveClass(/dark/)
    await expect(first.locator('html')).toHaveClass(/dark/)
    await first.getByRole('combobox', { name: 'Account language', exact: true }).click()
    await first.getByRole('option', { name: '简体中文', exact: true }).click()
    await expect(first.getByRole('heading', { name: '个人设置', exact: true })).toBeVisible()
    await expect(first.getByRole('combobox', { name: '账户语言', exact: true })).toBeEnabled()

    await second.reload()
    await expect(second.getByRole('heading', { name: 'Personal settings', exact: true })).toBeVisible()
    await expect(second.getByRole('heading', { name: '个人设置', exact: true })).toHaveCount(0)

    await first.getByRole('combobox', { name: '账户语言', exact: true }).click()
    await first.getByRole('option', { name: 'English', exact: true }).click()
    await expect(first.getByRole('combobox', { name: 'Account language', exact: true })).toBeEnabled()
    await first.getByRole('radio', { name: 'Light', exact: true }).check()
    await expect(first.locator('html')).not.toHaveClass(/dark/)
    await expect(first.getByRole('heading', { name: 'Personal settings', exact: true })).toBeVisible()
  } finally {
    await first.close()
    await second.close()
  }
})

test('completes email verification through Mailpit without coupling it to sessions', async ({ page, request }) => {
  test.skip(!email || !password || !newEmail || !mailpitAPIURL, 'set disposable personal email credentials, E2E_PERSONAL_NEW_EMAIL, and E2E_MAILPIT_API_URL')
  await signIn(page, email!, password!)
  await page.goto('/personal-settings')
  const previousIDs = await listMailpitMessageIDs(request)
  await page.getByLabel('Current password', { exact: true }).last().fill(password!)
  await page.getByLabel('New email address', { exact: true }).fill(newEmail!)
  await page.getByRole('button', { name: 'Send verification code', exact: true }).click()
  await expect(page.getByLabel('Verification code', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Resend code', exact: true })).toBeDisabled()
  await expect(page.getByText(/resend in/i)).toBeVisible()

  const message = await waitForNewMail(request, previousIDs, (candidate) => mailpitContainsRecipient(candidate, newEmail!) && /verification code|邮箱验证码/i.test(mailpitBody(candidate)))
  const code = extractEmailChangeCode(message)

  await signOut(page)
  await signIn(page, email!, password!)
  await page.goto('/personal-settings')
  await expect(page.getByLabel('Verification code', { exact: true })).toBeVisible()
  await page.getByLabel('Verification code', { exact: true }).fill(code)
  await page.getByRole('button', { name: 'Verify and change email', exact: true }).click()
  await expect(page).toHaveURL(/\/login$/)

  await signIn(page, newEmail!, password!)
  await page.goto('/personal-settings')
  await expect(page.getByLabel('Email address', { exact: true })).toHaveValue(newEmail!)
  await waitForNewMail(request, previousIDs, (candidate) => mailpitContainsRecipient(candidate, email!) && /email changed|邮箱已修改|邮箱已变更|邮箱变更/i.test(mailpitBody(candidate)))
})

function requirePersonalCredentials(): void {
  if (!email || !password) throw new Error('personal browser credentials are not configured')
}

async function signIn(page: Page, userEmail: string, userPassword: string): Promise<void> {
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
  await page.getByLabel('Email', { exact: true }).fill(userEmail)
  await page.getByLabel('Password', { exact: true }).fill(userPassword)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/$/)
}

async function signOut(page: Page): Promise<void> {
  await page.getByRole('button', { name: /, Menu$/ }).click()
  await page.getByRole('menuitem', { name: 'Log out', exact: true }).click()
  await expect(page).toHaveURL(/\/login$/)
}

async function uploadAvatar(page: Page, content: Buffer, filename: string): Promise<void> {
  const preview = page.getByTestId('avatar-crop-preview')
  await page.locator('input[type="file"]').setInputFiles({ name: filename, mimeType: 'image/png', buffer: content })
  await expect(preview.locator('img')).toBeVisible()
  await expect(preview).toHaveAttribute('data-source-ready', 'true')
  await expect(page.getByRole('button', { name: 'Save avatar', exact: true })).toBeEnabled()
}

async function dragCrop(page: Page, direction: 'left' | 'right' | 'up' | 'down'): Promise<void> {
  const preview = page.getByTestId('avatar-crop-preview')
  await preview.scrollIntoViewIfNeeded()
  const box = await preview.boundingBox()
  if (!box) throw new Error('avatar crop preview is not laid out')
  const centerX = box.x + box.width / 2
  const centerY = box.y + box.height / 2
  const delta = 47
  const endX = centerX + (direction === 'right' ? delta : direction === 'left' ? -delta : 0)
  const endY = centerY + (direction === 'down' ? delta : direction === 'up' ? -delta : 0)
  await page.mouse.move(centerX, centerY)
  await page.mouse.down()
  await page.mouse.move(endX, endY)
  await page.mouse.up()
}

async function expectPreviewEdge(page: Page, edge: 'left' | 'right' | 'top' | 'bottom'): Promise<void> {
  const preview = page.getByTestId('avatar-crop-preview')
  const box = await preview.boundingBox()
  if (!box) throw new Error('avatar crop preview is not laid out')
  const image = preview.locator('img')
  const geometry = await image.evaluate((element) => {
    const imageRect = element.getBoundingClientRect()
    const parentRect = element.parentElement!.getBoundingClientRect()
    return {
      left: imageRect.left - parentRect.left,
      right: parentRect.right - imageRect.right,
      top: imageRect.top - parentRect.top,
      bottom: parentRect.bottom - imageRect.bottom,
    }
  })
  const distance = edge === 'left' ? geometry.left : edge === 'right' ? geometry.right : edge === 'top' ? geometry.top : geometry.bottom
  expect(Math.abs(distance)).toBeLessThan(4)
}

async function saveAvatar(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Save avatar', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Save avatar', exact: true })).toHaveCount(0)
}

async function avatarCenterPixel(page: Page): Promise<{ r: number; g: number; b: number }> {
  return page.evaluate(async () => {
    const profileResponse = await fetch('/api/auth/me/profile')
    const profile = (await profileResponse.json()) as { user: { avatarUrl?: string } }
    if (!profile.user.avatarUrl) throw new Error('saved avatar URL is unavailable')
    const imageResponse = await fetch(profile.user.avatarUrl)
    const blob = await imageResponse.blob()
    const objectURL = URL.createObjectURL(blob)
    try {
      const image = await new Promise<HTMLImageElement>((resolve, reject) => {
        const element = new Image()
        element.onload = () => resolve(element)
        element.onerror = () => reject(new Error('saved avatar could not be decoded'))
        element.src = objectURL
      })
      const canvas = document.createElement('canvas')
      canvas.width = 1
      canvas.height = 1
      const context = canvas.getContext('2d')!
      context.drawImage(image, 0, 0, 1, 1)
      const [r, g, b] = context.getImageData(0, 0, 1, 1).data
      return { r, g, b }
    } finally {
      URL.revokeObjectURL(objectURL)
    }
  })
}

function splitPNG(width: number, height: number, axis: 'x' | 'y'): Buffer {
  const rowSize = width * 4 + 1
  const raw = Buffer.alloc(rowSize * height)
  for (let y = 0; y < height; y++) {
    raw[y * rowSize] = 0
    for (let x = 0; x < width; x++) {
      const first = axis === 'x' ? x < width / 2 : y < height / 2
      const offset = y * rowSize + 1 + x * 4
      if (axis === 'x') {
        raw[offset] = first ? 220 : 0
        raw[offset + 1] = 0
        raw[offset + 2] = first ? 0 : 220
      } else {
        raw[offset] = 0
        raw[offset + 1] = first ? 220 : 0
        raw[offset + 2] = first ? 0 : 220
      }
      raw[offset + 3] = 255
    }
  }
  return Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    pngChunk('IHDR', Buffer.from([width >> 24, width >> 16, width >> 8, width, height >> 24, height >> 16, height >> 8, height, 8, 6, 0, 0, 0])),
    pngChunk('IDAT', deflateSync(raw)),
    pngChunk('IEND', Buffer.alloc(0)),
  ])
}

function pngChunk(type: string, content: Buffer): Buffer {
  const typeBytes = Buffer.from(type)
  const payload = Buffer.concat([typeBytes, content])
  const crc = crc32(payload)
  const checksum = Buffer.from([(crc >>> 24) & 255, (crc >>> 16) & 255, (crc >>> 8) & 255, crc & 255])
  return Buffer.concat([Buffer.from([content.length >>> 24, content.length >>> 16, content.length >>> 8, content.length]), payload, checksum])
}

function crc32(value: Buffer): number {
  let crc = 0xffffffff
  for (const byte of value) {
    crc ^= byte
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0)
  }
  return (crc ^ 0xffffffff) >>> 0
}

function mailpitURL(path: string): string {
  if (!mailpitAPIURL) throw new Error('mail transport is not configured')
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

async function listMailpitMessageIDs(request: APIRequestContext): Promise<Set<string>> {
  const response = await request.get(mailpitURL('messages?limit=200'), { timeout: 5_000 })
  if (!response.ok()) throw new Error('mail transport message listing failed')
  const payload = (await response.json()) as { messages?: unknown[] } | unknown[]
  const messages = Array.isArray(payload) ? payload : payload.messages ?? []
  return new Set(messages.map((message) => mailpitMessageID(message as MailpitMessage)).filter((id): id is string => Boolean(id)))
}

async function getMailpitMessage(request: APIRequestContext, id: string): Promise<MailpitMessage | undefined> {
  const response = await request.get(mailpitURL(`message/${encodeURIComponent(id)}`), { timeout: 5_000 })
  if (!response.ok()) return undefined
  const payload = (await response.json()) as unknown
  return typeof payload === 'object' && payload !== null && !Array.isArray(payload) ? payload as MailpitMessage : undefined
}

async function waitForNewMail(request: APIRequestContext, previousIDs: Set<string>, predicate: (message: MailpitMessage) => boolean): Promise<MailpitMessage> {
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    const summaries = await listMailpitMessages(request)
    for (const summary of summaries) {
      const id = mailpitMessageID(summary)
      if (!id || previousIDs.has(id)) continue
      const message = await getMailpitMessage(request, id)
      if (message && predicate(message)) return message
    }
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
  throw new Error('timed out waiting for the expected mail message')
}

async function listMailpitMessages(request: APIRequestContext): Promise<MailpitMessage[]> {
  const response = await request.get(mailpitURL('messages?limit=200'), { timeout: 5_000 })
  if (!response.ok()) throw new Error('mail transport message listing failed')
  const payload = (await response.json()) as { messages?: unknown[] } | unknown[]
  const messages = Array.isArray(payload) ? payload : payload.messages ?? []
  return messages.filter((message): message is MailpitMessage => typeof message === 'object' && message !== null && !Array.isArray(message))
}

function mailpitBody(message: MailpitMessage): string {
  const text = typeof message.Text === 'string' ? message.Text : ''
  const html = typeof message.HTML === 'string' ? message.HTML : ''
  return `${text}\n${html}`
}

function mailpitContainsRecipient(message: MailpitMessage, recipient: string): boolean {
  return JSON.stringify(message.To ?? '').toLowerCase().includes(recipient.toLowerCase())
}

function extractEmailChangeCode(message: MailpitMessage): string {
  const match = mailpitBody(message).match(/(?:verification code is:\s*|验证码为：\s*)([0-9]{6})/)
  if (!match) throw new Error('mail message did not contain a six-digit verification code')
  return match[1]
}
