import { CSSProperties, FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import {
  ArrowDownToLine,
  ArrowLeft,
  Check,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Copy,
  FileImage,
  ImagePlus,
  Images,
  LockKeyhole,
  LogIn,
  Menu,
  RefreshCw,
  Share2,
  ShieldCheck,
  Smartphone,
  Trash2,
  Upload,
  UserRound,
  Users,
  Video,
  X,
} from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { Link, Navigate, Route, Routes, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ApiError, ensureIdentity, getSession, media, rooms } from './lib/api'
import { bytes, errorMessage, normalizeSlug, remaining } from './lib/format'
import { getTelegramBootstrapError, getTelegramWebApp } from './lib/telegram'
import { uploadFile } from './lib/upload'
import type { AccessMode, GalleryItem, Room, RoomMember, RoomPreview, Session, UploadProgress } from './types'

function uuid() {
  return crypto.randomUUID()
}

function useSession() {
  const [session, setSession] = useState<Session | null>(getSession())
  useEffect(() => {
    const update = () => setSession(getSession())
    window.addEventListener('photodrop:session', update)
    return () => window.removeEventListener('photodrop:session', update)
  }, [])
  return session
}

function TelegramNavigation() {
  const location = useLocation()
  const navigate = useNavigate()
  useEffect(() => {
    const telegram = getTelegramWebApp()
    if (!telegram) return
    const onBack = () => {
      telegram.HapticFeedback?.impactOccurred('light')
      if (location.search) navigate(location.pathname, { replace: true })
      else navigate('/')
    }
    if (location.pathname === '/') telegram.BackButton.hide()
    else telegram.BackButton.show()
    telegram.onEvent('backButtonClicked', onBack)
    return () => telegram.offEvent('backButtonClicked', onBack)
  }, [location.pathname, location.search, navigate])
  return null
}

function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <Link className={`brand ${compact ? 'brand--compact' : ''}`} to="/" aria-label="Zhyvo — на головну">
      <img src="/brand-mark.svg" alt="" />
      <span>Zhyvo</span>
    </Link>
  )
}

function ProfileChip({ session }: { session: Session }) {
  return (
    <div className="profile-chip" title={`Поточний профіль: ${session.identity.display_name}`}>
      <UserRound size={17} />
      <span><small>Ви</small><strong>{session.identity.display_name}</strong></span>
    </div>
  )
}

function IdentityField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>Ваше ім’я</span>
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        autoComplete="name"
        maxLength={80}
        required
        placeholder="Як вас підписати"
      />
    </label>
  )
}

function AccessFields({ mode, onMode, secret, onSecret }: {
  mode: AccessMode
  onMode: (mode: AccessMode) => void
  secret: string
  onSecret: (value: string) => void
}) {
  return (
    <>
      <fieldset className="field">
        <legend>Доступ</legend>
        <div className="segmented segmented--three">
          {([
            ['public', 'Без пароля'],
            ['pin', 'PIN'],
            ['password', 'Пароль'],
          ] as const).map(([value, label]) => (
            <button type="button" className={mode === value ? 'is-active' : ''} onClick={() => onMode(value)} key={value}>
              {label}
            </button>
          ))}
        </div>
      </fieldset>
      {mode !== 'public' && (
        <label className="field">
          <span>{mode === 'pin' ? 'PIN-код' : 'Пароль'}</span>
          <input
            type={mode === 'pin' ? 'tel' : 'password'}
            inputMode={mode === 'pin' ? 'numeric' : undefined}
            pattern={mode === 'pin' ? '[0-9]{4,8}' : undefined}
            minLength={mode === 'password' ? 6 : 4}
            maxLength={mode === 'password' ? 72 : 8}
            value={secret}
            onChange={(event) => onSecret(mode === 'pin' ? event.target.value.replace(/\D/g, '') : event.target.value)}
            required
            placeholder={mode === 'pin' ? '4–8 цифр' : 'Щонайменше 6 символів'}
          />
        </label>
      )}
    </>
  )
}

