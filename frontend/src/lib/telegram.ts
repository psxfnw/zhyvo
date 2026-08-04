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
  if (!candidate?.initData) return null
  webApp = candidate
  applyTheme()
  candidate.onEvent('themeChanged', applyTheme)
  candidate.ready()
  candidate.expand()
  candidate.setHeaderColor('#f5f5f7')
  candidate.setBackgroundColor('#f5f5f7')
  candidate.setBottomBarColor?.('#f5f5f7')

  const startParam = candidate.initDataUnsafe?.start_param ?? new URLSearchParams(location.search).get('tgWebAppStartParam') ?? ''
  const slug = startParam.replace(/^room[_-]/i, '').toUpperCase()
  if (location.pathname === '/' && /^[A-Z0-9]{6,12}$/.test(slug)) {
    history.replaceState(history.state, '', `/r/${slug}`)
  }
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
