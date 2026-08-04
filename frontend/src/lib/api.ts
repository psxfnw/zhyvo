import type { AccessMode, GalleryPage, Identity, Room, RoomMember, RoomPreview, Session, UploadTicket } from '../types'

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
    return new ApiError(response.status, body.error?.code ?? 'REQUEST_FAILED', body.error?.message ?? fallback)
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
  if (response.status === 204 || response.status === 202) return undefined as T
  return response.json() as Promise<T>
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
	members: (slug: string) => api<{ members: RoomMember[] }>(`/rooms/${slug}/members`),
  update: (slug: string, input: Partial<{ name: string; accepting_uploads: boolean }>) =>
    api<{ room: Room }>(`/rooms/${slug}`, { method: 'PATCH', body: JSON.stringify(input) }),
  remove: (slug: string) => api<void>(`/rooms/${slug}`, { method: 'DELETE' }),
}

export const media = {
  gallery: (slug: string, cursor?: string | null) => api<GalleryPage>(
    `/rooms/${slug}/media?limit=50${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`,
  ),
  download: (id: string) => api<{ url: string; filename: string; expires_at: string }>(`/media/${id}/download-url`, { method: 'POST' }),
  remove: (id: string) => api<void>(`/media/${id}`, { method: 'DELETE' }),
  initiate: (slug: string, file: File, signal?: AbortSignal) => api<{ upload: UploadTicket; media_id: string }>(`/rooms/${slug}/uploads`, {
    method: 'POST',
    signal,
    headers: { 'Idempotency-Key': crypto.randomUUID() },
    body: JSON.stringify({ filename: file.name, mime_type: file.type, size_bytes: file.size, captured_at: null }),
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

export type { Identity }
