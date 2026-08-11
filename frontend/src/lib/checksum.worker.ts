import { createSHA256 } from 'hash-wasm'

const CHUNK_SIZE = 8 * 1024 * 1024

self.onmessage = async (event: MessageEvent<{ file: File }>) => {
  try {
    const { file } = event.data
    const hasher = await createSHA256()
    for (let offset = 0; offset < file.size; offset += CHUNK_SIZE) {
      const chunk = new Uint8Array(await file.slice(offset, Math.min(offset + CHUNK_SIZE, file.size)).arrayBuffer())
      hasher.update(chunk)
      self.postMessage({ type: 'progress', value: Math.min(100, Math.round(((offset + chunk.byteLength) / file.size) * 100)) })
    }
    self.postMessage({ type: 'complete', checksum: hasher.digest('hex') })
  } catch (error) {
    self.postMessage({ type: 'error', message: error instanceof Error ? error.message : 'Не вдалося перевірити файл' })
  }
}
