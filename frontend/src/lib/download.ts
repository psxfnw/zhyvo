import { getTelegramWebApp } from './telegram'

export interface RemoteFile {
  url: string
  filename: string
  mimeType?: string
}

type ShareNavigator = Navigator & {
  canShare?: (data?: ShareData) => boolean
  share?: (data?: ShareData) => Promise<void>
}

export function isMobileDevice() {
  return window.matchMedia('(pointer: coarse)').matches || /Android|iPhone|iPad|iPod/i.test(navigator.userAgent)
}

export function canShareFiles(files: File[]) {
  const shareNavigator = navigator as ShareNavigator
  return typeof shareNavigator.canShare === 'function' && shareNavigator.canShare({ files })
}

export async function fetchShareFile(remote: RemoteFile) {
  const response = await fetch(remote.url)
  if (!response.ok) throw new Error('Не вдалося отримати оригінал файлу')
  const blob = await response.blob()
  return new File([blob], remote.filename, { type: remote.mimeType || blob.type || 'application/octet-stream' })
}

export async function sharePreparedFiles(files: File[], title: string) {
  const shareNavigator = navigator as ShareNavigator
  if (typeof shareNavigator.canShare !== 'function' || !shareNavigator.canShare({ files })) {
    throw new Error('Цей браузер не підтримує збереження кількох файлів')
  }
  await shareNavigator.share({ files, title })
}

export async function saveRemoteFile(remote: RemoteFile) {
  const telegram = getTelegramWebApp()
  if (telegram?.downloadFile) {
    telegram.downloadFile({ url: remote.url, file_name: remote.filename })
    return
  }
  if (telegram?.openLink) {
    telegram.openLink(remote.url)
    return
  }

  if (isMobileDevice()) {
    try {
      const file = await fetchShareFile(remote)
      if (canShareFiles([file])) {
        await sharePreparedFiles([file], remote.filename)
        return
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      // Continue with the browser download fallback.
    }
  }

  const anchor = document.createElement('a')
  anchor.href = remote.url
  anchor.download = remote.filename
  anchor.rel = 'noopener'
  anchor.style.display = 'none'
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
}
