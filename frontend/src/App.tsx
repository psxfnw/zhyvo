import { CSSProperties, FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import {
  ArrowDownToLine,
  Archive,
  ArrowLeft,
  Ban,
  Check,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Copy,
  Crown,
  FileImage,
  ImagePlus,
  Images,
  LockKeyhole,
  LogIn,
  Menu,
  RefreshCw,
  RotateCcw,
  Send,
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
import { ApiError, archives, ensureIdentity, getSession, media, rooms } from './lib/api'
import { bytes, errorMessage, normalizeSlug, remaining } from './lib/format'
import { canShareFiles, fetchShareFile, isMobileDevice, saveRemoteFile, sharePreparedFiles } from './lib/download'
import { getTelegramBootstrapError, getTelegramWebApp, openTelegramInvite, roomInviteLink, telegramRoomLink } from './lib/telegram'
import { uploadFile } from './lib/upload'
import { loadUploadQueue, saveUploadQueue } from './lib/uploadQueue'
import { completeTelegramLogin, startTelegramLogin } from './lib/telegramLogin'
import type { AccessMode, BlockedRoomMember, GalleryItem, Room, RoomActivityEvent, RoomArchive, RoomMember, RoomPreview, Session, UploadProgress } from './types'

function uuid() {
  return crypto.randomUUID()
}

function activityLabel(event: RoomActivityEvent) {
  switch (event.type) {
    case 'room_created': return `${event.actor_display_name} створив(ла) кімнату`
    case 'member_joined': return `${event.actor_display_name} приєднався(-лася) до кімнати`
    case 'member_removed': return `${event.actor_display_name} видалив(ла) ${event.subject_display_name ?? 'учасника'}`
    case 'member_unblocked': return `${event.actor_display_name} розблокував(ла) ${event.subject_display_name ?? 'учасника'}`
    case 'ownership_transferred': return `${event.actor_display_name} передав(ла) права власника ${event.subject_display_name ?? 'учаснику'}`
    case 'room_updated': return `${event.actor_display_name} змінив(ла) налаштування кімнати`
  }
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

function BrowserSessionNotice({ session }: { session: Session }) {
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(`photodrop.login-notice.${session.identity.id}`) === 'hidden')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  if (dismissed || session.identity.kind !== 'anonymous' || getTelegramWebApp()) return null

  async function login() {
    setBusy(true)
    setError('')
    try {
      await startTelegramLogin(window.location.pathname + window.location.search)
    } catch (cause) {
      setError(errorMessage(cause))
      setBusy(false)
    }
  }

  function dismiss() {
    localStorage.setItem(`photodrop.login-notice.${session.identity.id}`, 'hidden')
    setDismissed(true)
  }

  return (
    <aside className="browser-session-notice">
      <div className="browser-session-notice__icon"><Send size={22} /></div>
      <div><strong>Збережіть доступ до кімнат</strong><p>Зараз профіль зберігається лише в цьому браузері. Підключіть Telegram, щоб відкрити свої кімнати з іншого телефона або комп’ютера.</p>{error && <span role="alert">{error}</span>}</div>
      <button className="telegram-login-button" onClick={() => void login()} disabled={busy}><Send size={17} /> {busy ? 'Відкриваємо…' : 'Увійти через Telegram'}</button>
      <button className="notice-close" onClick={dismiss} aria-label="Нагадати пізніше"><X size={17} /></button>
    </aside>
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

      {session && <BrowserSessionNotice session={session} />}

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

function TelegramLoginCallback() {
  const navigate = useNavigate()
  const [error, setError] = useState('')
  useEffect(() => {
    let active = true
    completeTelegramLogin(window.location.search).then(({ returnTo }) => {
      if (active) navigate(returnTo, { replace: true })
    }).catch((cause) => {
      if (active) setError(errorMessage(cause))
    })
    return () => { active = false }
  }, [navigate])
  return <main className="status-page"><Brand />{error ? <><h1>Не вдалося увійти</h1><p>{error}</p><Link className="primary-button" to="/">На головну</Link></> : <><div className="loading-line" /><p>Підключаємо Telegram і переносимо ваші кімнати…</p></>}</main>
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
              {(item.state === 'error' || item.state === 'waiting_file') && item.canRetry && <button type="button" onClick={() => onRetry(item.id)}><RefreshCw size={15} /> {item.state === 'waiting_file' ? 'Вибрати файл' : 'Повторити'}</button>}
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
      await saveRemoteFile({ ...result, mimeType: item.mime_type })
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

const MOBILE_BATCH_FILES = 10
const MOBILE_BATCH_BYTES = 128 * 1024 * 1024

function MobileSaveDialog({ slug, roomName, onClose, onError }: {
  slug: string
  roomName: string
  onClose: () => void
  onError: (message: string) => void
}) {
  const [items, setItems] = useState<GalleryItem[]>([])
  const [offset, setOffset] = useState(0)
  const [prepared, setPrepared] = useState<File[]>([])
  const [preparing, setPreparing] = useState(false)
  const [progress, setProgress] = useState(0)
  const [loading, setLoading] = useState(true)
  const [savedOneByOne, setSavedOneByOne] = useState(0)
  const closeRef = useRef<HTMLButtonElement>(null)
  const supportsBatchShare = canShareFiles([new File([''], 'zhyvo.jpg', { type: 'image/jpeg' })])

  useEffect(() => {
    let active = true
    async function loadItems() {
      try {
        const all: GalleryItem[] = []
        let cursor: string | null = null
        do {
          const page = await media.gallery(slug, cursor)
          all.push(...page.items)
          cursor = page.has_more ? page.next_cursor : null
        } while (cursor)
        if (active) setItems(all)
      } catch (cause) {
        if (active) onError(errorMessage(cause))
      } finally {
        if (active) setLoading(false)
      }
    }
    void loadItems()
    closeRef.current?.focus()
    return () => { active = false }
  }, [onError, slug])

  function nextBatch() {
    const batch: GalleryItem[] = []
    let total = 0
    for (const item of items.slice(offset)) {
      if (batch.length && (batch.length >= MOBILE_BATCH_FILES || total + item.size_bytes > MOBILE_BATCH_BYTES)) break
      batch.push(item)
      total += item.size_bytes
    }
    return batch
  }

  async function prepareBatch() {
    const batch = nextBatch()
    if (!batch.length) return
    setPreparing(true)
    setProgress(0)
    try {
      const files: File[] = []
      for (let index = 0; index < batch.length; index += 1) {
        const item = batch[index]
        const remote = await media.download(item.id)
        files.push(await fetchShareFile({ ...remote, mimeType: item.mime_type }))
        setProgress(Math.round(((index + 1) / batch.length) * 100))
      }
      if (!canShareFiles(files)) throw new Error('Телефон не підтримує передачу цього набору файлів')
      setPrepared(files)
    } catch (cause) {
      onError(errorMessage(cause))
    } finally {
      setPreparing(false)
    }
  }

  async function shareBatch() {
    try {
      await sharePreparedFiles(prepared, `${roomName} — Zhyvo`)
      setOffset((current) => current + prepared.length)
      setPrepared([])
      setProgress(0)
    } catch (cause) {
      if (!(cause instanceof DOMException && cause.name === 'AbortError')) onError(errorMessage(cause))
    }
  }

  async function saveNextFile() {
    const item = items[savedOneByOne]
    if (!item) return
    try {
      const remote = await media.download(item.id)
      await saveRemoteFile({ ...remote, mimeType: item.mime_type })
      setSavedOneByOne((current) => current + 1)
    } catch (cause) {
      onError(errorMessage(cause))
    }
  }

  const completed = supportsBatchShare ? offset >= items.length && items.length > 0 : savedOneByOne >= items.length && items.length > 0
  const batch = nextBatch()

  return (
    <div className="dialog-backdrop" role="presentation" onMouseDown={onClose}>
      <section className="mobile-save-dialog" role="dialog" aria-modal="true" aria-labelledby="mobile-save-title" onMouseDown={(event) => event.stopPropagation()}>
        <header>
          <div><p className="eyebrow">Оригінальна якість</p><h2 id="mobile-save-title">Зберегти на телефон</h2></div>
          <button ref={closeRef} className="icon-button" onClick={onClose} aria-label="Закрити"><X /></button>
        </header>
        <div className="mobile-save-body">
          {loading ? <p className="mobile-save-message">Збираємо список файлів…</p> : completed ? (
            <div className="mobile-save-complete"><Check size={30} /><strong>Усі файли передано</strong><span>Оберіть «Зберегти у Фото» або Files у системному меню.</span></div>
          ) : supportsBatchShare ? (
            <>
              <div className="mobile-save-summary"><Smartphone size={24} /><div><strong>{items.length - offset} файлів залишилося</strong><span>Передаємо пакетами до {MOBILE_BATCH_FILES}, щоб телефон не зависав.</span></div></div>
              {preparing && <div className="mobile-save-progress"><span style={{ width: `${progress}%` }} /></div>}
              {!prepared.length ? (
                <button className="primary-button" onClick={() => void prepareBatch()} disabled={preparing || !batch.length}>
                  <ArrowDownToLine size={19} /> {preparing ? `Готуємо… ${progress}%` : `Підготувати ${batch.length} ${batch.length === 1 ? 'файл' : 'файлів'}`}
                </button>
              ) : (
                <button className="primary-button" onClick={() => void shareBatch()}><Share2 size={19} /> Зберегти {prepared.length} файлів</button>
              )}
              <p className="field-note">Після підготовки відкриється системне меню телефона — там виберіть збереження у Фото, Галерею або Files.</p>
            </>
          ) : (
            <>
              <div className="mobile-save-summary"><Smartphone size={24} /><div><strong>{items.length - savedOneByOne} файлів залишилося</strong><span>Telegram попросить підтвердити кожен оригінал окремо.</span></div></div>
              <button className="primary-button" onClick={() => void saveNextFile()}><ArrowDownToLine size={19} /> Зберегти наступний файл</button>
            </>
          )}
          {!loading && !items.length && <p className="mobile-save-message">У кімнаті немає готових файлів.</p>}
        </div>
        <footer><button className="secondary-button" onClick={onClose}>{completed ? 'Готово' : 'Закрити'}</button></footer>
      </section>
    </div>
  )
}

function ShareDialog({ room, webURL, telegramURL, previewURL, onClose, onCopied }: {
  room: Room
  webURL: string
  telegramURL: string
  previewURL: string
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
    try { await shareNavigator.share({ title: room.name, text: `Приєднуйтеся до кімнати «${room.name}» у Zhyvo`, url: previewURL }) } catch { /* User cancelled the share sheet. */ }
  }

  return (
    <div className="dialog-backdrop" role="presentation" onMouseDown={onClose}>
      <section className="share-dialog" role="dialog" aria-modal="true" aria-labelledby="share-title" onMouseDown={(event) => event.stopPropagation()}>
        <header><div><p className="eyebrow">Кімната {room.slug}</p><h2 id="share-title">Запросити друзів</h2></div><button ref={closeRef} className="icon-button" onClick={onClose} aria-label="Закрити"><X /></button></header>
        <div className="qr-wrap"><QRCodeSVG value={telegramURL} size={224} level="M" title={`QR-код кімнати ${room.slug}`} data-invite-url={telegramURL} /></div>
        <p className="share-note">QR-код відкриє цю кімнату прямо в Telegram. Для захищеної кімнати PIN чи пароль передайте окремо.</p>
        <div className="share-actions">
          <button className="primary-button share-actions__telegram" onClick={() => openTelegramInvite(room.name, previewURL)}><Send size={18} /> Надіслати в Telegram</button>
          <button className="secondary-button" onClick={onCopied}><Copy size={18} /> Копіювати запрошення</button>
          {supportsNativeShare && <button className="secondary-button" onClick={nativeShare}><Share2 size={18} /> Інші застосунки</button>}
        </div>
        <a className="browser-invite-link" href={webURL}>Відкрити кімнату у браузері</a>
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
          {source && <button className="icon-button" onClick={() => void saveRemoteFile({ ...source, mimeType: item.mime_type }).catch((cause) => onError(errorMessage(cause)))} aria-label="Завантажити оригінал"><ArrowDownToLine /></button>}
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
  const session = useSession()
  const [searchParams, setSearchParams] = useSearchParams()
  const inputRef = useRef<HTMLInputElement>(null)
  const resumeInputRef = useRef<HTMLInputElement>(null)
  const resumeTargetRef = useRef<string | null>(null)
  const uploadFilesRef = useRef(new Map<string, File>())
  const uploadControllersRef = useRef(new Map<string, AbortController>())
  const uploadsRef = useRef<UploadProgress[]>([])
  const uploadsHydratedKeyRef = useRef('')
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
  const [blockedMembers, setBlockedMembers] = useState<BlockedRoomMember[]>([])
  const [activity, setActivity] = useState<RoomActivityEvent[]>([])
  const [roomArchive, setRoomArchive] = useState<RoomArchive | null>(null)
  const [archiveBusy, setArchiveBusy] = useState(false)
  const [mobileSaveOpen, setMobileSaveOpen] = useState(false)
  const [membersTab, setMembersTab] = useState<'members' | 'activity'>('members')
  const [memberActionID, setMemberActionID] = useState<string | null>(null)
  const [membersLoading, setMembersLoading] = useState(false)
  const [membersError, setMembersError] = useState('')
  const selectedMediaID = searchParams.get('media')

  useEffect(() => { uploadsRef.current = uploads }, [uploads])

  useEffect(() => {
    if (!room || !session) return
    const key = `${session.identity.id}:${slug}`
    if (uploadsHydratedKeyRef.current === key) return
    uploadsHydratedKeyRef.current = key
    setUploads(loadUploadQueue(session.identity.id, slug))
  }, [room, session, slug])

  useEffect(() => {
    if (!session || uploadsHydratedKeyRef.current !== `${session.identity.id}:${slug}`) return
    saveUploadQueue(session.identity.id, slug, uploads)
  }, [session, slug, uploads])

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
    if (!roomArchive || !['pending', 'processing'].includes(roomArchive.status)) return
    const timer = window.setInterval(() => {
      void archives.get(roomArchive.id).then(({ archive }) => setRoomArchive(archive)).catch((cause) => {
        setError(errorMessage(cause))
        setRoomArchive(null)
      })
    }, 1500)
    return () => window.clearInterval(timer)
  }, [roomArchive])

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

  const processUpload = useCallback(async (queueID: string, file: File, initialItem?: UploadProgress) => {
    const controller = uploadControllersRef.current.get(queueID) ?? new AbortController()
    uploadControllersRef.current.set(queueID, controller)
    if (controller.signal.aborted) return
    const queueItem = initialItem ?? uploadsRef.current.find((item) => item.id === queueID)
    if (!queueItem) return
    setUploads((current) => current.map((item) => item.id === queueID ? { ...item, state: 'uploading', message: undefined, canRetry: false } : item))
    try {
      await uploadFile(slug, file, {
        signal: controller.signal,
        idempotencyKey: queueItem.idempotency_key,
        mimeType: queueItem.mime_type,
        completedParts: queueItem.completed_parts,
        onProgress: (progress) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, progress, message: undefined } : item)),
        onStatus: (message) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, message } : item)),
        onCheckpoint: ({ uploadID, completedParts }) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, upload_id: uploadID, completed_parts: completedParts } : item)),
      })
      setUploads((current) => current.map((item) => item.id === queueID ? { ...item, state: 'done', progress: 100, message: undefined } : item))
      setRoomArchive(null)
      uploadFilesRef.current.delete(queueID)
      uploadControllersRef.current.delete(queueID)
    } catch (cause) {
      const cancelled = controller.signal.aborted
      const sessionExpired = cause instanceof ApiError && ['UPLOAD_EXPIRED', 'UPLOAD_NOT_FOUND'].includes(cause.code)
      setUploads((current) => current.map((item) => item.id === queueID ? {
        ...item,
        state: 'error',
        message: cancelled ? 'Скасовано' : sessionExpired ? 'Сесія завершилася — почнемо файл заново' : errorMessage(cause),
        canRetry: !cancelled,
        ...(sessionExpired ? { idempotency_key: uuid(), upload_id: undefined, completed_parts: [] } : {}),
      } : item))
      uploadControllersRef.current.delete(queueID)
    }
  }, [slug])

  const openMedia = useCallback((item: GalleryItem) => setSearchParams({ media: item.id }), [setSearchParams])
  const closeMedia = useCallback(() => setSearchParams({}, { replace: true }), [setSearchParams])

  async function acceptFiles(files: FileList | null) {
    if (!files?.length || !room) return
    const accepted = Array.from(files)
    const queue: UploadProgress[] = accepted.map((file) => ({
      id: uuid(),
      filename: file.name,
      size_bytes: file.size,
      mime_type: file.type,
      last_modified: file.lastModified,
      idempotency_key: uuid(),
      created_at: new Date().toISOString(),
      progress: 0,
      state: 'queued',
    }))
    queue.forEach((item, index) => {
      uploadFilesRef.current.set(item.id, accepted[index])
      uploadControllersRef.current.set(item.id, new AbortController())
    })
    setUploads((current) => [...current.filter((item) => item.state !== 'done'), ...queue])
    let nextIndex = 0
    const worker = async () => {
      while (nextIndex < queue.length) {
        const index = nextIndex++
        await processUpload(queue[index].id, accepted[index], queue[index])
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
    setUploads((current) => current.map((item) => item.id === id ? { ...item, state: 'error', message: 'Скасовано', canRetry: false } : item))
    window.setTimeout(() => setUploads((current) => current.filter((item) => item.id !== id)), 1800)
  }

  function retryUpload(id: string) {
    const file = uploadFilesRef.current.get(id)
    if (!file) {
      resumeTargetRef.current = id
      resumeInputRef.current?.click()
      return
    }
    uploadControllersRef.current.set(id, new AbortController())
    setUploads((current) => current.map((item) => item.id === id ? { ...item, progress: 0, state: 'queued', message: undefined, canRetry: false } : item))
    void processUpload(id, file).then(async () => {
      await refreshGallery().catch(() => undefined)
      const refreshedRoom = await rooms.get(slug).catch(() => null)
      if (refreshedRoom) setRoom(refreshedRoom.room)
    })
  }

  function resumeUpload(files: FileList | null) {
    const id = resumeTargetRef.current
    const file = files?.[0]
    resumeTargetRef.current = null
    if (resumeInputRef.current) resumeInputRef.current.value = ''
    if (!id || !file) return
    const item = uploadsRef.current.find((candidate) => candidate.id === id)
    if (!item) return
    if (file.name !== item.filename || file.size !== item.size_bytes) {
      setUploads((current) => current.map((candidate) => candidate.id === id ? { ...candidate, state: 'waiting_file', message: 'Оберіть той самий файл: назва й розмір мають збігатися', canRetry: true } : candidate))
      return
    }
    uploadFilesRef.current.set(id, file)
    uploadControllersRef.current.set(id, new AbortController())
    const resumed = { ...item, state: 'queued' as const, message: undefined, canRetry: false }
    setUploads((current) => current.map((candidate) => candidate.id === id ? resumed : candidate))
    void processUpload(id, file, resumed).then(async () => {
      await refreshGallery().catch(() => undefined)
      const refreshedRoom = await rooms.get(slug).catch(() => null)
      if (refreshedRoom) setRoom(refreshedRoom.room)
    })
  }

  async function copyLink() {
    const url = roomInviteLink(slug)
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
    setMembersTab('members')
    setMembersLoading(true)
    setMembersError('')
    try {
      const [memberResult, activityResult] = await Promise.all([rooms.members(slug), rooms.activity(slug)])
      setMembers(memberResult.members)
      setBlockedMembers(memberResult.blocked_members)
      setActivity(activityResult.events)
    } catch (cause) {
      setMembersError(errorMessage(cause))
    } finally {
      setMembersLoading(false)
    }
  }

  async function refreshMembers() {
    const [memberResult, activityResult] = await Promise.all([rooms.members(slug), rooms.activity(slug)])
    setMembers(memberResult.members)
    setBlockedMembers(memberResult.blocked_members)
    setActivity(activityResult.events)
  }

  async function removeMember(member: RoomMember) {
    if (!window.confirm(`Видалити ${member.display_name} з кімнати та заблокувати повторний вхід?`)) return
    setMemberActionID(member.id)
    setMembersError('')
    try {
      await rooms.removeMember(slug, member.id)
      await refreshMembers()
    } catch (cause) {
      setMembersError(errorMessage(cause))
    } finally {
      setMemberActionID(null)
    }
  }

  async function unblockMember(member: BlockedRoomMember) {
    setMemberActionID(member.id)
    setMembersError('')
    try {
      await rooms.unblockMember(slug, member.id)
      await refreshMembers()
    } catch (cause) {
      setMembersError(errorMessage(cause))
    } finally {
      setMemberActionID(null)
    }
  }

  async function transferOwnership(member: RoomMember) {
    if (!window.confirm(`Передати кімнату користувачу ${member.display_name}? Ви втратите права власника.`)) return
    setMemberActionID(member.id)
    setMembersError('')
    try {
      const result = await rooms.transferOwnership(slug, member.id)
      setRoom(result.room)
      setMembersOpen(false)
    } catch (cause) {
      setMembersError(errorMessage(cause))
    } finally {
      setMemberActionID(null)
    }
  }

  async function deleteItem(item: GalleryItem) {
    if (!window.confirm(`Видалити «${item.original_filename}» назавжди?`)) return
    try {
      await media.remove(item.id)
      setGallery((current) => current.filter((candidate) => candidate.id !== item.id))
      setRoomArchive(null)
    } catch (cause) { setError(errorMessage(cause)) }
  }

  async function deleteRoom() {
    if (!room || !window.confirm('Видалити кімнату та всі її файли назавжди?')) return
    try {
      await rooms.remove(slug)
      navigate('/')
    } catch (cause) { setError(errorMessage(cause)) }
  }

  async function handleArchive() {
    if (archiveBusy) return
    setArchiveBusy(true)
    setError('')
    try {
      let job = roomArchive
      if (!job || job.status === 'failed') {
        const result = await archives.request(slug)
        job = result.archive
        setRoomArchive(job)
      }
      if (job.status === 'ready') {
        const download = await archives.download(job.id)
        const anchor = document.createElement('a')
        anchor.href = download.url
        anchor.download = download.filename
        anchor.rel = 'noopener'
        anchor.click()
      }
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setArchiveBusy(false)
    }
  }

  if (loading) return <main className="status-page"><Brand /><div className="loading-line" /><p>Відкриваємо кімнату…</p></main>
  if (error && !room && !preview) return <main className="status-page"><Brand /><h1>Не вдалося відкрити кімнату</h1><p>{error}</p><Link className="primary-button" to="/">На головну</Link></main>
  if (preview && !room) return <JoinRoom preview={preview} onJoined={(joined) => { setPreview(null); setRoom(joined); void loadGallery() }} />
  if (!room) return null

  const ttl = remaining(room.expires_at)
  const usedPercent = Math.min(100, (room.used_storage_bytes / room.max_storage_bytes) * 100)
  const selectedMedia = gallery.find((item) => item.id === selectedMediaID) ?? null
  const shareURL = `${window.location.origin}/r/${slug}`
  const telegramInviteURL = telegramRoomLink(slug)
  const previewInviteURL = roomInviteLink(slug)

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
        <div className="gallery-toolbar__actions">
          {gallery.length > 0 && (
            <button className="secondary-button primary-button--fit archive-button" onClick={() => isMobileDevice() ? setMobileSaveOpen(true) : void handleArchive()} disabled={!isMobileDevice() && (archiveBusy || roomArchive?.status === 'pending' || roomArchive?.status === 'processing')} aria-label="Завантажити всю галерею">
              {isMobileDevice() ? <Smartphone size={19} /> : <Archive size={19} />}
              <small>{isMobileDevice() ? 'Усе' : 'ZIP'}</small>
              <span>{isMobileDevice() ? 'Зберегти на телефон' : roomArchive?.status === 'ready' ? 'Завантажити ZIP' : roomArchive?.status === 'failed' ? 'Повторити ZIP' : roomArchive ? `${roomArchive.processed_files}/${roomArchive.total_files}` : 'Завантажити все'}</span>
            </button>
          )}
          {room.accepting_uploads ? (
          <>
            <input ref={inputRef} type="file" accept="image/jpeg,image/png,image/webp,image/heic,image/heif,image/avif,image/gif,video/mp4,video/quicktime,video/webm,video/x-m4v,video/3gpp" multiple hidden onChange={(event) => void acceptFiles(event.target.files)} />
            <button className="primary-button primary-button--fit upload-button" onClick={() => inputRef.current?.click()} aria-label="Додати фото або відео"><ImagePlus size={21} /><span>Додати медіа</span></button>
          </>
          ) : <span className="uploads-closed"><LockKeyhole size={16} /> Завантаження закриті</span>}
        </div>
      </section>
      <input ref={resumeInputRef} data-resume-upload type="file" hidden onChange={(event) => resumeUpload(event.target.files)} />

      <div className="storage-line"><span style={{ width: `${usedPercent}%` }} /></div>

      {roomArchive && <section className={`archive-status archive-status--${roomArchive.status}`} aria-live="polite">
        <div><Archive size={20} /><div><strong>{roomArchive.status === 'ready' ? 'Архів готовий' : roomArchive.status === 'failed' ? 'Не вдалося створити архів' : roomArchive.status === 'pending' ? 'Архів у черзі' : 'Збираємо оригінали'}</strong><span>{roomArchive.status === 'ready' ? `${roomArchive.total_files} файлів · ${bytes(roomArchive.size_bytes ?? roomArchive.total_bytes)}` : roomArchive.status === 'failed' ? 'Натисніть «Повторити ZIP»' : `${roomArchive.processed_files} із ${roomArchive.total_files} файлів`}</span></div></div>
        <div className="archive-progress"><span style={{ width: `${roomArchive.total_files ? (roomArchive.processed_files / roomArchive.total_files) * 100 : 0}%` }} /></div>
      </section>}

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

      {shareDialog && <ShareDialog room={room} webURL={shareURL} telegramURL={telegramInviteURL} previewURL={previewInviteURL} onClose={() => setShareDialog(false)} onCopied={() => void copyLink()} />}
      {mobileSaveOpen && <MobileSaveDialog slug={slug} roomName={room.name} onClose={() => setMobileSaveOpen(false)} onError={setError} />}

      {membersOpen && (
        <div className="dialog-backdrop" role="presentation" onMouseDown={() => setMembersOpen(false)}>
          <section className="members-dialog" role="dialog" aria-modal="true" aria-labelledby="members-title" onMouseDown={(event) => event.stopPropagation()}>
            <header><div><h2 id="members-title">Керування кімнатою</h2><p>{membersLoading ? 'Оновлюємо дані…' : `${members.length} активних · ${blockedMembers.length} заблоковано`}</p></div><button ref={membersCloseRef} className="icon-button" onClick={() => setMembersOpen(false)} aria-label="Закрити"><X /></button></header>
            <div className="members-tabs" role="tablist" aria-label="Керування кімнатою">
              <button role="tab" aria-selected={membersTab === 'members'} onClick={() => setMembersTab('members')}>Учасники</button>
              <button role="tab" aria-selected={membersTab === 'activity'} onClick={() => setMembersTab('activity')}>Історія</button>
            </div>
            {membersError ? <p className="members-error" role="alert">{membersError}</p> : membersLoading ? <div className="members-loading"><span /><span /><span /></div> : (
              membersTab === 'members' ? (
                <div className="members-list">
                  {members.map((member) => (
                    <div className="member-row" key={member.id}>
                      <span className="member-avatar" aria-hidden="true">{member.display_name.trim().slice(0, 1).toLocaleUpperCase('uk-UA')}</span>
                      <div><strong>{member.display_name}</strong><span>Приєднався {new Date(member.joined_at).toLocaleString('uk-UA', { dateStyle: 'medium', timeStyle: 'short' })}</span></div>
                      {member.role === 'owner' ? <small>Власник</small> : (
                        <div className="member-actions">
                          <button disabled={memberActionID === member.id} onClick={() => void transferOwnership(member)} aria-label={`Передати кімнату користувачу ${member.display_name}`} title="Передати права власника"><Crown size={17} /></button>
                          <button className="member-action--danger" disabled={memberActionID === member.id} onClick={() => void removeMember(member)} aria-label={`Видалити та заблокувати ${member.display_name}`} title="Видалити та заблокувати"><Ban size={17} /></button>
                        </div>
                      )}
                    </div>
                  ))}
                  {blockedMembers.length > 0 && <div className="blocked-heading"><strong>Заблоковані</strong><span>Не можуть повторно приєднатися</span></div>}
                  {blockedMembers.map((member) => (
                    <div className="member-row member-row--blocked" key={member.id}>
                      <span className="member-avatar" aria-hidden="true">{member.display_name.trim().slice(0, 1).toLocaleUpperCase('uk-UA')}</span>
                      <div><strong>{member.display_name}</strong><span>Заблоковано {new Date(member.blocked_at).toLocaleString('uk-UA', { dateStyle: 'medium', timeStyle: 'short' })}</span></div>
                      <button className="unblock-button" disabled={memberActionID === member.id} onClick={() => void unblockMember(member)}><RotateCcw size={16} /> Розблокувати</button>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="activity-list">
                  {activity.length === 0 ? <p>Подій поки немає.</p> : activity.map((event) => (
                    <div className="activity-row" key={event.id}>
                      <span aria-hidden="true" />
                      <div><strong>{activityLabel(event)}</strong><time dateTime={event.created_at}>{new Date(event.created_at).toLocaleString('uk-UA', { dateStyle: 'medium', timeStyle: 'short' })}</time></div>
                    </div>
                  ))}
                </div>
              )
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
        <Route path="/auth/telegram/callback" element={<TelegramLoginCallback />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </>
  )
}
