import { createTelegramSession, getSession } from './api'

type TelegramEventHandler = () => void

export interface TelegramWebApp {
  initData: string
  initDataUnsafe?: { start_param?: string }
  colorScheme: 'light' | 'dark'
  platform: string
  version: string
  ready: () => void
  expand: () => void
  setHeaderColor: (color: string) => void
  setBackgroundColor: (color: string) => void
  setBottomBarColor?: (color: string) => void
  openTelegramLink?: (url: string) => void
  onEvent: (event: string, handler: TelegramEventHandler) => void
  offEvent: (event: string, handler: TelegramEventHandler) => void
  BackButton: { show: () => void; hide: () => void }
  HapticFeedback?: { impactOccurred: (style: 'light' | 'medium' | 'heavy') => void }
}

declare global {
  interface Window {
    Telegram?: { WebApp?: TelegramWebApp }
  }
}

let webApp: TelegramWebApp | null = null
let bootstrapError = ''
const TELEGRAM_SESSION_FINGERPRINT = 'photodrop.telegram.session.v1'

function startParamFromURLParameters(parameters: URLSearchParams) {
  const direct = parameters.get('tgWebAppStartParam')
  if (direct) return direct
  const embeddedInitData = parameters.get('tgWebAppData')
  return embeddedInitData ? new URLSearchParams(embeddedInitData).get('start_param') ?? '' : ''
}

export function getTelegramStartParam(candidate = window.Telegram?.WebApp) {
  const search = new URLSearchParams(location.search)
  const hash = new URLSearchParams(location.hash.replace(/^#/, ''))
  const rawInitData = candidate?.initData ? new URLSearchParams(candidate.initData) : null
  return [
    candidate?.initDataUnsafe?.start_param,
    rawInitData?.get('start_param'),
    startParamFromURLParameters(search),
    startParamFromURLParameters(hash),
  ].find((value) => value?.trim())?.trim() ?? ''
}

function applyTelegramStartRoute(startParam: string) {
  const slug = startParam.replace(/^room[_-]/i, '').toUpperCase()
  if (location.pathname === '/' && /^[A-Z0-9]{6,12}$/.test(slug)) {
    history.replaceState(history.state, '', `/r/${slug}`)
  }
}

function applyTheme() {
  if (!webApp) return
  document.documentElement.dataset.telegram = 'true'
  // Zhyvo intentionally keeps one light visual system inside Telegram. Mixing
  // Telegram's dark theme params with light glass surfaces causes unreadable
  // combinations on clients that override only part of the palette.
  document.documentElement.dataset.telegramTheme = 'light'
  document.documentElement.style.colorScheme = 'light'
}

export function initializeTelegram() {
  const candidate = window.Telegram?.WebApp
  applyTelegramStartRoute(getTelegramStartParam(candidate))
  if (!candidate?.initData) return null
  webApp = candidate
  applyTheme()
  candidate.onEvent('themeChanged', applyTheme)
  candidate.ready()
  candidate.expand()
  candidate.setHeaderColor('#f5f5f7')
  candidate.setBackgroundColor('#f5f5f7')
  candidate.setBottomBarColor?.('#f5f5f7')

  return candidate
}

export async function bootstrapTelegramSession() {
  if (!webApp?.initData) return
  const fingerprint = new URLSearchParams(webApp.initData).get('hash') ?? ''
  if (fingerprint && getSession()?.identity.kind === 'telegram' && localStorage.getItem(TELEGRAM_SESSION_FINGERPRINT) === fingerprint) return
  try {
    await createTelegramSession(webApp.initData)
    localStorage.setItem(TELEGRAM_SESSION_FINGERPRINT, fingerprint)
  } catch {
    bootstrapError = 'Не вдалося підтвердити профіль Telegram. Закрийте й повторно відкрийте Mini App.'
  }
}

export function getTelegramWebApp() {
  return webApp
}

export function getTelegramBootstrapError() {
  return bootstrapError
}

const TELEGRAM_BOT_USERNAME = (import.meta.env.VITE_TELEGRAM_BOT_USERNAME || 'zhyvoappbot').replace(/^@/, '')

export function telegramRoomLink(slug: string) {
  return `https://t.me/${TELEGRAM_BOT_USERNAME}?startapp=room_${slug.toUpperCase()}`
}

export function telegramShareLink(roomName: string, roomURL: string) {
  const params = new URLSearchParams({
    url: roomURL,
    text: `Приєднуйтеся до кімнати «${roomName}» у Zhyvo`,
  })
  return `https://t.me/share/url?${params}`
}

export function openTelegramInvite(roomName: string, roomURL: string) {
  const url = telegramShareLink(roomName, roomURL)
  if (webApp?.openTelegramLink) webApp.openTelegramLink(url)
  else window.open(url, '_blank', 'noopener,noreferrer')
}
