import { parse } from 'exifr'

function validDate(value: unknown): Date | null {
  const date = value instanceof Date ? value : typeof value === 'string' || typeof value === 'number' ? new Date(value) : null
  if (!date || Number.isNaN(date.getTime())) return null
  const earliest = new Date('1990-01-01T00:00:00Z').getTime()
  const latest = Date.now() + 24 * 60 * 60 * 1000
  return date.getTime() >= earliest && date.getTime() <= latest ? date : null
}

export async function mediaCapturedAt(file: File) {
  if (file.type.startsWith('image/')) {
    try {
      const metadata = await parse(file, { pick: ['DateTimeOriginal', 'CreateDate', 'ModifyDate'] }) as Record<string, unknown> | undefined
      const embedded = validDate(metadata?.DateTimeOriginal) ?? validDate(metadata?.CreateDate) ?? validDate(metadata?.ModifyDate)
      if (embedded) return embedded.toISOString()
    } catch {
      // Unsupported or malformed metadata must never block the original upload.
    }
  }
  return validDate(file.lastModified)?.toISOString() ?? null
}
