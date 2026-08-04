import { media } from './api'
import type { UploadTicket } from '../types'

const PART_URL_BATCH = 20
const MAX_ATTEMPTS = 3

interface UploadOptions {
  signal?: AbortSignal
  onProgress: (value: number) => void
  onStatus?: (message: string) => void
}

class StorageResponseError extends Error {
  constructor(message: string, public retryable: boolean) { super(message) }
}

function wait(milliseconds: number, signal?: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timeout = window.setTimeout(resolve, milliseconds)
    signal?.addEventListener('abort', () => {
      window.clearTimeout(timeout)
      reject(new DOMException('Завантаження скасовано', 'AbortError'))
    }, { once: true })
  })
}

async function put(url: string, body: Blob, headers: Record<string, string>, options: UploadOptions) {
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
    try {
      const response = await fetch(url, { method: 'PUT', body, headers, signal: options.signal })
      if (response.ok) return response
      const retryable = [408, 425, 429, 500, 502, 503, 504].includes(response.status)
      throw new StorageResponseError(`Сховище відхилило файл (${response.status})`, retryable)
    } catch (error) {
      if (options.signal?.aborted || (error instanceof DOMException && error.name === 'AbortError')) throw error
      if (error instanceof StorageResponseError && !error.retryable) throw error
      if (attempt === MAX_ATTEMPTS) throw error
    }
    options.onStatus?.(`Повторна спроба ${attempt + 1} із ${MAX_ATTEMPTS}`)
    await wait(500 * 2 ** (attempt - 1), options.signal)
  }
  throw new Error('Не вдалося передати файл у сховище')
}

async function uploadSingle(ticket: UploadTicket, file: File, options: UploadOptions) {
  if (!ticket.url) throw new Error('Сервер не повернув адресу завантаження')
  options.onProgress(15)
  await put(ticket.url, file, ticket.headers ?? {}, options)
  options.onProgress(90)
  await media.complete(ticket.id, [], options.signal)
}

async function uploadMultipart(ticket: UploadTicket, file: File, options: UploadOptions) {
  if (!ticket.part_size_bytes || !ticket.parts_count) throw new Error('Неповні параметри часткового завантаження')
  const completed: Array<{ part_number: number; etag: string }> = []
  for (let offset = 1; offset <= ticket.parts_count; offset += PART_URL_BATCH) {
    const numbers = Array.from(
      { length: Math.min(PART_URL_BATCH, ticket.parts_count - offset + 1) },
      (_, index) => offset + index,
    )
    const { parts } = await media.partURLs(ticket.id, numbers, options.signal)
    for (const part of parts) {
      const start = (part.part_number - 1) * ticket.part_size_bytes
      const chunk = file.slice(start, Math.min(start + ticket.part_size_bytes, file.size))
      const response = await put(part.url, chunk, {}, options)
      const etag = response.headers.get('ETag')
      if (!etag) throw new Error('Сховище не повернуло ETag для частини файлу')
      completed.push({ part_number: part.part_number, etag })
      options.onProgress(Math.round((completed.length / ticket.parts_count) * 90))
    }
  }
  await media.complete(ticket.id, completed, options.signal)
}

export async function uploadFile(slug: string, file: File, options: UploadOptions) {
  const { upload } = await media.initiate(slug, file, options.signal)
  try {
    if (upload.type === 'single') await uploadSingle(upload, file, options)
    else await uploadMultipart(upload, file, options)
    options.onProgress(100)
  } catch (error) {
    await media.abort(upload.id).catch(() => undefined)
    throw error
  }
}
