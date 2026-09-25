import { expect, test, type Page } from '@playwright/test'

const enabled = process.env.E2E_SYSTEM_IDENTITY === '1'
const email = process.env.E2E_ADMIN_EMAIL ?? 'admin@example.com'
const password = process.env.E2E_ADMIN_PASSWORD ?? 'Admin1!x'
const customSystemName = '品牌 <主站> & Co'
const wideIcon = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAIAAAABCAYAAAD0In+KAAAADklEQVR4nGP4z8AAQv8BD/kD/YURmXYAAAAASUVORK5CYII=', 'base64')

type SystemIdentity = {
  systemName: string
  iconUrl: string
  hasCustomIcon: boolean
  revision: number
}

type SystemIdentitySnapshot = SystemIdentity & {
  iconBase64?: string
  iconContentType?: string
}

type RasterImage = {
  contentType: string
  width: number
  height: number
  corner: [number, number, number, number]
  center: [number, number, number, number]
  leftCenter: [number, number, number, number]
  rightCenter: [number, number, number, number]
}

async function signIn(page: Page): Promise<void> {
  await page.goto('/login')
  await expect(page.getByRole('heading', { name: 'Sign in', exact: true })).toBeVisible()
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: 'Home', exact: true })).toBeVisible()
}

async function setLocale(page: Page, locale: 'en' | 'zh-CN'): Promise<void> {
  const languageButton = page.getByRole('button', { name: /^(Language settings|语言设置)$/ })
  await languageButton.click()
  await page.getByRole('menuitemradio', { name: locale === 'en' ? 'English' : '简体中文', exact: true }).click()
}

async function readRaster(page: Page, url: string): Promise<RasterImage> {
  return page.evaluate(async (source) => {
    const response = await fetch(new URL(source, location.href), { cache: 'no-store' })
    if (!response.ok) throw new Error(`Icon request failed: ${response.status}`)
    const blob = await response.blob()
    const objectURL = URL.createObjectURL(blob)
    const image = new Image()
    image.src = objectURL
    await image.decode()
    const canvas = document.createElement('canvas')
    canvas.width = image.naturalWidth
    canvas.height = image.naturalHeight
    const context = canvas.getContext('2d')
    if (!context) throw new Error('Canvas is unavailable')
    context.drawImage(image, 0, 0)
    const width = image.naturalWidth
    const height = image.naturalHeight
    const pixels = context.getImageData(0, 0, width, height)
    const pixel = (x: number, y: number): [number, number, number, number] => {
      const offset = (y * width + x) * 4
      return [pixels.data[offset], pixels.data[offset + 1], pixels.data[offset + 2], pixels.data[offset + 3]]
    }
    URL.revokeObjectURL(objectURL)
    return {
      contentType: response.headers.get('content-type') ?? '',
      width,
      height,
      corner: pixel(0, 0),
      center: pixel(Math.floor(width / 2), Math.floor(height / 2)),
      leftCenter: pixel(Math.floor(width * 0.2), Math.floor(height / 2)),
      rightCenter: pixel(Math.floor(width * 0.8), Math.floor(height / 2)),
    }
  }, url) as Promise<RasterImage>
}

async function readIdentity(page: Page, path: string): Promise<SystemIdentity> {
  return page.evaluate(async (requestPath) => {
    const response = await fetch(requestPath, { cache: 'no-store' })
    if (!response.ok) throw new Error(`Identity request failed: ${response.status}`)
    return response.json() as Promise<SystemIdentity>
  }, path)
}

async function readIdentitySnapshot(page: Page, path: string): Promise<SystemIdentitySnapshot> {
  const identity = await readIdentity(page, path)
  if (!identity.hasCustomIcon) return identity
  const icon = await page.evaluate(async (source) => {
    const response = await fetch(new URL(source, location.href), { cache: 'no-store' })
    if (!response.ok) throw new Error(`Original icon request failed: ${response.status}`)
    const bytes = new Uint8Array(await response.arrayBuffer())
    let binary = ''
    for (let offset = 0; offset < bytes.length; offset += 0x8000) {
      binary += String.fromCharCode(...bytes.subarray(offset, Math.min(offset + 0x8000, bytes.length)))
    }
    return { iconBase64: btoa(binary), iconContentType: response.headers.get('content-type') ?? 'image/png' }
  }, identity.iconUrl)
  return { ...identity, ...icon }
}

