import type { AccessMode, BlockedRoomMember, GalleryPage, Identity, Room, RoomActivityEvent, RoomArchive, RoomMember, RoomNotificationSettings, RoomPreview, Session, UploadTicket } from '../types'

const SESSION_KEY = 'photodrop.session.v1'

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
  }
}

function loadStoredSession(): Session | null {
  try {
    const raw = localStorage.getItem(SESSION_KEY)
    return raw ? (JSON.parse(raw) as Session) : null
  } catch {
    localStorage.removeItem(SESSION_KEY)
    return null
  }
}

let session = loadStoredSession()
let refreshPromise: Promise<Session> | null = null

export function getSession() {
  return session
}

function saveSession(next: Session | null) {
  session = next
  if (next) localStorage.setItem(SESSION_KEY, JSON.stringify(next))
  else localStorage.removeItem(SESSION_KEY)
  window.dispatchEvent(new CustomEvent('photodrop:session'))
}

async function parseError(response: Response) {
  const fallback = `Запит завершився з кодом ${response.status}`
  try {
    const body = await response.json() as { error?: { code?: string; message?: string } }
    const code = body.error?.code ?? 'REQUEST_FAILED'
    const message = code === 'AUTH_REQUIRED'
      ? 'Сесію завершено. Закрийте й повторно відкрийте Zhyvo.'
      : body.error?.message ?? fallback
    return new ApiError(response.status, code, message)
  } catch {
    return new ApiError(response.status, 'REQUEST_FAILED', fallback)
  }
}

async function refreshSession() {
  if (!session?.refresh_token) throw new ApiError(401, 'AUTH_REQUIRED', 'Потрібно вказати своє ім’я')
  if (!refreshPromise) {
    refreshPromise = fetch('/api/v1/auth/refresh', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: session.refresh_token }),
    }).then(async (response) => {
      if (!response.ok) {
        saveSession(null)
        throw await parseError(response)
      }
      const next = await response.json() as Session
      saveSession(next)
      return next
    }).finally(() => { refreshPromise = null })
  }
  return refreshPromise
}

interface RequestOptions extends RequestInit {
  auth?: boolean
  retryAuth?: boolean
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { auth = true, retryAuth = true, ...init } = options
  const headers = new Headers(init.headers)
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
  if (auth && session?.access_token) headers.set('Authorization', `Bearer ${session.access_token}`)

  const response = await fetch(`/api/v1${path}`, { ...init, headers })
  if (response.status === 401 && auth && retryAuth && session?.refresh_token) {
    await refreshSession()
    return api<T>(path, { ...options, retryAuth: false })
  }
  if (!response.ok) throw await parseError(response)
  if (response.status === 204) return undefined as T
  const body = await response.text()
  if (!body) return undefined as T
  return JSON.parse(body) as T
}

export type RoomRealtimeEvent = {
  id: number
  type: 'media_ready' | 'media_updated' | 'media_deleted' | 'favorite_changed' | 'room_updated' | 'members_changed'
  entity_id?: string
  created_at: string
}

type RealtimeCallbacks = {
  onEvent: (event: RoomRealtimeEvent) => void
  onConnected?: () => void
  onDisconnected?: () => void
  onAccessRevoked?: () => void
}

