import { existsSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { mkdir, readFile } from 'node:fs/promises'
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
const lateGuestAuth = await json('/auth/anonymous', {
  method: 'POST',
  body: JSON.stringify({ display_name: 'E2E Late Guest', client_type: 'web' }),
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

const browser = await chromium.launch({
  executablePath,
  headless: true,
  args: process.env.CHROME_HOST_RESOLVER_RULES ? [`--host-resolver-rules=${process.env.CHROME_HOST_RESOLVER_RULES}`] : [],
})
const context = await browser.newContext({ viewport: { width: 375, height: 812 }, locale: 'uk-UA' })
await context.addInitScript((storedSession) => {
  localStorage.setItem('photodrop.session.v1', JSON.stringify(storedSession))
}, auth)
const page = await context.newPage()
const pageErrors = []
const requestFailures = []
page.on('pageerror', (error) => pageErrors.push(error.message))
page.on('requestfailed', (request) => requestFailures.push(`${request.method()} ${request.url()}: ${request.failure()?.errorText}`))

try {
  const onboardingContext = await browser.newContext({ viewport: { width: 375, height: 812 }, locale: 'uk-UA' })
  try {
    const onboardingPage = await onboardingContext.newPage()
    await onboardingPage.goto(baseURL, { waitUntil: 'domcontentloaded' })
    const onboardingDialog = onboardingPage.getByRole('dialog', { name: 'Створіть кімнату' })
    await onboardingDialog.waitFor()
    const onboardingTargets = await onboardingDialog.locator('button:visible, a:visible').evaluateAll((elements) => elements
      .map((element) => ({ label: element.getAttribute('aria-label') || element.textContent?.trim(), ...element.getBoundingClientRect().toJSON() }))
      .filter((box) => box.width < 44 || box.height < 44))
    if (onboardingTargets.length) throw new Error(`Onboarding touch targets below 44px: ${JSON.stringify(onboardingTargets)}`)
    await onboardingPage.screenshot({ path: resolve(artifacts, 'onboarding-375.png'), fullPage: true })
    await onboardingDialog.getByRole('button', { name: 'Далі' }).click()
    await onboardingPage.getByRole('dialog', { name: 'Запросіть друзів' }).waitFor()
    await onboardingPage.getByRole('button', { name: 'Далі' }).click()
    await onboardingPage.getByRole('dialog', { name: 'Збережіть потрібне' }).waitFor()
    await onboardingPage.getByRole('button', { name: 'Почати' }).click()
    await onboardingPage.getByRole('dialog').waitFor({ state: 'hidden' })
    await onboardingPage.reload({ waitUntil: 'domcontentloaded' })
    if (await onboardingPage.getByRole('dialog').count()) throw new Error('Completed onboarding reopened after reload')
    await onboardingPage.getByRole('button', { name: 'Як працює Zhyvo' }).click()
    await onboardingPage.getByRole('dialog', { name: 'Створіть кімнату' }).waitFor()
    await onboardingPage.getByRole('button', { name: 'Закрити знайомство' }).click()
  } finally {
    await onboardingContext.close()
  }

  await page.goto(baseURL, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: 'Мої кімнати' }).waitFor()
  await page.evaluate((roomSlug) => {
    window.history.pushState({ usr: { justCreated: true }, key: 'e2e-activation', idx: 1 }, '', `/r/${roomSlug}`)
    window.dispatchEvent(new PopStateEvent('popstate', { state: window.history.state }))
  }, slug)
  await page.getByRole('heading', { name: 'Mobile UX Check' }).waitFor()
  const activationPanel = page.getByRole('region', { name: 'Запустіть спільну галерею' })
  await activationPanel.waitFor()
  await activationPanel.getByText('Запросіть друзів').waitFor()
  await activationPanel.getByText('Додайте перший кадр').waitFor()
  const activationTargets = await activationPanel.locator('button:visible').evaluateAll((elements) => elements
    .map((element) => ({ label: element.getAttribute('aria-label') || element.textContent?.trim(), ...element.getBoundingClientRect().toJSON() }))
    .filter((box) => box.width < 44 || box.height < 44))
  if (activationTargets.length) throw new Error(`Room activation touch targets below 44px: ${JSON.stringify(activationTargets)}`)
  await page.screenshot({ path: resolve(artifacts, 'room-activation-375.png'), fullPage: true })
  await activationPanel.getByRole('button', { name: 'Відкрити запрошення' }).click()
  const activationShareDialog = page.getByRole('dialog', { name: 'Запросити друзів' })
  await activationShareDialog.waitFor()
  await activationShareDialog.getByRole('button', { name: 'Закрити' }).click()
  await activationPanel.getByRole('button', { name: 'Запросити ще' }).waitFor()
  await activationPanel.getByRole('button', { name: 'Закрити підказки' }).click()
  await page.reload({ waitUntil: 'domcontentloaded' })
  if (await page.locator('.room-activation').count()) throw new Error('Room activation reopened after reload')

  await page.goto(`${baseURL}/r/${slug}`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: 'Mobile UX Check' }).waitFor()
  if (await page.locator('.room-activation').count()) throw new Error('Direct room navigation showed owner activation')

  const realtimeResponse = page.waitForResponse((response) => response.url().endsWith(`/api/v1/rooms/${slug}/events`) && response.status() === 200)
  await page.goto(`${baseURL}/?tgWebAppStartParam=room_${slug}`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: 'Mobile UX Check' }).waitFor()
  await realtimeResponse
  if (new URL(page.url()).pathname !== `/r/${slug}`) throw new Error(`Telegram start parameter did not route to room: ${page.url()}`)

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

  let interruptedPUTs = 0
  await page.route('**/*', async (route) => {
    if (route.request().method() === 'PUT' && interruptedPUTs < 5) {
      interruptedPUTs += 1
      await route.abort('connectionreset')
      return
    }
    await route.continue()
  })
  await page.locator('input[type="file"]:not([data-resume-upload])').setInputFiles(resolve('public', 'pwa-64x64.png'))
  await page.getByRole('button', { name: 'Повторити' }).waitFor({ timeout: 20_000 })
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: 'Mobile UX Check' }).waitFor()
  const restoreButton = page.getByRole('button', { name: 'Вибрати файл' })
  await restoreButton.waitFor()
  await page.screenshot({ path: resolve(artifacts, 'upload-recovery-375.png'), fullPage: true })
  await page.unroute('**/*')
  await restoreButton.click()
  await page.locator('input[data-resume-upload]').setInputFiles(resolve('public', 'pwa-64x64.png'))
  await page.locator('article.media-card').waitFor({ timeout: 20_000 }).catch(async (cause) => {
    const queueState = await page.locator('.upload-queue').innerText().catch(() => 'upload queue missing')
    throw new Error(`Recovered upload did not finish: ${queueState}; browser errors: ${pageErrors.join('; ')}; requests: ${requestFailures.slice(-8).join('; ')}`, { cause })
  })
  await page.getByRole('button', { name: 'Приховати завершені' }).click()
  let duplicateStoragePUTs = 0
  const countDuplicateStoragePUT = (request) => {
    if (request.method() === 'PUT' && request.url().includes('/photodrop-media/')) duplicateStoragePUTs += 1
  }
  page.on('request', countDuplicateStoragePUT)
  const duplicateBytes = await readFile(resolve('public', 'pwa-64x64.png'))
  await page.locator('input[type="file"]:not([data-resume-upload])').setInputFiles({ name: 'same-photo-renamed.png', mimeType: 'image/png', buffer: duplicateBytes })
  await page.getByText('Вже є в галереї').waitFor({ timeout: 20_000 })
  page.off('request', countDuplicateStoragePUT)
  if (duplicateStoragePUTs !== 0) throw new Error(`Duplicate uploaded ${duplicateStoragePUTs} object-storage requests`)
  const deduplicatedGallery = await json(`/rooms/${slug}/media?limit=50`, { headers: { Authorization: `Bearer ${auth.access_token}` } })
  if (deduplicatedGallery.items.length !== 1) throw new Error(`Duplicate changed gallery size to ${deduplicatedGallery.items.length}`)
  await page.getByRole('button', { name: 'Приховати завершені' }).click()
  let ownerGallery
  for (let attempt = 0; attempt < 30; attempt += 1) {
    ownerGallery = await json(`/rooms/${slug}/media?limit=50`, { headers: { Authorization: `Bearer ${auth.access_token}` } })
    if (ownerGallery.items[0]?.thumbnail_status === 'ready') break
    await new Promise((resolveDelay) => setTimeout(resolveDelay, 500))
  }
  const uploadedMediaID = ownerGallery.items[0]?.id
  if (!uploadedMediaID) throw new Error('Uploaded media is missing from gallery API')
  if (ownerGallery.items[0].thumbnail_status !== 'ready' || !ownerGallery.items[0].thumbnail_url) throw new Error(`Thumbnail did not become ready: ${ownerGallery.items[0].thumbnail_status}`)

  const expiryContext = await browser.newContext({ viewport: { width: 375, height: 812 }, locale: 'uk-UA' })
  try {
    await expiryContext.addInitScript((storedSession) => {
      localStorage.setItem('photodrop.session.v1', JSON.stringify(storedSession))
    }, auth)
    const expiryPage = await expiryContext.newPage()
    await expiryPage.route(`**/api/v1/rooms/${slug}`, async (route) => {
      if (route.request().method() !== 'GET') return route.continue()
      const response = await route.fetch()
      const body = await response.json()
      body.room.expires_at = new Date(Date.now() + 5 * 60 * 60 * 1000).toISOString()
      await route.fulfill({ response, json: body })
    })
    await expiryPage.goto(`${baseURL}/r/${slug}`, { waitUntil: 'domcontentloaded' })
    const expiryWarning = expiryPage.getByRole('region', { name: /годин, щоб зберегти файли/ })
    await expiryWarning.waitFor()
    await expiryWarning.getByRole('button', { name: 'Зберегти файли' }).waitFor()
    await expiryWarning.getByRole('button', { name: 'Продовжити строк' }).waitFor()
    const expiryTargets = await expiryWarning.locator('button:visible').evaluateAll((elements) => elements
      .map((element) => ({ label: element.textContent?.trim(), ...element.getBoundingClientRect().toJSON() }))
      .filter((box) => box.width < 44 || box.height < 44))
    if (expiryTargets.length) throw new Error(`Expiry warning touch targets below 44px: ${JSON.stringify(expiryTargets)}`)
    await expiryPage.screenshot({ path: resolve(artifacts, 'expiry-warning-375.png'), fullPage: true })
  } finally {
    await expiryContext.close()
  }

  await page.getByRole('button', { name: 'Додати в обране pwa-64x64.png' }).click()
  await page.getByRole('button', { name: 'Прибрати з обраного pwa-64x64.png' }).getByText('1').waitFor()
  const guestFavorite = await json(`/media/${uploadedMediaID}/favorite`, {
    method: 'PUT', headers: { Authorization: `Bearer ${guestAuth.access_token}` },
  })
  if (guestFavorite.favorite_count !== 2 || !guestFavorite.favorited) throw new Error('Guest favorite was not counted')
  await page.getByRole('button', { name: 'Прибрати з обраного pwa-64x64.png' }).getByText('2').waitFor({ timeout: 4000 })
  const forbiddenCaption = await fetch(`${baseURL}/api/v1/media/${uploadedMediaID}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${guestAuth.access_token}` }, body: JSON.stringify({ caption: 'Чужий підпис' }),
  })
  if (forbiddenCaption.status !== 403) throw new Error(`Non-uploader edited caption with status ${forbiddenCaption.status}`)
  const oversizedCaption = await fetch(`${baseURL}/api/v1/media/${uploadedMediaID}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${auth.access_token}` }, body: JSON.stringify({ caption: 'ї'.repeat(301) }),
  })
  if (oversizedCaption.status !== 422) throw new Error(`Oversized caption returned ${oversizedCaption.status}`)
  const forbiddenCover = await fetch(`${baseURL}/api/v1/rooms/${slug}/cover`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${guestAuth.access_token}` },
    body: JSON.stringify({ media_id: uploadedMediaID }),
  })
  if (forbiddenCover.status !== 403) throw new Error(`Non-owner set room cover with status ${forbiddenCover.status}`)
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: 'Прибрати з обраного pwa-64x64.png' }).getByText('2').waitFor()
  await page.locator('.gallery-filters button').filter({ hasText: 'Фото' }).click()
  await page.locator('article.media-card').waitFor()
  await page.locator('.gallery-filters button').filter({ hasText: 'Відео' }).click()
  await page.getByText('У вибраному фільтрі немає файлів.').waitFor()
  await page.getByRole('button', { name: 'Показати всі' }).click()
  await page.locator('.gallery-filters button').filter({ hasText: 'Обрані' }).click()
  await page.locator('article.media-card').waitFor()
  await page.locator('.gallery-filters button').filter({ hasText: 'Найкращі' }).click()
  await page.getByRole('heading', { name: 'Найкращі кадри' }).waitFor()
  await page.locator('.gallery-filters button').filter({ hasText: 'Усі' }).click()
  await page.getByRole('button', { name: 'Вибрати' }).click()
  await page.locator('article.media-card .media-preview').first().click()
  const selectionBar = page.locator('.selection-bar')
  await selectionBar.getByText('1 вибрано').waitFor()
  await selectionBar.getByRole('button', { name: 'Скасувати вибір' }).click()
  await page.getByRole('button', { name: 'Завантажити всю галерею' }).click()
  await page.getByText('Архів готовий').waitFor({ timeout: 30_000 })
  await page.screenshot({ path: resolve(artifacts, 'archive-ready-375.png'), fullPage: true })
  const downloadEvent = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Завантажити всю галерею' }).click()
  const archiveDownload = await downloadEvent
  const archivePath = resolve(artifacts, 'room.zip')
  await archiveDownload.saveAs(archivePath)
  const archiveBytes = await readFile(archivePath)
  if (archiveBytes[0] !== 0x50 || archiveBytes[1] !== 0x4b) throw new Error('Downloaded room archive is not a ZIP file')
  const reusedArchive = await json(`/rooms/${slug}/archive`, {
    method: 'POST', headers: { Authorization: `Bearer ${auth.access_token}` },
  })
  const reusedAgain = await json(`/rooms/${slug}/archive`, {
    method: 'POST', headers: { Authorization: `Bearer ${auth.access_token}` },
  })
  if (reusedArchive.archive.id !== reusedAgain.archive.id || reusedAgain.archive.status !== 'ready') throw new Error('Ready archive was not reused')
  await page.locator('article.media-card .media-preview').first().click()
  const viewer = page.getByRole('dialog', { name: 'pwa-64x64.png' })
  await viewer.waitFor()
  if (!page.url().includes('media=')) throw new Error('Media viewer did not update the URL')
  await viewer.getByRole('button', { name: 'Додати підпис' }).click()
  await viewer.getByLabel('Підпис').fill('Перший кадр події')
  await viewer.getByRole('button', { name: 'Зберегти', exact: true }).click()
  await viewer.getByText('Перший кадр події').waitFor()
  for (const label of ['Автор', 'Завантажено', 'Роздільність', 'Файл']) await viewer.getByText(label, { exact: true }).waitFor()
  await json(`/media/${uploadedMediaID}`, {
    method: 'PATCH', headers: { Authorization: `Bearer ${auth.access_token}` }, body: JSON.stringify({ caption: 'Оновлено наживо' }),
  })
  await viewer.getByText('Оновлено наживо').waitFor({ timeout: 4000 })
  await page.getByRole('button', { name: 'Зробити обкладинкою кімнати' }).click()
  await page.getByRole('button', { name: 'Поточна обкладинка кімнати' }).waitFor()
  await page.screenshot({ path: resolve(artifacts, 'viewer-375.png') })
  await page.getByRole('button', { name: 'Закрити перегляд' }).click()
  await page.getByText('Обкладинка').waitFor()
  await page.getByRole('button', { name: 'Налаштування кімнати' }).click()
  const settingsDialog = page.getByRole('dialog', { name: 'Налаштування' })
  await settingsDialog.waitFor()
  const settingsTargets = await settingsDialog.locator('button:visible').evaluateAll((elements) => elements
    .map((element) => ({ label: element.getAttribute('aria-label') || element.textContent?.trim(), ...element.getBoundingClientRect().toJSON() }))
    .filter((box) => box.width < 44 || box.height < 44))
  if (settingsTargets.length) throw new Error(`Settings touch targets below 44px: ${JSON.stringify(settingsTargets)}`)
  await settingsDialog.getByLabel('Назва кімнати').fill('Mobile UX Updated')
  await settingsDialog.locator('.lifetime-slider').fill('3')
  await settingsDialog.getByRole('button', { name: 'Зберегти зміни' }).click()
  await page.getByRole('heading', { name: 'Mobile UX Updated' }).waitFor()
  await page.getByRole('button', { name: 'Налаштування кімнати' }).click()
  await settingsDialog.waitFor()
  await settingsDialog.locator('.setting-row').filter({ hasText: 'Приймати нових учасників' }).getByRole('switch').click()
  await settingsDialog.getByRole('button', { name: 'Переглянути' }).click()
  const membersDialog = page.getByRole('dialog', { name: 'Керування кімнатою' })
  await membersDialog.waitFor()
  await membersDialog.getByText('E2E Guest').waitFor()
  await membersDialog.getByRole('tab', { name: 'Історія' }).click()
  await membersDialog.getByText(/приєднався/).first().waitFor()
  await membersDialog.getByRole('button', { name: 'Закрити' }).click()

  const closedJoin = await fetch(`${baseURL}/api/v1/rooms/${slug}/join`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${lateGuestAuth.access_token}` },
    body: JSON.stringify({}),
  })
  if (closedJoin.status !== 403) throw new Error(`Closed room allowed a new member with status ${closedJoin.status}`)
  await json(`/rooms/${slug}`, {
    method: 'PATCH',
    headers: { Authorization: `Bearer ${auth.access_token}` },
    body: JSON.stringify({ accepting_members: true }),
  })

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

  await page.setViewportSize({ width: 375, height: 500 })
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight))
  const galleryHeadingTop = await page.locator('.gallery-toolbar').evaluate((element) => element.getBoundingClientRect().top)
  if (galleryHeadingTop >= 0) throw new Error(`Live shelf precondition failed: gallery heading top is ${galleryHeadingTop}`)
  const liveBytes = await readFile(resolve('public', 'pwa-192x192.png'))
  const liveChecksum = createHash('sha256').update(liveBytes).digest('hex')
  const liveUpload = await json(`/rooms/${slug}/uploads`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${guestAuth.access_token}`, 'Idempotency-Key': crypto.randomUUID() },
    body: JSON.stringify({ filename: 'guest-live-photo.png', mime_type: 'image/png', size_bytes: liveBytes.length, checksum: liveChecksum }),
  })
  const livePUT = await fetch(liveUpload.upload.url, { method: 'PUT', headers: liveUpload.upload.headers, body: liveBytes })
  if (!livePUT.ok) throw new Error(`Live test object upload failed with ${livePUT.status}`)
  await json(`/uploads/${liveUpload.upload.id}/complete`, {
    method: 'POST', headers: { Authorization: `Bearer ${guestAuth.access_token}` }, body: JSON.stringify({ parts: [] }),
  })
  const newMediaShelf = page.locator('.new-media-shelf')
  await newMediaShelf.getByText('Нових файлів: 1').waitFor({ timeout: 5000 })
  const shelfButtonBox = await newMediaShelf.getByRole('button', { name: 'Показати' }).boundingBox()
  if (!shelfButtonBox || shelfButtonBox.width < 44 || shelfButtonBox.height < 44) throw new Error(`New-media shelf touch target is too small: ${JSON.stringify(shelfButtonBox)}`)
  await page.screenshot({ path: resolve(artifacts, 'new-media-shelf-375.png') })
  await newMediaShelf.getByRole('button', { name: 'Показати' }).click()

  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto(baseURL, { waitUntil: 'domcontentloaded' })
  await page.getByRole('heading', { name: 'Мої кімнати' }).waitFor()
  await page.getByText('Mobile UX Updated').waitFor()
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
  console.log(JSON.stringify({ slug, portraitOverflow, homeOverflow, overflows, touchTargets: true, onboarding: true, roomActivation: true, expiryWarning: true, qr: true, telegramDeepLink: true, upload: true, uploadRecovery: true, checksumDeduplication: true, realtime: true, newMediaShelf: true, mediaCaptions: true, mediaDetails: true, galleryFilters: true, favorites: true, bestSort: true, roomCover: true, batchSelection: true, archive: true, viewer: true, myRooms: true, roomLifecycle: true, joiningClosed: true, members: true, moderation: true, ownershipTransfer: true, activity: true, telegramLightTheme: true }))
} finally {
  await context.close()
  await browser.close()
  await json(`/rooms/${slug}`, { method: 'DELETE', headers: { Authorization: `Bearer ${cleanupAuth.access_token}` } }).catch(() => undefined)
  await json('/auth/session', { method: 'DELETE', headers: { Authorization: `Bearer ${auth.access_token}` } }).catch(() => undefined)
  await json('/auth/session', { method: 'DELETE', headers: { Authorization: `Bearer ${guestAuth.access_token}` } }).catch(() => undefined)
  await json('/auth/session', { method: 'DELETE', headers: { Authorization: `Bearer ${lateGuestAuth.access_token}` } }).catch(() => undefined)
}