async function restoreIdentity(page: Page, original: SystemIdentitySnapshot): Promise<void> {
  await page.evaluate(async (saved) => {
    const currentResponse = await fetch('/api/settings/system-identity', { cache: 'no-store' })
    if (!currentResponse.ok) throw new Error(`Current identity request failed: ${currentResponse.status}`)
    const current = await currentResponse.json() as SystemIdentity
    const form = new FormData()
    form.set('systemName', saved.systemName)
    form.set('revision', String(current.revision))
    if (saved.hasCustomIcon) {
      if (!saved.iconBase64) throw new Error('Original custom icon bytes were not captured')
      const binary = atob(saved.iconBase64)
      const bytes = new Uint8Array(binary.length)
      for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index)
      form.set('iconAction', 'replace')
      form.set('icon', new Blob([bytes], { type: saved.iconContentType ?? 'image/png' }), 'original-icon')
    } else {
      form.set('iconAction', 'default')
    }
    const response = await fetch('/api/settings/system-identity', { method: 'PUT', body: form })
    if (!response.ok) throw new Error(`Identity restore failed: ${response.status}`)
  }, original)
}

async function expectPublicAuthPage(page: Page, path: string, title: string, brand: string): Promise<void> {
  await page.goto(path)
  await expect(page.getByRole('heading', { name: title, exact: true })).toBeVisible()
  await expect(page.getByText(brand, { exact: true }).first()).toBeVisible()
  await expect(page.locator('img[alt=""]').first()).toBeVisible()
}

async function saveIdentity(page: Page, systemName: string): Promise<void> {
  const section = page.locator('section[aria-labelledby="identity-settings-title"]')
  await section.getByRole('textbox', { name: 'System name', exact: true }).fill(systemName)
  await section.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(section.getByRole('textbox', { name: 'System name', exact: true })).toHaveValue(systemName.trim())
}