function HomePage() {
  const navigate = useNavigate()
  const session = useSession()
  const [tab, setTab] = useState<'create' | 'join'>('create')
  const [displayName, setDisplayName] = useState('')
  const [roomName, setRoomName] = useState('')
  const [lifetime, setLifetime] = useState(2)
  const [accessMode, setAccessMode] = useState<AccessMode>('pin')
  const [secret, setSecret] = useState('')
  const [joinCode, setJoinCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [activeRooms, setActiveRooms] = useState<Room[]>([])
  const [roomsLoading, setRoomsLoading] = useState(Boolean(session))

  useEffect(() => {
    let active = true
    if (!session) {
      setActiveRooms([])
      setRoomsLoading(false)
      return () => { active = false }
    }
    setRoomsLoading(true)
    rooms.list()
      .then((result) => { if (active) setActiveRooms(result.rooms) })
      .catch(() => { if (active) setActiveRooms([]) })
      .finally(() => { if (active) setRoomsLoading(false) })
    return () => { active = false }
  }, [session])

  async function createRoom(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await ensureIdentity(displayName)
      const { room } = await rooms.create({
        name: roomName,
        lifetime_days: lifetime,
        access: { mode: accessMode, ...(accessMode === 'public' ? {} : { secret }) },
      })
      navigate(`/r/${room.slug}`)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  async function joinRoom(event: FormEvent) {
    event.preventDefault()
    const slug = normalizeSlug(joinCode)
    if (slug.length < 6) {
      setError('Введіть код кімнати або повне посилання')
      return
    }
    setBusy(true)
    setError('')
    try {
      await ensureIdentity(displayName)
      navigate(`/r/${slug}`)
    } catch (cause) {
      setError(errorMessage(cause))
      setBusy(false)
    }
  }

  return (
    <main className="home-shell">
      <header className="home-header">
        <Brand />
        {session && <ProfileChip session={session} />}
      </header>

      {(roomsLoading || activeRooms.length > 0) && (
        <section className="my-rooms" aria-labelledby="my-rooms-title">
          <header>
            <div><h2 id="my-rooms-title">Мої кімнати</h2><p>Доступні на цьому пристрої до автоматичного видалення.</p></div>
            {activeRooms.length > 0 && <span>{activeRooms.length}</span>}
          </header>
          {roomsLoading ? <div className="rooms-loading" aria-label="Завантажуємо кімнати"><span /><span /></div> : (
            <div className="room-list">
              {activeRooms.map((item) => {
                const time = remaining(item.expires_at)
                return (
                  <Link className="room-list-item" to={`/r/${item.slug}`} key={item.id}>
                    <div><strong>{item.name}</strong><span>{item.role === 'owner' ? 'Ви власник' : 'Ви учасник'} · {item.used_files} файлів</span></div>
                    <div className="room-list-item__time"><span>{time.value} {time.unit}</span><ChevronRight size={19} /></div>
                  </Link>
                )
              })}
            </div>
          )}
        </section>
      )}

      <section className="hero-grid">
        <div className="hero-copy">
          <p className="eyebrow">Спільна галерея події</p>
          <h1>Усі моменти.<br />Без стиснення.</h1>
          <p className="hero-summary">Створіть приватний простір, запросіть друзів і зберіть оригінальні фото та відео з усіх телефонів.</p>
          <div className="hero-benefits">
            <span><Images size={18} /> Оригінальна якість</span>
            <span><Clock3 size={18} /> Автовидалення</span>
            <span><Smartphone size={18} /> iOS та Android</span>
          </div>
        </div>

        <section className="action-panel" aria-labelledby="action-heading">
          <div className="panel-tabs" role="tablist">
            <button role="tab" aria-selected={tab === 'create'} onClick={() => { setTab('create'); setError('') }}>Створити</button>
            <button role="tab" aria-selected={tab === 'join'} onClick={() => { setTab('join'); setError('') }}>Приєднатися</button>
          </div>

          {tab === 'create' ? (
            <form onSubmit={createRoom}>
              <h2 id="action-heading">Нова кімната</h2>
              {!session && <IdentityField value={displayName} onChange={setDisplayName} />}
              <label className="field">
                <span>Назва події</span>
                <input value={roomName} onChange={(event) => setRoomName(event.target.value)} maxLength={120} required placeholder="Наприклад, день народження" />
              </label>
              <fieldset className="field lifetime-field">
                <legend>Коли видалити</legend>
                <div className="lifetime-value"><strong>{lifetime}</strong><span>{lifetime === 1 ? 'день' : 'дні'}</span></div>
                <input
                  className="lifetime-slider"
                  type="range"
                  min="1"
                  max="3"
                  step="1"
                  value={lifetime}
                  onChange={(event) => setLifetime(Number(event.target.value))}
                  aria-label="Термін зберігання кімнати у днях"
                  style={{ '--slider-progress': `${(lifetime - 1) * 50}%` } as CSSProperties}
                />
                <div className="slider-labels" aria-hidden="true"><span>24 год</span><span>48 год</span><span>72 год</span></div>
              </fieldset>
              <AccessFields mode={accessMode} onMode={setAccessMode} secret={secret} onSecret={setSecret} />
              {error && <p className="form-error" role="alert">{error}</p>}
              <button className="primary-button" disabled={busy} type="submit">
                {busy ? 'Створюємо…' : 'Створити кімнату'}
              </button>
            </form>
          ) : (
            <form onSubmit={joinRoom}>
              <h2 id="action-heading">Знайти кімнату</h2>
              {!session && <IdentityField value={displayName} onChange={setDisplayName} />}
              <label className="field">
                <span>Код або посилання</span>
                <input value={joinCode} onChange={(event) => setJoinCode(event.target.value)} autoCapitalize="characters" required placeholder="ABC123" />
              </label>
              <p className="field-note">PIN або пароль попросимо на наступному кроці.</p>
              {error && <p className="form-error" role="alert">{error}</p>}
              <button className="primary-button" disabled={busy} type="submit">
                <LogIn size={19} /> {busy ? 'Зачекайте…' : 'Продовжити'}
              </button>
            </form>
          )}
        </section>
      </section>

      <footer className="home-footer">
        <span>Без реєстрації</span><span>До 2 ГБ на відео</span><span>Автовидалення через 1–3 дні</span>
      </footer>
    </main>
  )
}

function JoinRoom({ preview, onJoined }: { preview: RoomPreview; onJoined: (room: Room) => void }) {
  const session = useSession()
  const [displayName, setDisplayName] = useState('')
  const [secret, setSecret] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await ensureIdentity(displayName)
      const { room } = await rooms.join(preview.slug, secret)
      onJoined(room)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="join-shell">
      <header className="room-topbar"><Brand compact /><Link className="text-link" to="/"><ArrowLeft size={17} /> Головна</Link></header>
      <section className="join-card">
        <p className="eyebrow">Кімната {preview.slug}</p>
        <h1>{preview.name}</h1>
        <p>Медіа зберігаються до {new Date(preview.expires_at).toLocaleString('uk-UA', { dateStyle: 'long', timeStyle: 'short' })}.</p>
        <form onSubmit={submit}>
          {!session && <IdentityField value={displayName} onChange={setDisplayName} />}
          {preview.access_mode !== 'public' && (
            <label className="field">
              <span>{preview.access_mode === 'pin' ? 'PIN-код' : 'Пароль'}</span>
              <input
                type={preview.access_mode === 'pin' ? 'tel' : 'password'}
                inputMode={preview.access_mode === 'pin' ? 'numeric' : undefined}
                value={secret}
                onChange={(event) => setSecret(event.target.value)}
                required
                autoFocus={Boolean(session)}
              />
            </label>
          )}
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="primary-button" disabled={busy} type="submit">
            {preview.access_mode === 'public' ? <LogIn size={19} /> : <LockKeyhole size={19} />}
            {busy ? 'Входимо…' : 'Увійти до кімнати'}
          </button>
        </form>
      </section>
    </main>
  )
}

function UploadQueue({ uploads, onCancel, onRetry }: {
  uploads: UploadProgress[]
  onCancel: (id: string) => void
  onRetry: (id: string) => void
}) {
  if (!uploads.length) return null
  return (
    <aside className="upload-queue" aria-live="polite">
      <div className="upload-queue__title"><Upload size={17} /> Завантаження</div>
      {uploads.map((item) => (
        <div className="upload-row" key={item.id}>
          <div>
            <div><strong>{item.filename}</strong><span>{item.state === 'done' ? 'Готово' : item.state === 'error' ? item.message : item.message ?? `${item.progress}%`}</span></div>
            <div className="upload-actions">
              {item.state === 'uploading' && <button type="button" onClick={() => onCancel(item.id)}>Скасувати</button>}
              {item.state === 'error' && item.canRetry && <button type="button" onClick={() => onRetry(item.id)}><RefreshCw size={15} /> Повторити</button>}
            </div>
          </div>
          <div className={`progress ${item.state}`}><span style={{ width: `${item.progress}%` }} /></div>
        </div>
      ))}
    </aside>
  )
}

function GalleryCard({ item, onDelete, onOpen, onError }: {
  item: GalleryItem
  onDelete: (item: GalleryItem) => void
  onOpen: (item: GalleryItem) => void
  onError: (message: string) => void
}) {
  const [busy, setBusy] = useState(false)
  async function download() {
    setBusy(true)
    try {
      const result = await media.download(item.id)
      const anchor = document.createElement('a')
      anchor.href = result.url
      anchor.download = result.filename
      anchor.rel = 'noopener'
      anchor.click()
    } catch (cause) {
      onError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }
  return (
    <article className="media-card">
      <button className="media-preview" type="button" onClick={() => onOpen(item)} aria-label={`Відкрити ${item.original_filename}`}>
        {item.thumbnail_url ? <img src={item.thumbnail_url} alt={item.original_filename} loading="lazy" /> : (
          <div className="media-placeholder">{item.media_type === 'video' ? <Video /> : <FileImage />}<span>Готуємо прев’ю</span></div>
        )}
        {item.media_type === 'video' && <span className="video-badge"><Video size={14} /> Відео</span>}
      </button>
      <div className="media-meta">
        <div><strong title={item.original_filename}>{item.original_filename}</strong><span>{item.uploaded_by.display_name} · {bytes(item.size_bytes)}</span></div>
        <div className="media-actions">
          <button onClick={download} disabled={busy} aria-label={`Завантажити ${item.original_filename}`} title="Завантажити"><ArrowDownToLine size={18} /></button>
          {item.permissions.can_delete && <button onClick={() => onDelete(item)} aria-label={`Видалити ${item.original_filename}`} title="Видалити"><Trash2 size={18} /></button>}
        </div>
      </div>
    </article>
  )
}

function ShareDialog({ room, url, onClose, onCopied }: {
  room: Room
  url: string
  onClose: () => void
  onCopied: () => void
}) {
  const closeRef = useRef<HTMLButtonElement>(null)
  const shareNavigator = navigator as Navigator & { share?: (data?: ShareData) => Promise<void> }
  const supportsNativeShare = typeof shareNavigator.share === 'function'
  useEffect(() => {
    const previousFocus = document.activeElement as HTMLElement | null
    closeRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKeyDown)
    return () => { document.removeEventListener('keydown', onKeyDown); previousFocus?.focus() }
  }, [onClose])

  async function nativeShare() {
    if (!shareNavigator.share) return
    try { await shareNavigator.share({ title: room.name, text: `Приєднуйтеся до кімнати «${room.name}» у Zhyvo`, url }) } catch { /* User cancelled the share sheet. */ }
  }

  return (
    <div className="dialog-backdrop" role="presentation" onMouseDown={onClose}>
      <section className="share-dialog" role="dialog" aria-modal="true" aria-labelledby="share-title" onMouseDown={(event) => event.stopPropagation()}>
        <header><div><p className="eyebrow">Кімната {room.slug}</p><h2 id="share-title">Запросити друзів</h2></div><button ref={closeRef} className="icon-button" onClick={onClose} aria-label="Закрити"><X /></button></header>
        <div className="qr-wrap"><QRCodeSVG value={url} size={224} level="M" title={`QR-код кімнати ${room.slug}`} /></div>
        <p className="share-note">Відскануйте QR-код або надішліть посилання. Для захищеної кімнати PIN чи пароль передайте окремо.</p>
        <div className="share-actions">
          <button className="secondary-button" onClick={onCopied}><Copy size={18} /> Копіювати посилання</button>
          {supportsNativeShare && <button className="primary-button" onClick={nativeShare}><Share2 size={18} /> Поділитися</button>}
        </div>
      </section>
    </div>
  )
}

function MediaViewer({ item, items, onSelect, onClose, onError }: {
  item: GalleryItem
  items: GalleryItem[]
  onSelect: (item: GalleryItem) => void
  onClose: () => void
  onError: (message: string) => void
}) {
  const closeRef = useRef<HTMLButtonElement>(null)
  const [source, setSource] = useState<{ url: string; filename: string } | null>(null)
  const [loading, setLoading] = useState(true)
  const index = items.findIndex((candidate) => candidate.id === item.id)
  const previous = index > 0 ? items[index - 1] : null
  const next = index >= 0 && index < items.length - 1 ? items[index + 1] : null

  useEffect(() => {
    let active = true
    setLoading(true)
    setSource(null)
    media.download(item.id).then((result) => { if (active) setSource(result) }).catch((cause) => {
      if (active) onError(errorMessage(cause))
    }).finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [item.id, onError])

  useEffect(() => {
    const previousFocus = document.activeElement as HTMLElement | null
    closeRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
      if (event.key === 'ArrowLeft' && previous) onSelect(previous)
      if (event.key === 'ArrowRight' && next) onSelect(next)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => { document.removeEventListener('keydown', onKeyDown); previousFocus?.focus() }
  }, [next, onClose, onSelect, previous])

  return (
    <div className="viewer" role="dialog" aria-modal="true" aria-labelledby="viewer-title">
      <header className="viewer-topbar">
        <div><strong id="viewer-title">{item.original_filename}</strong><span>{item.uploaded_by.display_name} · {bytes(item.size_bytes)}</span></div>
        <div className="viewer-actions">
          {source && <a className="icon-button" href={source.url} download={source.filename} aria-label="Завантажити оригінал"><ArrowDownToLine /></a>}
          <button ref={closeRef} className="icon-button" onClick={onClose} aria-label="Закрити перегляд"><X /></button>
        </div>
      </header>
      <div className="viewer-stage">
        {loading && <div className="viewer-loading" role="status">Завантажуємо оригінал…</div>}
        {!loading && source && (item.media_type === 'video'
          ? <video key={source.url} src={source.url} controls playsInline autoPlay aria-label={item.original_filename} />
          : <img src={source.url} alt={item.original_filename} />)}
        {previous && <button className="viewer-nav viewer-nav--previous" onClick={() => onSelect(previous)} aria-label="Попередній файл"><ChevronLeft /></button>}
        {next && <button className="viewer-nav viewer-nav--next" onClick={() => onSelect(next)} aria-label="Наступний файл"><ChevronRight /></button>}
      </div>
      <footer className="viewer-footer"><span>{index + 1} / {items.length}</span><span>{new Date(item.created_at).toLocaleString('uk-UA', { dateStyle: 'medium', timeStyle: 'short' })}</span></footer>
    </div>
  )
}

function RoomPage() {
  const { slug: routeSlug = '' } = useParams()
  const slug = normalizeSlug(routeSlug)
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const inputRef = useRef<HTMLInputElement>(null)
  const uploadFilesRef = useRef(new Map<string, File>())
  const uploadControllersRef = useRef(new Map<string, AbortController>())
  const galleryRefreshRef = useRef(false)
  const loadedPastFirstPageRef = useRef(false)
  const settingsCloseRef = useRef<HTMLButtonElement>(null)
  const membersCloseRef = useRef<HTMLButtonElement>(null)
  const [room, setRoom] = useState<Room | null>(null)
  const [preview, setPreview] = useState<RoomPreview | null>(null)
  const [gallery, setGallery] = useState<GalleryItem[]>([])
  const [cursor, setCursor] = useState<string | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [uploads, setUploads] = useState<UploadProgress[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const [settings, setSettings] = useState(false)
  const [shareDialog, setShareDialog] = useState(false)
  const [membersOpen, setMembersOpen] = useState(false)
  const [members, setMembers] = useState<RoomMember[]>([])
  const [membersLoading, setMembersLoading] = useState(false)
  const [membersError, setMembersError] = useState('')
  const selectedMediaID = searchParams.get('media')

  const loadGallery = useCallback(async (append = false, nextCursor?: string | null) => {
    const page = await media.gallery(slug, append ? nextCursor : null)
    setGallery((current) => append ? [...current, ...page.items] : page.items)
    loadedPastFirstPageRef.current = append
    setCursor(page.next_cursor)
    setHasMore(page.has_more)
  }, [slug])

  const refreshGallery = useCallback(async () => {
    if (galleryRefreshRef.current) return
    galleryRefreshRef.current = true
    try {
      const page = await media.gallery(slug)
      setGallery((current) => {
        const freshIDs = new Set(page.items.map((item) => item.id))
        if (current.length <= 50) return page.items
        return [...page.items, ...current.slice(50).filter((item) => !freshIDs.has(item.id))]
      })
      if (!loadedPastFirstPageRef.current) {
        setCursor(page.next_cursor)
        setHasMore(page.has_more)
      }
    } finally {
      galleryRefreshRef.current = false
    }
  }, [slug])

  useEffect(() => {
    let active = true
    async function load() {
      setLoading(true)
      setError('')
      try {
        if (getSession()) {
          try {
            const result = await rooms.get(slug)
            if (!active) return
            setRoom(result.room)
            await loadGallery()
            return
          } catch (cause) {
            if (!(cause instanceof ApiError) || ![401, 403].includes(cause.status)) throw cause
          }
        }
        const result = await rooms.preview(slug)
        if (active) setPreview(result)
      } catch (cause) {
        if (active) setError(errorMessage(cause))
      } finally {
        if (active) setLoading(false)
      }
    }
    void load()
    return () => { active = false }
  }, [slug, loadGallery])

  useEffect(() => {
    if (!room) return
    const interval = window.setInterval(() => {
      if (document.visibilityState === 'visible') void refreshGallery().catch(() => undefined)
    }, 8000)
    return () => window.clearInterval(interval)
  }, [refreshGallery, room])

  useEffect(() => {
    if (!settings) return
    const previousFocus = document.activeElement as HTMLElement | null
    settingsCloseRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape') setSettings(false) }
    document.addEventListener('keydown', onKeyDown)
    return () => { document.removeEventListener('keydown', onKeyDown); previousFocus?.focus() }
  }, [settings])

  useEffect(() => {
    if (!membersOpen) return
    const previousFocus = document.activeElement as HTMLElement | null
    membersCloseRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape') setMembersOpen(false) }
    document.addEventListener('keydown', onKeyDown)
    return () => { document.removeEventListener('keydown', onKeyDown); previousFocus?.focus() }
  }, [membersOpen])

  useEffect(() => {
    if (!settings && !shareDialog && !membersOpen && !selectedMediaID) return
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => { document.body.style.overflow = previousOverflow }
  }, [membersOpen, selectedMediaID, settings, shareDialog])

  const processUpload = useCallback(async (queueID: string, file: File) => {
    const controller = uploadControllersRef.current.get(queueID) ?? new AbortController()
    uploadControllersRef.current.set(queueID, controller)
    if (controller.signal.aborted) return
    setUploads((current) => current.map((item) => item.id === queueID ? { ...item, state: 'uploading', message: undefined, canRetry: false } : item))
    try {
      await uploadFile(slug, file, {
        signal: controller.signal,
        onProgress: (progress) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, progress, message: undefined } : item)),
        onStatus: (message) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, message } : item)),
      })
      setUploads((current) => current.map((item) => item.id === queueID ? { ...item, state: 'done', progress: 100, message: undefined } : item))
      uploadFilesRef.current.delete(queueID)
      uploadControllersRef.current.delete(queueID)
    } catch (cause) {
      const cancelled = controller.signal.aborted
      setUploads((current) => current.map((item) => item.id === queueID ? {
        ...item,
        state: 'error',
        message: cancelled ? 'Скасовано' : errorMessage(cause),
        canRetry: true,
      } : item))
      uploadControllersRef.current.delete(queueID)
    }
  }, [slug])

  const openMedia = useCallback((item: GalleryItem) => setSearchParams({ media: item.id }), [setSearchParams])
  const closeMedia = useCallback(() => setSearchParams({}, { replace: true }), [setSearchParams])

  async function acceptFiles(files: FileList | null) {
    if (!files?.length || !room) return
    const accepted = Array.from(files)
    const queue = accepted.map((file) => ({ id: uuid(), filename: file.name, progress: 0, state: 'queued' as const }))
    queue.forEach((item, index) => {
      uploadFilesRef.current.set(item.id, accepted[index])
      uploadControllersRef.current.set(item.id, new AbortController())
    })
    setUploads((current) => [...current.filter((item) => item.state !== 'done'), ...queue])
    let nextIndex = 0
    const worker = async () => {
      while (nextIndex < queue.length) {
        const index = nextIndex++
        await processUpload(queue[index].id, accepted[index])
      }
    }
    await Promise.all(Array.from({ length: Math.min(2, queue.length) }, worker))
    if (inputRef.current) inputRef.current.value = ''
    await refreshGallery().catch(() => undefined)
    const refreshedRoom = await rooms.get(slug).catch(() => null)
    if (refreshedRoom) setRoom(refreshedRoom.room)
    window.setTimeout(() => setUploads((current) => current.filter((item) => item.state !== 'done')), 4000)
  }

  function cancelUpload(id: string) {
    uploadControllersRef.current.get(id)?.abort()
    setUploads((current) => current.map((item) => item.id === id ? { ...item, state: 'error', message: 'Скасовано', canRetry: true } : item))
  }

  function retryUpload(id: string) {
    const file = uploadFilesRef.current.get(id)
    if (!file) return
    uploadControllersRef.current.set(id, new AbortController())
    setUploads((current) => current.map((item) => item.id === id ? { ...item, progress: 0, state: 'queued', message: undefined, canRetry: false } : item))
    void processUpload(id, file).then(async () => {
      await refreshGallery().catch(() => undefined)
      const refreshedRoom = await rooms.get(slug).catch(() => null)
      if (refreshedRoom) setRoom(refreshedRoom.room)
    })
  }

  async function copyLink() {
    const url = `${window.location.origin}/r/${slug}`
    await navigator.clipboard.writeText(url)
    setCopied(true)
    setShareDialog(false)
    window.setTimeout(() => setCopied(false), 1800)
  }

  async function toggleUploads() {
    if (!room) return
    try {
      const result = await rooms.update(slug, { accepting_uploads: !room.accepting_uploads })
      setRoom(result.room)
    } catch (cause) { setError(errorMessage(cause)) }
  }

  async function openMembers() {
    setSettings(false)
    setMembersOpen(true)
    setMembersLoading(true)
    setMembersError('')
    try {
      const result = await rooms.members(slug)
      setMembers(result.members)
    } catch (cause) {
      setMembersError(errorMessage(cause))
    } finally {
      setMembersLoading(false)
    }
  }

  async function deleteItem(item: GalleryItem) {
    if (!window.confirm(`Видалити «${item.original_filename}» назавжди?`)) return
    try {
      await media.remove(item.id)
      setGallery((current) => current.filter((candidate) => candidate.id !== item.id))
    } catch (cause) { setError(errorMessage(cause)) }
  }

  async function deleteRoom() {
    if (!room || !window.confirm('Видалити кімнату та всі її файли назавжди?')) return
    try {
      await rooms.remove(slug)
      navigate('/')
    } catch (cause) { setError(errorMessage(cause)) }
  }

  if (loading) return <main className="status-page"><Brand /><div className="loading-line" /><p>Відкриваємо кімнату…</p></main>
  if (error && !room && !preview) return <main className="status-page"><Brand /><h1>Не вдалося відкрити кімнату</h1><p>{error}</p><Link className="primary-button" to="/">На головну</Link></main>
  if (preview && !room) return <JoinRoom preview={preview} onJoined={(joined) => { setPreview(null); setRoom(joined); void loadGallery() }} />
  if (!room) return null

  const ttl = remaining(room.expires_at)
  const usedPercent = Math.min(100, (room.used_storage_bytes / room.max_storage_bytes) * 100)
  const selectedMedia = gallery.find((item) => item.id === selectedMediaID) ?? null
  const shareURL = `${window.location.origin}/r/${slug}`

  return (
    <main className="room-shell">
      <header className="room-topbar">
        <Brand compact />
        <div className="room-topbar__actions">
          <button className="share-button" onClick={() => setShareDialog(true)}>{copied ? <Check size={17} /> : <Share2 size={17} />}{copied ? 'Скопійовано' : 'Запросити'}</button>
          {room.role === 'owner' && <button className="icon-button" onClick={() => setSettings(true)} aria-label="Налаштування кімнати"><Menu /></button>}
        </div>
      </header>

      <section className="room-heading">
        <div>
          <p className="eyebrow">Кімната {room.slug}</p>
          <h1>{room.name}</h1>
          <div className="room-facts"><span><ShieldCheck size={16} /> {room.access_mode === 'public' ? 'Без пароля' : room.access_mode === 'pin' ? 'Захищено PIN' : 'Захищено паролем'}</span><span>{room.used_files} із {room.max_files} файлів</span></div>
        </div>
        <div className="ttl-display"><strong>{ttl.value}</strong><div><span>{ttl.unit}</span><small>{ttl.detail}</small></div></div>
      </section>

      {error && <div className="page-error" role="alert"><span>{error}</span><button onClick={() => setError('')} aria-label="Закрити"><X size={17} /></button></div>}

      <section className="gallery-toolbar">
        <div><h2>Галерея</h2><p>{bytes(room.used_storage_bytes)} використано</p></div>
        {room.accepting_uploads ? (
          <>
            <input ref={inputRef} type="file" accept="image/jpeg,image/png,image/webp,image/heic,image/heif,image/avif,image/gif,video/mp4,video/quicktime,video/webm,video/x-m4v,video/3gpp" multiple hidden onChange={(event) => void acceptFiles(event.target.files)} />
            <button className="primary-button primary-button--fit upload-button" onClick={() => inputRef.current?.click()} aria-label="Додати фото або відео"><ImagePlus size={21} /><span>Додати медіа</span></button>
          </>
        ) : <span className="uploads-closed"><LockKeyhole size={16} /> Завантаження закриті</span>}
      </section>

      <div className="storage-line"><span style={{ width: `${usedPercent}%` }} /></div>

      {gallery.length ? (
        <>
          <section className="gallery-grid">
            {gallery.map((item) => <GalleryCard key={item.id} item={item} onDelete={deleteItem} onOpen={openMedia} onError={setError} />)}
          </section>
          {hasMore && <button className="secondary-button load-more" onClick={() => void loadGallery(true, cursor)}>Показати більше</button>}
        </>
      ) : (
        <section className="empty-gallery">
          <ImagePlus size={36} strokeWidth={1.5} />
          <h2>Тут ще немає медіа</h2>
          <p>{room.accepting_uploads ? 'Додайте перші фото або відео з події.' : 'Власник кімнати закрив завантаження.'}</p>
          {room.accepting_uploads && <button className="text-link" onClick={() => inputRef.current?.click()}>Вибрати з галереї</button>}
        </section>
      )}

      <UploadQueue uploads={uploads} onCancel={cancelUpload} onRetry={retryUpload} />

      {shareDialog && <ShareDialog room={room} url={shareURL} onClose={() => setShareDialog(false)} onCopied={() => void copyLink()} />}

      {membersOpen && (
        <div className="dialog-backdrop" role="presentation" onMouseDown={() => setMembersOpen(false)}>
          <section className="members-dialog" role="dialog" aria-modal="true" aria-labelledby="members-title" onMouseDown={(event) => event.stopPropagation()}>
            <header><div><h2 id="members-title">Учасники</h2><p>{membersLoading ? 'Оновлюємо список…' : `${members.length} ${members.length === 1 ? 'учасник' : 'учасники'}`}</p></div><button ref={membersCloseRef} className="icon-button" onClick={() => setMembersOpen(false)} aria-label="Закрити"><X /></button></header>
            {membersError ? <p className="members-error" role="alert">{membersError}</p> : membersLoading ? <div className="members-loading"><span /><span /><span /></div> : (
              <div className="members-list">
                {members.map((member) => (
                  <div className="member-row" key={member.id}>
                    <span className="member-avatar" aria-hidden="true">{member.display_name.trim().slice(0, 1).toLocaleUpperCase('uk-UA')}</span>
                    <div><strong>{member.display_name}</strong><span>Приєднався {new Date(member.joined_at).toLocaleString('uk-UA', { dateStyle: 'medium', timeStyle: 'short' })}</span></div>
                    <small>{member.role === 'owner' ? 'Власник' : 'Учасник'}</small>
                  </div>
                ))}
              </div>
            )}
          </section>
        </div>
      )}

      {selectedMedia && <MediaViewer item={selectedMedia} items={gallery} onSelect={openMedia} onClose={closeMedia} onError={setError} />}

      {settings && (
        <div className="dialog-backdrop" role="presentation" onMouseDown={() => setSettings(false)}>
          <section className="settings-dialog" role="dialog" aria-modal="true" aria-labelledby="settings-title" onMouseDown={(event) => event.stopPropagation()}>
            <header><h2 id="settings-title">Налаштування</h2><button ref={settingsCloseRef} className="icon-button" onClick={() => setSettings(false)} aria-label="Закрити"><X /></button></header>
            <div className="setting-row"><div><strong>Учасники кімнати</strong><span>Перегляньте всіх, хто приєднався за посиланням або QR-кодом.</span></div><button className="secondary-button primary-button--fit" onClick={() => void openMembers()}><Users size={17} /> Переглянути</button></div>
            <div className="setting-row"><div><strong>Приймати нові файли</strong><span>Учасники бачитимуть галерею, але не зможуть завантажувати медіа.</span></div><button className={`switch ${room.accepting_uploads ? 'on' : ''}`} role="switch" aria-checked={room.accepting_uploads} onClick={toggleUploads}><span /></button></div>
            <div className="setting-row setting-row--danger"><div><strong>Видалити кімнату</strong><span>Усі оригінали та дані буде видалено без можливості відновлення.</span></div><button className="danger-button" onClick={deleteRoom}><Trash2 size={17} /> Видалити</button></div>
          </section>
        </div>
      )}
    </main>
  )
}

export default function App() {
  const telegramError = getTelegramBootstrapError()
  return (
    <>
      <TelegramNavigation />
      {telegramError && <div className="telegram-error" role="alert">{telegramError}</div>}
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/r/:slug" element={<RoomPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </>
  )
}
