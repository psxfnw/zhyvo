import { ApiError, media } from './api'
import type { UploadTicket } from '../types'

const PART_URL_BATCH = 20
const MAX_ATTEMPTS = 5
const RETRYABLE_STATUS = new Set([408, 425, 429, 500, 502, 503, 504])

export interface UploadCheckpoint {
  uploadID: string
  completedParts: Array<{ part_number: number; etag: string }>
}

interface UploadOptions {
  signal?: AbortSignal
  idempotencyKey: string
  mimeType: string
  completedParts?: Array<{ part_number: number; etag: string }>
  onProgress: (value: number) => void
  onStatus?: (message: string) => void
  onCheckpoint?: (checkpoint: UploadCheckpoint) => void
  shouldPreserveOnAbort?: () => boolean
}

class StorageResponseError extends Error {
  constructor(message: string, public retryable: boolean) { super(message) }
}

function abortError() {
  return new DOMException('Завантаження скасовано', 'AbortError')
}

function wait(milliseconds: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) return reject(abortError())
    const timeout = window.setTimeout(resolve, milliseconds)
    signal?.addEventListener('abort', () => {
      window.clearTimeout(timeout)
      reject(abortError())
    }, { once: true })
  })
}

function waitForOnline(options: UploadOptions) {
  if (navigator.onLine) return Promise.resolve()
  options.onStatus?.('Немає мережі — продовжимо автоматично')
  return new Promise<void>((resolve, reject) => {
    const online = () => { cleanup(); resolve() }
    const aborted = () => { cleanup(); reject(abortError()) }
    const cleanup = () => {
      window.removeEventListener('online', online)
      options.signal?.removeEventListener('abort', aborted)
    }
    window.addEventListener('online', online, { once: true })
    options.signal?.addEventListener('abort', aborted, { once: true })
  })
}

function isAbort(error: unknown, signal?: AbortSignal) {
  return signal?.aborted || (error instanceof DOMException && error.name === 'AbortError')
}

function isRetryable(error: unknown) {
  if (error instanceof StorageResponseError) return error.retryable
  if (error instanceof ApiError) return RETRYABLE_STATUS.has(error.status)
  return error instanceof TypeError
}

async function withRetry<T>(operation: () => Promise<T>, options: UploadOptions) {
  let lastError: unknown
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
    await waitForOnline(options)
    try {
      return await operation()
    } catch (error) {
      if (isAbort(error, options.signal)) throw error
      lastError = error
      if (!isRetryable(error) || attempt === MAX_ATTEMPTS) throw error
      const delay = Math.min(8000, 500 * 2 ** (attempt - 1)) + Math.round(Math.random() * 250)
      options.onStatus?.(`Зв’язок перервався. Спроба ${attempt + 1} із ${MAX_ATTEMPTS}`)
      await wait(delay, options.signal)
    }
  }
  throw lastError
}

async function put(url: string, body: Blob, headers: Record<string, string>, options: UploadOptions) {
  return withRetry(async () => {
    const response = await fetch(url, { method: 'PUT', body, headers, signal: options.signal })
    if (response.ok) return response
    throw new StorageResponseError(`Сховище відхилило файл (${response.status})`, RETRYABLE_STATUS.has(response.status))
  }, options)
}

async function uploadSingle(ticket: UploadTicket, file: File, options: UploadOptions) {
  if (!ticket.url) throw new Error('Сервер не повернув адресу завантаження')
  options.onProgress(15)
  await put(ticket.url, file, ticket.headers ?? {}, options)
  options.onProgress(90)
  await withRetry(() => media.complete(ticket.id, [], options.signal), options)
}

async function uploadMultipart(ticket: UploadTicket, file: File, options: UploadOptions) {
  if (!ticket.part_size_bytes || !ticket.parts_count) throw new Error('Неповні параметри часткового завантаження')
  const completed = new Map<number, string>()
  for (const part of options.completedParts ?? []) {
    if (part.part_number >= 1 && part.part_number <= ticket.parts_count) completed.set(part.part_number, part.etag)
  }
  options.onProgress(Math.round((completed.size / ticket.parts_count) * 90))
  const missing = Array.from({ length: ticket.parts_count }, (_, index) => index + 1).filter((number) => !completed.has(number))
  for (let offset = 0; offset < missing.length; offset += PART_URL_BATCH) {
    const numbers = missing.slice(offset, offset + PART_URL_BATCH)
    const { parts } = await withRetry(() => media.partURLs(ticket.id, numbers, options.signal), options)
    for (const part of parts) {
      const start = (part.part_number - 1) * ticket.part_size_bytes
      const chunk = file.slice(start, Math.min(start + ticket.part_size_bytes, file.size))
      const response = await put(part.url, chunk, {}, options)
      const etag = response.headers.get('ETag')
      if (!etag) throw new Error('Сховище не повернуло ETag для частини файлу')
      completed.set(part.part_number, etag)
      const completedParts = [...completed].sort(([left], [right]) => left - right).map(([part_number, storedETag]) => ({ part_number, etag: storedETag }))
      options.onCheckpoint?.({ uploadID: ticket.id, completedParts })
      options.onProgress(Math.round((completed.size / ticket.parts_count) * 90))
    }
  }
  const completedParts = [...completed].sort(([left], [right]) => left - right).map(([part_number, etag]) => ({ part_number, etag }))
  await withRetry(() => media.complete(ticket.id, completedParts, options.signal), options)
}

export async function uploadFile(slug: string, file: File, options: UploadOptions) {
  const { upload } = await withRetry(() => media.initiate(slug, file, options.signal, options.idempotencyKey, options.mimeType), options)
  options.onCheckpoint?.({ uploadID: upload.id, completedParts: options.completedParts ?? [] })
  try {
    if (upload.type === 'single') await uploadSingle(upload, file, options)
    else await uploadMultipart(upload, file, options)
    options.onProgress(100)
  } catch (error) {
    if (isAbort(error, options.signal) && !options.shouldPreserveOnAbort?.()) await media.abort(upload.id).catch(() => undefined)
    throw error
  }
}