test.describe('system identity', () => {
  test('publishes identity across public pages, sessions, mail-facing entry points, and settings', async ({ page, browser }, testInfo) => {
    test.skip(!enabled, 'Set E2E_SYSTEM_IDENTITY=1 against an isolated installation.')
    test.setTimeout(150_000)
    await signIn(page)

    const original = await readIdentitySnapshot(page, '/api/settings/system-identity')
    const origin = new URL(page.url()).origin
    const publicContext = await browser.newContext({ baseURL: origin })
    const publicPage = await publicContext.newPage()
    const conflictAContext = await browser.newContext({ baseURL: origin })
    const conflictBContext = await browser.newContext({ baseURL: origin })
    const conflictA = await conflictAContext.newPage()
    const conflictB = await conflictBContext.newPage()

    try {
      const identitySection = page.locator('section[aria-labelledby="identity-settings-title"]')
      await page.goto('/settings')
      await expect(identitySection.getByRole('heading', { name: 'System identity', exact: true })).toBeVisible()
      await saveIdentity(page, customSystemName)

      const published = await readIdentity(page, '/api/public/system-identity')
      expect(published.systemName).toBe(customSystemName)
      expect(Object.keys(published)).not.toContain('english' + 'SystemName')
      await expect(identitySection.getByRole('textbox')).toHaveCount(1)
      await page.reload()
      await expect(identitySection.getByRole('textbox', { name: 'System name', exact: true })).toHaveValue(customSystemName)
      await expect(identitySection.getByRole('textbox')).toHaveCount(1)
      await setLocale(page, 'zh-CN')
      await page.reload()
      const chineseIdentitySection = page.locator('section[aria-labelledby="identity-settings-title"]')
      await expect(chineseIdentitySection.getByRole('textbox', { name: '系统名称', exact: true })).toHaveValue(customSystemName)
      await expect(chineseIdentitySection.getByRole('textbox')).toHaveCount(1)
      await setLocale(page, 'en')
      await page.reload()

      await identitySection.getByLabel('System icon', { exact: true }).setInputFiles({ name: 'wide.png', mimeType: 'image/png', buffer: wideIcon })
      await expect(identitySection.getByText('New icon will be published on save.', { exact: true })).toBeVisible()
      await identitySection.getByRole('button', { name: 'Save', exact: true }).click()
      await expect.poll(async () => (await readIdentity(page, '/api/public/system-identity')).hasCustomIcon).toBe(true)

      const iconPublished = await readIdentity(page, '/api/public/system-identity')
      expect(iconPublished.systemName).toBe(customSystemName)
      expect(Object.keys(iconPublished)).not.toContain('english' + 'SystemName')
      const faviconURL = await page.evaluate(() => document.head.querySelector<HTMLLinkElement>('link[data-temvia-system-icon]')?.href ?? '')
      expect(faviconURL).toContain(`/api/public/system-identity/icon?v=${iconPublished.revision}`)
      const favicon = await readRaster(page, faviconURL)
      expect(favicon.contentType).toBe('image/png')
      expect(favicon.width).toBe(512)
      expect(favicon.height).toBe(512)
      expect(favicon.corner[3]).toBe(0)
      expect(favicon.center[3]).toBeGreaterThan(200)
      expect(favicon.leftCenter[0]).toBeGreaterThan(200)
      expect(favicon.leftCenter[2]).toBeLessThan(80)
      expect(favicon.rightCenter[2]).toBeGreaterThan(200)
      expect(favicon.rightCenter[0]).toBeLessThan(80)
      const customIconSnapshot = await readIdentitySnapshot(page, '/api/public/system-identity')
      const customIconRaster = await readRaster(page, customIconSnapshot.iconUrl)

      await publicPage.goto('/login')
      await setLocale(publicPage, 'en')
      await expectPublicAuthPage(publicPage, '/login', 'Sign in', customSystemName)
      await expectPublicAuthPage(publicPage, '/forgot-password', 'Reset your password', customSystemName)
      await expectPublicAuthPage(publicPage, '/reset-password', 'This reset link is invalid', customSystemName)
      await expectPublicAuthPage(publicPage, '/accept-invitation', 'This invitation is invalid', customSystemName)
      await publicPage.setViewportSize({ width: 1440, height: 900 })
      await publicPage.goto('/login')
      await publicPage.screenshot({ path: testInfo.outputPath('system-identity-en-desktop.png'), fullPage: true })

      await setLocale(publicPage, 'zh-CN')
      await expectPublicAuthPage(publicPage, '/login', '登录', customSystemName)
      await expectPublicAuthPage(publicPage, '/forgot-password', '重置密码', customSystemName)
      await expectPublicAuthPage(publicPage, '/reset-password', '重置链接无效', customSystemName)
      await expectPublicAuthPage(publicPage, '/accept-invitation', '邀请链接无效', customSystemName)
      await publicPage.setViewportSize({ width: 1440, height: 900 })
      await publicPage.goto('/login')
      await publicPage.screenshot({ path: testInfo.outputPath('system-identity-zh-desktop.png'), fullPage: true })
      await publicPage.setViewportSize({ width: 390, height: 844 })
      await publicPage.screenshot({ path: testInfo.outputPath('system-identity-zh-mobile.png'), fullPage: true })
      await setLocale(publicPage, 'en')
      await publicPage.goto('/login')
      await publicPage.screenshot({ path: testInfo.outputPath('system-identity-en-mobile.png'), fullPage: true })

      await page.goto('/settings')
      await saveIdentity(page, customSystemName)
      await publicPage.reload()
      await expect(publicPage).toHaveTitle(customSystemName)
      await expect(publicPage.getByText(customSystemName, { exact: true }).first()).toBeVisible()

      await signIn(conflictA)
      await signIn(conflictB)
      await conflictA.goto('/settings')
      await conflictB.goto('/settings')
      await expect(conflictA.locator('section[aria-labelledby="identity-settings-title"]').getByRole('textbox', { name: 'System name', exact: true })).toHaveValue(customSystemName)
      await expect(conflictB.locator('section[aria-labelledby="identity-settings-title"]').getByRole('textbox', { name: 'System name', exact: true })).toHaveValue(customSystemName)
      await saveIdentity(conflictA, 'Conflict A')
      await saveIdentity(conflictB, 'Conflict B')
      await expect(conflictB.getByRole('alert')).toContainText('This record changed')
      await conflictB.getByRole('button', { name: 'Reload', exact: true }).click()
      await expect(conflictB.locator('section[aria-labelledby="identity-settings-title"]').getByRole('textbox', { name: 'System name', exact: true })).toHaveValue('Conflict A')
      await conflictB.reload()
      await expect(conflictB.locator('section[aria-labelledby="identity-settings-title"]').getByRole('textbox', { name: 'System name', exact: true })).toHaveValue('Conflict A')

      await conflictA.goto('/operation-logs')
      await expect(conflictA.getByRole('heading', { name: 'Operation history', exact: true })).toBeVisible()
      await conflictA.locator('#operation-log-action').fill('settings.system_identity.update')
      await expect.poll(() => conflictA.locator('tbody tr').count()).toBeGreaterThan(0)

      await conflictA.goto('/settings')
      const defaultSection = conflictA.locator('section[aria-labelledby="identity-settings-title"]')
      await defaultSection.getByRole('button', { name: 'Restore default', exact: true }).click()
      await expect(defaultSection.locator('svg[data-system-icon="default"]')).toBeVisible()
      await defaultSection.getByRole('button', { name: 'Save', exact: true }).click()
      await expect.poll(async () => (await readIdentity(conflictA, '/api/public/system-identity')).hasCustomIcon).toBe(false)
      await expect.poll(async () => (await readIdentity(conflictA, '/api/public/system-identity')).systemName).toBe('Conflict A')

      await restoreIdentity(conflictA, customIconSnapshot)
      const restoredCustomIcon = await readIdentity(conflictA, '/api/public/system-identity')
      expect(restoredCustomIcon.systemName).toBe(customSystemName)
      expect(Object.keys(restoredCustomIcon)).not.toContain('english' + 'SystemName')
      expect(restoredCustomIcon.hasCustomIcon).toBe(true)
      const restoredCustomIconRaster = await readRaster(conflictA, restoredCustomIcon.iconUrl)
      expect(restoredCustomIconRaster).toEqual(customIconRaster)
    } finally {
      await restoreIdentity(page, original)
      await publicContext.close()
      await conflictAContext.close()
      await conflictBContext.close()
    }
  })
})