export async function streamRoomEvents(slug: string, callbacks: RealtimeCallbacks, signal: AbortSignal) {
  let lastEventID = ''
  let retryDelay = 1000
  while (!signal.aborted) {
    try {
      const headers = new Headers({ Accept: 'text/event-stream' })
      if (session?.access_token) headers.set('Authorization', `Bearer ${session.access_token}`)
      if (lastEventID) headers.set('Last-Event-ID', lastEventID)
      let response = await fetch(`/api/v1/rooms/${encodeURIComponent(slug)}/events`, { headers, signal })
      if (response.status === 401 && session?.refresh_token) {
        await refreshSession()
        headers.set('Authorization', `Bearer ${session?.access_token ?? ''}`)
        response = await fetch(`/api/v1/rooms/${encodeURIComponent(slug)}/events`, { headers, signal })
      }
      if (response.status === 403) {
        callbacks.onAccessRevoked?.()
        return
      }
      if (!response.ok || !response.body) throw new Error(`Realtime connection failed with ${response.status}`)

      retryDelay = 1000
      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      while (!signal.aborted) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        let boundary = buffer.indexOf('\n\n')
        while (boundary >= 0) {
          const frame = buffer.slice(0, boundary)
          buffer = buffer.slice(boundary + 2)
          const fields = new Map<string, string>()
          for (const line of frame.split('\n')) {
            if (!line || line.startsWith(':')) continue
            const separator = line.indexOf(':')
            if (separator < 0) continue
            fields.set(line.slice(0, separator), line.slice(separator + 1).trimStart())
          }
          const id = fields.get('id')
          if (id) lastEventID = id
          const eventType = fields.get('event')
          const data = fields.get('data')
          if (eventType === 'ready') callbacks.onConnected?.()
          if (eventType === 'access_revoked') {
            callbacks.onAccessRevoked?.()
            return
          }
          if (eventType === 'room' && data) callbacks.onEvent(JSON.parse(data) as RoomRealtimeEvent)
          boundary = buffer.indexOf('\n\n')
        }
      }
      if (!signal.aborted) callbacks.onDisconnected?.()
    } catch (cause) {
      if (signal.aborted) return
      callbacks.onDisconnected?.()
      if (cause instanceof ApiError && cause.status === 401) return
    }
    if (signal.aborted) return
    await new Promise<void>((resolve) => {
      const finish = () => {
        signal.removeEventListener('abort', onAbort)
        resolve()
      }
      const timer = window.setTimeout(finish, retryDelay)
      const onAbort = () => {
        window.clearTimeout(timer)
        finish()
      }
      signal.addEventListener('abort', onAbort, { once: true })
    })
    retryDelay = Math.min(retryDelay * 2, 8000)
  }
}

export async function createAnonymous(displayName: string) {
  const next = await api<Session>('/auth/anonymous', {
    method: 'POST', auth: false, body: JSON.stringify({ display_name: displayName.trim(), client_type: 'web' }),
  })
  saveSession(next)
  return next
}

export async function createTelegramSession(initData: string) {
  const previous = session
  const next = await api<Session>('/auth/telegram', {
    method: 'POST', auth: false, body: JSON.stringify({ init_data: initData }),
  })
  if (previous?.identity.kind === 'telegram') {
    void fetch('/api/v1/auth/session', {
      method: 'DELETE', headers: { Authorization: `Bearer ${previous.access_token}` },
    }).catch(() => undefined)
  }
  saveSession(next)
  return next
}

export async function ensureIdentity(displayName: string) {
  if (session) return session.identity
  return (await createAnonymous(displayName)).identity
}

export const auth = {
  telegramConfig: () => api<{ enabled: boolean; client_id: string }>('/auth/telegram/config', { auth: false }),
  linkTelegram: async (input: { code: string; code_verifier: string; nonce: string }) => {
    const next = await api<Session>('/auth/telegram/oidc', { method: 'POST', body: JSON.stringify(input) })
    saveSession(next)
    return next
  },
  createBrowserLink: createOrReuseBrowserLink,
  browserLinkStatus: (token: string) => api<{ status: string; expires_at: string }>('/auth/telegram/link-challenges/status', { method: 'POST', body: JSON.stringify({ token }) }),
  approveBrowserLink: (token: string) => api<{ status: string }>('/auth/telegram/link-challenges/approve', { method: 'POST', body: JSON.stringify({ token }) }),
  exchangeBrowserLink: async (token: string) => {
    const next = await api<Session>('/auth/telegram/link-challenges/exchange', { method: 'POST', body: JSON.stringify({ token }) })
    sessionStorage.removeItem(BROWSER_LINK_KEY)
    saveSession(next)
    return next
  },
}

const BROWSER_LINK_KEY = 'photodrop.telegram.browser-link.v1'
let browserLinkPromise: Promise<{ token: string; status: string; expires_at: string }> | null = null

function createOrReuseBrowserLink() {
  try {
    const raw = sessionStorage.getItem(BROWSER_LINK_KEY)
    if (raw) {
      const stored = JSON.parse(raw) as { token: string; status: string; expires_at: string }
      if (stored.token && Date.parse(stored.expires_at) > Date.now() + 10_000) return Promise.resolve(stored)
      sessionStorage.removeItem(BROWSER_LINK_KEY)
    }
  } catch { sessionStorage.removeItem(BROWSER_LINK_KEY) }
  if (!browserLinkPromise) {
    browserLinkPromise = api<{ token: string; status: string; expires_at: string }>('/auth/telegram/link-challenges', { method: 'POST' })
      .then((challenge) => { sessionStorage.setItem(BROWSER_LINK_KEY, JSON.stringify(challenge)); return challenge })
      .finally(() => { browserLinkPromise = null })
  }
  return browserLinkPromise
}

