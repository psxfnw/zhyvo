export function checksumFile(file: File, signal?: AbortSignal, onProgress?: (value: number) => void) {
  return new Promise<string>((resolve, reject) => {
    const worker = new Worker(new URL('./checksum.worker.ts', import.meta.url), { type: 'module' })
    const cleanup = () => {
      signal?.removeEventListener('abort', abort)
      worker.terminate()
    }
    const abort = () => {
      cleanup()
      reject(new DOMException('Перевірку файла скасовано', 'AbortError'))
    }
    if (signal?.aborted) {
      abort()
      return
    }
    signal?.addEventListener('abort', abort, { once: true })
    worker.onerror = (event) => {
      cleanup()
      reject(new Error(event.message || 'Не вдалося перевірити файл'))
    }
    worker.onmessage = (event: MessageEvent<{ type: 'progress'; value: number } | { type: 'complete'; checksum: string } | { type: 'error'; message: string }>) => {
      if (event.data.type === 'progress') {
        onProgress?.(event.data.value)
        return
      }
      cleanup()
      if (event.data.type === 'complete') resolve(event.data.checksum)
      else reject(new Error(event.data.message))
    }
    worker.postMessage({ file })
  })
}
