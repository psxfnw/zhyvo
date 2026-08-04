import type { UploadProgress } from '../types'

const STORAGE_PREFIX = 'zhyvo.upload.queue.v1'
const MAX_QUEUE_AGE = 20 * 60 * 60 * 1000

function storageKey(identityID: string, slug: string) {
  return `${STORAGE_PREFIX}.${identityID}.${slug}`
}

function isStoredUpload(value: unknown): value is UploadProgress {
  if (!value || typeof value !== 'object') return false
  const item = value as Partial<UploadProgress>
  return typeof item.id === 'string'
    && typeof item.filename === 'string'
    && typeof item.size_bytes === 'number'
    && typeof item.idempotency_key === 'string'
    && typeof item.created_at === 'string'
}

export function loadUploadQueue(identityID: string, slug: string): UploadProgress[] {
  try {
    const raw = localStorage.getItem(storageKey(identityID, slug))
    const parsed = raw ? JSON.parse(raw) as unknown : []
    if (!Array.isArray(parsed)) return []
    const cutoff = Date.now() - MAX_QUEUE_AGE
    return parsed.filter(isStoredUpload).filter((item) => new Date(item.created_at).getTime() > cutoff).map((item) => ({
      ...item,
      state: 'waiting_file',
      message: 'Виберіть цей файл повторно',
      canRetry: true,
    }))
  } catch {
    return []
  }
}

export function saveUploadQueue(identityID: string, slug: string, uploads: UploadProgress[]) {
  try {
    const pending = uploads.filter((item) => item.state !== 'done' && !(item.state === 'error' && !item.canRetry))
    const key = storageKey(identityID, slug)
    if (pending.length === 0) {
      localStorage.removeItem(key)
      return
    }
    localStorage.setItem(key, JSON.stringify(pending))
  } catch {
    // Uploading still works when storage is unavailable; only cross-reload
    // recovery is disabled for this browser session.
  }
}