export const rooms = {
	list: () => api<{ rooms: Room[] }>('/rooms'),
  create: (input: { name: string; lifetime_days: number; access: { mode: AccessMode; secret?: string } }) =>
    api<{ room: Room; share_path: string }>('/rooms', {
      method: 'POST', headers: { 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(input),
    }),
  preview: (slug: string) => api<RoomPreview>(`/rooms/${slug}/preview`, { auth: false }),
  join: (slug: string, secret = '') => api<{ room: Room }>(`/rooms/${slug}/join`, {
    method: 'POST', body: JSON.stringify({ secret }),
  }),
  get: (slug: string) => api<{ room: Room }>(`/rooms/${slug}`),
	members: (slug: string) => api<{ members: RoomMember[]; blocked_members: BlockedRoomMember[] }>(`/rooms/${slug}/members`),
  removeMember: (slug: string, identityID: string) => api<void>(`/rooms/${slug}/members/${identityID}`, { method: 'DELETE' }),
  unblockMember: (slug: string, identityID: string) => api<void>(`/rooms/${slug}/blocked-members/${identityID}`, { method: 'DELETE' }),
  transferOwnership: (slug: string, identityID: string) => api<{ room: Room }>(`/rooms/${slug}/ownership`, {
    method: 'POST', body: JSON.stringify({ identity_id: identityID }),
  }),
  activity: (slug: string) => api<{ events: RoomActivityEvent[] }>(`/rooms/${slug}/activity`),
  update: (slug: string, input: Partial<{ name: string; accepting_uploads: boolean; accepting_members: boolean; access: { mode: AccessMode; secret: string }; lifetime_days: number }>) =>
    api<{ room: Room }>(`/rooms/${slug}`, { method: 'PATCH', body: JSON.stringify(input) }),
  notifications: (slug: string) => api<RoomNotificationSettings>(`/rooms/${slug}/notifications`),
  updateNotifications: (slug: string, telegramEnabled: boolean) => api<RoomNotificationSettings>(`/rooms/${slug}/notifications`, { method: 'PATCH', body: JSON.stringify({ telegram_enabled: telegramEnabled }) }),
  remove: (slug: string) => api<void>(`/rooms/${slug}`, { method: 'DELETE' }),
}

export const media = {
  gallery: (slug: string, cursor?: string | null) => api<GalleryPage>(
    `/rooms/${slug}/media?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`,
  ),
  download: (id: string) => api<{ url: string; filename: string; expires_at: string }>(`/media/${id}/download-url`, { method: 'POST' }),
  favorite: (id: string, enabled: boolean) => api<{ favorite_count: number; favorited: boolean }>(`/media/${id}/favorite`, { method: enabled ? 'PUT' : 'DELETE' }),
  setCover: (slug: string, id: string) => api<void>(`/rooms/${slug}/cover`, { method: 'PUT', body: JSON.stringify({ media_id: id }) }),
  remove: (id: string) => api<void>(`/media/${id}`, { method: 'DELETE' }),
  initiate: (slug: string, file: File, signal?: AbortSignal, idempotencyKey: string = crypto.randomUUID(), mimeType = file.type, capturedAt?: string | null, checksum?: string) => api<{ upload: UploadTicket; media_id: string }>(`/rooms/${slug}/uploads`, {
    method: 'POST',
    signal,
    headers: { 'Idempotency-Key': idempotencyKey },
    body: JSON.stringify({ filename: file.name, mime_type: mimeType, size_bytes: file.size, captured_at: capturedAt ?? null, checksum }),
  }),
  partURLs: (uploadID: string, partNumbers: number[], signal?: AbortSignal) => api<{ parts: Array<{ part_number: number; url: string }> }>(`/uploads/${uploadID}/parts`, {
    method: 'POST', signal, body: JSON.stringify({ part_numbers: partNumbers }),
  }),
  complete: (uploadID: string, parts: Array<{ part_number: number; etag: string }> = [], signal?: AbortSignal) =>
    api<{ media: { id: string; status: string } }>(`/uploads/${uploadID}/complete`, {
      method: 'POST', signal, body: JSON.stringify({ parts }),
    }),
  abort: (uploadID: string) => api<void>(`/uploads/${uploadID}`, { method: 'DELETE' }),
}

export const archives = {
  request: (slug: string) => api<{ archive: RoomArchive }>(`/rooms/${slug}/archive`, { method: 'POST' }),
  get: (id: string) => api<{ archive: RoomArchive }>(`/archives/${id}`),
  download: (id: string) => api<{ url: string; filename: string; expires_at: string }>(`/archives/${id}/download-url`, { method: 'POST' }),
}

export type { Identity }
