import { auth } from './api'

const LOGIN_STATE_KEY = 'photodrop.telegram.oidc.v1'

interface LoginState {
  state: string
  nonce: string
  codeVerifier: string
  returnTo: string
}

function randomBase64URL(bytes = 32) {
  const value = crypto.getRandomValues(new Uint8Array(bytes))
  let binary = ''
  value.forEach((part) => { binary += String.fromCharCode(part) })
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

async function codeChallenge(verifier: string) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier))
  let binary = ''
  new Uint8Array(digest).forEach((part) => { binary += String.fromCharCode(part) })
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

export async function startTelegramLogin(returnTo = '/') {
  const config = await auth.telegramConfig()
  if (!config.enabled || !config.client_id) {
    throw new Error('Вхід через Telegram ще не підключено на сервері')
  }
  const state: LoginState = {
    state: randomBase64URL(),
    nonce: randomBase64URL(),
    codeVerifier: randomBase64URL(64),
    returnTo: returnTo.startsWith('/') ? returnTo : '/',
  }
  sessionStorage.setItem(LOGIN_STATE_KEY, JSON.stringify(state))
  const redirectURI = `${window.location.origin}/auth/telegram/callback`
  const params = new URLSearchParams({
    client_id: config.client_id,
    redirect_uri: redirectURI,
    response_type: 'code',
    scope: 'openid profile',
    state: state.state,
    nonce: state.nonce,
    code_challenge: await codeChallenge(state.codeVerifier),
    code_challenge_method: 'S256',
  })
  window.location.assign(`https://oauth.telegram.org/auth?${params}`)
}

export async function completeTelegramLogin(search: string) {
  const parameters = new URLSearchParams(search)
  const code = parameters.get('code') ?? ''
  const returnedState = parameters.get('state') ?? ''
  const telegramError = parameters.get('error_description') || parameters.get('error')
  if (telegramError) throw new Error('Вхід через Telegram скасовано')

  const raw = sessionStorage.getItem(LOGIN_STATE_KEY)
  if (!raw) throw new Error('Спроба входу застаріла. Почніть ще раз.')
  const state = JSON.parse(raw) as LoginState
  if (!code || !returnedState || returnedState !== state.state) {
    throw new Error('Не вдалося перевірити спробу входу')
  }
  const session = await auth.linkTelegram({ code, code_verifier: state.codeVerifier, nonce: state.nonce })
  sessionStorage.removeItem(LOGIN_STATE_KEY)
  return { session, returnTo: state.returnTo }
}
