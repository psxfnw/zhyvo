import { existsSync } from 'node:fs'
import { mkdir } from 'node:fs/promises'
import { resolve } from 'node:path'
import { chromium } from 'playwright-core'

const baseURL = process.env.PHOTODROP_URL ?? 'http://localhost:3000'
const chromeCandidates = [
  process.env.CHROME_PATH,
  'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
  'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
  '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
  '/usr/bin/google-chrome',
  '/usr/bin/chromium',
].filter(Boolean)
const executablePath = chromeCandidates.find(existsSync)

if (!executablePath) {
  throw new Error('Chrome/Chromium not found. Set CHROME_PATH to run the browser smoke test.')
}

async function json(path, options = {}) {
  const response = await fetch(`${baseURL}/api/v1${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...options.headers },
  })
  if (!response.ok) throw new Error(`${options.method ?? 'GET'} ${path} failed with ${response.status}: ${await response.text()}`)
  return response.status === 204 || response.status === 202 ? null : response.json()
}

const auth = await json('/auth/anonymous', {
  method: 'POST',
  body: JSON.stringify({ display_name: 'E2E Browser', client_type: 'web' }),
})
const created = await json('/rooms', {
  method: 'POST',
  headers: { Authorization: `Bearer ${auth.access_token}`, 'Idempotency-Key': crypto.randomUUID() },
  body: JSON.stringify({ name: 'Mobile UX Check', lifetime_days: 1, access: { mode: 'public' } }),
})
const slug = created.room.slug
const guestAuth = await json('/auth/anonymous', {
  method: 'POST',
  body: JSON.stringify({ display_name: 'E2E Guest', client_type: 'web' }),
})
let cleanupAuth = auth
await json(`/rooms/${slug}/join`, {
  method: 'POST',
  headers: { Authorization: `Bearer ${guestAuth.access_token}` },
  body: JSON.stringify({}),
})
const listed = await json('/rooms', { headers: { Authorization: `Bearer ${auth.access_token}` } })
if (!listed.rooms.some((room) => room.slug === slug && room.role === 'owner')) throw new Error('Created room is missing from my rooms')
const roomMembers = await json(`/rooms/${slug}/members`, { headers: { Authorization: `Bearer ${auth.access_token}` } })
if (roomMembers.members.length !== 2) throw new Error(`Expected 2 room members, got ${roomMembers.members.length}`)
const artifacts = resolve('tmp', 'e2e')
await mkdir(artifacts, { recursive: true })

const browser = await chromium.launch({ executablePath, headless: true })
const context = await browser.newContext({ viewport: { width: 375, height: 812 }, locale: 'uk-UA' })
await context.addInitScript((storedSession) => {
  localStorage.setItem('photodrop.session.v1', JSON.stringify(storedSession))
}, auth)
const page = await context.newPage()
const pageErrors = []
page.on('pageerror', (error) => pageErrors.push(error.message))

try {
  await page.goto(`${baseURL}/r/${slug}`, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: 'Mobile UX Check' }).waitFor()

  const portraitOverflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  if (portraitOverflow > 1) throw new Error(`375px layout has ${portraitOverflow}px horizontal overflow`)
  await page.screenshot({ path: resolve(artifacts, 'room-375.png'), fullPage: true })

  await page.getByRole('button', { name: 'Запросити' }).click()
  const shareDialog = page.getByRole('dialog', { name: 'Запросити друзів' })
  await shareDialog.waitFor()
  if (await shareDialog.locator('.qr-wrap > svg').count() !== 1) throw new Error('QR code was not rendered')
  const expectedInviteURL = `https://t.me/zhyvoappbot?startapp=room_${slug}`
  const qrInviteURL = await shareDialog.locator('.qr-wrap > svg').getAttribute('data-invite-url')
  if (qrInviteURL !== expectedInviteURL) throw new Error(`Unexpected QR invite URL: ${qrInviteURL}`)
  await shareDialog.getByRole('button', { name: 'Надіслати в Telegram' }).waitFor()
  const browserInviteURL = await shareDialog.getByRole('link', { name: 'Відкрити кімнату у браузері' }).getAttribute('href')
  if (browserInviteURL !== `${baseURL}/r/${slug}`) throw new Error(`Unexpected browser invite URL: ${browserInviteURL}`)
  await page.screenshot({ path: resolve(artifacts, 'share-375.png'), fullPage: true })
  await shareDialog.getByRole('button', { name: 'Закрити' }).click()

  await page.locator('input[type="file"]').setInputFiles(resolve('public', 'pwa-64x64.png'))
  await page.locator('article.media-card').waitFor({ timeout: 20_000 })
  await page.locator('article.media-card .media-preview').first().click()
  await page.getByRole('dialog', { name: 'pwa-64x64.png' }).waitFor()
  if (!page.url().includes('media=')) throw new Error('Media viewer did not update the URL')
  await page.screenshot({ path: resolve(artifacts, 'viewer-375.png') })
  await page.getByRole('button', { name: 'Закрити перегляд' }).click()
  await page.getByRole('button', { name: 'Налаштування кімнати' }).click()
  const settingsDialog = page.getByRole('dialog', { name: 'Налаштування' })
  await settingsDialog.waitFor()
  const settingsTargets = await settingsDialog.locator('button:visible').evaluateAll((elements) => elements
    .map((element) => ({ label: element.getAttribute('aria-label') || element.textContent?.trim(), ...element.getBoundingClientRect().toJSON() }))
    .filter((box) => box.width < 44 || box.height < 44))
  if (settingsTargets.length) throw new Error(`Settings touch targets below 44px: ${JSON.stringify(settingsTargets)}`)
  await settingsDialog.getByRole('button', { name: 'Переглянути' }).click()
  const membersDialog = page.getByRole('dialog', { name: 'Керування кімнатою' })
  await membersDialog.waitFor()
  await membersDialog.getByText('E2E Guest').waitFor()
  await membersDialog.getByRole('tab', { name: 'Історія' }).click()
  await membersDialog.getByText(/приєднався/).first().waitFor()
  await membersDialog.getByRole('button', { name: 'Закрити' }).click()

  const viewportChecks = [
    { width: 768, height: 1024 },
    { width: 1024, height: 768 },
    { width: 1440, height: 900 },
    { width: 844, height: 390 },
  ]
  const overflows = {}
  for (const viewport of viewportChecks) {
    await page.setViewportSize(viewport)
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    overflows[`${viewport.width}x${viewport.height}`] = overflow
    if (overflow > 1) throw new Error(`${viewport.width}px layout has ${overflow}px horizontal overflow`)
  }
  await page.screenshot({ path: resolve(artifacts, 'room-landscape.png') })

  await page.setViewportSize({ width: 375, height: 812 })
  const undersizedTargets = await page.locator('button:visible, a:visible').evaluateAll((elements) => elements
    .map((element) => {
      const box = element.getBoundingClientRect()
      return { label: element.getAttribute('aria-label') || element.textContent?.trim(), width: box.width, height: box.height }
    })
    .filter((box) => box.width < 44 || box.height < 44))
  if (undersizedTargets.length) throw new Error(`Touch targets below 44px: ${JSON.stringify(undersizedTargets)}`)

  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(baseURL, { waitUntil: 'networkidle' })
  await page.getByRole('heading', { name: 'Мої кімнати' }).waitFor()
  await page.getByText('Mobile UX Check').waitFor()
  await page.screenshot({ path: resolve(artifacts, 'home-1440.png'), fullPage: true })
  await page.setViewportSize({ width: 375, height: 812 })
  const homeOverflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  if (homeOverflow > 1) throw new Error(`Home 375px layout has ${homeOverflow}px horizontal overflow`)
  const homeTargets = await page.locator('button:visible, a:visible, input[type="range"]:visible').evaluateAll((elements) => elements
    .map((element) => ({ label: element.getAttribute('aria-label') || element.textContent?.trim(), ...element.getBoundingClientRect().toJSON() }))
    .filter((box) => box.width < 44 || box.height < 44))
  if (homeTargets.length) throw new Error(`Home touch targets below 44px: ${JSON.stringify(homeTargets)}`)
  await page.screenshot({ path: resolve(artifacts, 'home-375.png'), fullPage: true })

  await page.emulateMedia({ colorScheme: 'dark' })
  await page.evaluate(() => { document.documentElement.dataset.telegramTheme = 'dark' })
  const telegramPalette = await page.evaluate(() => {
    const style = getComputedStyle(document.documentElement)
    return { canvas: style.getPropertyValue('--canvas').trim(), ink: style.getPropertyValue('--ink').trim(), colorScheme: style.colorScheme }
  })
  if (telegramPalette.canvas !== '#f5f5f7' || telegramPalette.ink !== '#1d1d1f' || telegramPalette.colorScheme !== 'light') {
    throw new Error(`Telegram dark preference changed Zhyvo palette: ${JSON.stringify(telegramPalette)}`)
  }
  await page.screenshot({ path: resolve(artifacts, 'home-telegram-dark-375.png'), fullPage: true })

  await json(`/rooms/${slug}/members/${guestAuth.identity.id}`, {
    method: 'DELETE', headers: { Authorization: `Bearer ${auth.access_token}` },
  })
  const blocked = await json(`/rooms/${slug}/members`, { headers: { Authorization: `Bearer ${auth.access_token}` } })
  if (!blocked.blocked_members.some((member) => member.id === guestAuth.identity.id)) throw new Error('Removed member was not blocked')
  const blockedJoin = await fetch(`${baseURL}/api/v1/rooms/${slug}/join`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${guestAuth.access_token}` },
    body: JSON.stringify({}),
  })
  if (blockedJoin.status !== 403) throw new Error(`Blocked member rejoined with status ${blockedJoin.status}`)
  await json(`/rooms/${slug}/blocked-members/${guestAuth.identity.id}`, {
    method: 'DELETE', headers: { Authorization: `Bearer ${auth.access_token}` },
  })
  await json(`/rooms/${slug}/join`, {
    method: 'POST', headers: { Authorization: `Bearer ${guestAuth.access_token}` }, body: JSON.stringify({}),
  })
  const transferred = await json(`/rooms/${slug}/ownership`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${auth.access_token}` },
    body: JSON.stringify({ identity_id: guestAuth.identity.id }),
  })
  if (transferred.room.role !== 'member') throw new Error('Previous owner did not become a member')
  const newOwnerRoom = await json(`/rooms/${slug}`, { headers: { Authorization: `Bearer ${guestAuth.access_token}` } })
  if (newOwnerRoom.room.role !== 'owner') throw new Error('Ownership was not transferred')
  const roomActivity = await json(`/rooms/${slug}/activity`, { headers: { Authorization: `Bearer ${guestAuth.access_token}` } })
  for (const eventType of ['member_removed', 'member_unblocked', 'ownership_transferred']) {
    if (!roomActivity.events.some((event) => event.type === eventType)) throw new Error(`Missing ${eventType} activity event`)
  }
  cleanupAuth = guestAuth

  if (pageErrors.length) throw new Error(`Browser errors: ${pageErrors.join('; ')}`)
  console.log(JSON.stringify({ slug, portraitOverflow, homeOverflow, overflows, touchTargets: true, qr: true, upload: true, viewer: true, myRooms: true, members: true, moderation: true, ownershipTransfer: true, activity: true, telegramLightTheme: true }))
} finally {
  await context.close()
  await browser.close()
  await json(`/rooms/${slug}`, { method: 'DELETE', headers: { Authorization: `Bearer ${cleanupAuth.access_token}` } }).catch(() => undefined)
  await json('/auth/session', { method: 'DELETE', headers: { Authorization: `Bearer ${auth.access_token}` } }).catch(() => undefined)
  await json('/auth/session', { method: 'DELETE', headers: { Authorization: `Bearer ${guestAuth.access_token}` } }).catch(() => undefined)
}
