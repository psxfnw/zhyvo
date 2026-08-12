import { CSSProperties, FormEvent, useCallback, useEffect, useRef, useState } from 'react'
import {
  ArrowDownToLine,
  Archive,
  ArrowLeft,
  Ban,
  BarChart3,
  Bug,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleHelp,
  Clock3,
  Copy,
  Crown,
  Eye,
  FileImage,
  ImagePlus,
  Info,
  Images,
  Inbox,
  Heart,
  LockKeyhole,
  LogIn,
  Menu,
  Pause,
  Pencil,
  Play,
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
import { admin, ApiError, archives, auth, ensureIdentity, getLastAPIErrorContext, getSession, media, problemReports, rooms, streamRoomEvents } from './lib/api'
import { bytes, errorMessage, normalizeSlug, remaining } from './lib/format'
import { canShareFiles, fetchShareFile, isMobileDevice, saveRemoteFile, sharePreparedFiles } from './lib/download'
import { consumeTelegramStartParam, getTelegramBootstrapError, getTelegramStartParam, getTelegramWebApp, managedBrowserInviteLink, managedInviteLink, openTelegramInvite, roomInviteLink, telegramBrowserLink, telegramInviteLink, telegramRoomLink, telegramStartPath } from './lib/telegram'
import { uploadFile } from './lib/upload'
import { loadUploadQueue, saveUploadQueue } from './lib/uploadQueue'
import { mediaCapturedAt } from './lib/metadata'
import { checksumFile } from './lib/checksum'
import { completeTelegramLogin, startTelegramLogin } from './lib/telegramLogin'
import type { AccessMode, AdminStats, BlockedRoomMember, GalleryItem, ProblemReport, ProblemReportCategory, ProblemReportStatus, Room, RoomActivityEvent, RoomArchive, RoomInvite, RoomInviteList, RoomInvitePreview, RoomMember, RoomNotificationSettings, RoomPreview, RoomRecap, Session, UploadProgress } from './types'

function uuid() {
  return crypto.randomUUID()
}

type GalleryFilter = 'all' | 'image' | 'video' | 'mine' | 'favorites' | 'best'

function mediaDate(item: GalleryItem) {
  return new Date(item.captured_at ?? item.created_at)
}

function sortGallery(items: GalleryItem[]) {
  return [...items].sort((left, right) => {
    const dateDifference = mediaDate(right).getTime() - mediaDate(left).getTime()
    return dateDifference || right.id.localeCompare(left.id)
  })
}

function galleryDay(item: GalleryItem) {
  const date = mediaDate(item)
  const today = new Date()
  const yesterday = new Date(today)
  yesterday.setDate(today.getDate() - 1)
  const dateKey = date.toLocaleDateString('sv-SE')
  if (dateKey === today.toLocaleDateString('sv-SE')) return { key: dateKey, label: 'Сьогодні' }
  if (dateKey === yesterday.toLocaleDateString('sv-SE')) return { key: dateKey, label: 'Учора' }
  return {
    key: dateKey,
    label: date.toLocaleDateString('uk-UA', { day: 'numeric', month: 'long', year: 'numeric' }),
  }
}

function mediaDuration(durationMS?: number) {
  if (!durationMS || durationMS < 0) return ''
  const totalSeconds = Math.round(durationMS / 1000)
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return `${minutes}:${seconds.toString().padStart(2, '0')}`
}

function isHEIFItem(item: GalleryItem) {
  return item.mime_type === 'image/heic' || item.mime_type === 'image/heif'
}

function uploadMimeType(file: File) {
  if (file.type) return file.type.toLowerCase() === 'image/jpg' ? 'image/jpeg' : file.type.toLowerCase()
  const extension = file.name.split('.').at(-1)?.toLowerCase()
  return ({ jpg: 'image/jpeg', jpeg: 'image/jpeg', png: 'image/png', webp: 'image/webp', heic: 'image/heic', heif: 'image/heif', avif: 'image/avif', gif: 'image/gif', mp4: 'video/mp4', mov: 'video/quicktime', webm: 'video/webm', m4v: 'video/x-m4v', '3gp': 'video/3gpp' } as Record<string, string>)[extension ?? ''] ?? ''
}

function shouldAvoidPreReadingMobileFile() {
  return getTelegramWebApp()?.platform.toLowerCase() === 'android'
}

async function stabilizeAndroidGalleryFiles(files: File[]) {
  if (!shouldAvoidPreReadingMobileFile()) return files
  const candidates = files.filter((file) => uploadMimeType(file).startsWith('image/') && file.size <= 32 * 1024 * 1024)
  const totalBytes = candidates.reduce((sum, file) => sum + file.size, 0)
  if (!candidates.length || totalBytes > 192 * 1024 * 1024) return files
  const stable = new Map<File, File>()
  await Promise.all(candidates.map(async (file) => {
    try {
      const contents = await file.arrayBuffer()
      stable.set(file, new File([contents], file.name, { type: uploadMimeType(file), lastModified: file.lastModified }))
    } catch {
      // Keep the original handle: the queue will surface a retry action if Android revoked it.
    }
  }))
  return files.map((file) => stable.get(file) ?? file)
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
  useEffect(() => {
    if (location.pathname !== '/') return
    const path = telegramStartPath(getTelegramStartParam())
    if (path) navigate(path, { replace: true })
  }, [location.pathname, navigate])
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

const reportCategoryLabels: Record<ProblemReportCategory, string> = {
  upload: 'Завантаження', download: 'Збереження', room: 'Кімната або запрошення', telegram: 'Telegram', other: 'Інше',
}

const reportStatusLabels: Record<ProblemReportStatus, string> = {
  new: 'Нове', in_progress: 'У роботі', resolved: 'Вирішено', closed: 'Закрито',
}

function ProblemReportDialog({ onClose }: { onClose: () => void }) {
  const closeRef = useRef<HTMLButtonElement>(null)
  const [category, setCategory] = useState<ProblemReportCategory>('other')
  const [description, setDescription] = useState('')
  const [contact, setContact] = useState('')
  const [includeTechnical, setIncludeTechnical] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [publicID, setPublicID] = useState('')

  useEffect(() => {
    closeRef.current?.focus()
    const keydown = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    document.addEventListener('keydown', keydown)
    return () => document.removeEventListener('keydown', keydown)
  }, [onClose])

  async function submit(event: FormEvent) {
    event.preventDefault()
    if (description.trim().length < 10) {
      setError('Опишіть проблему трохи детальніше — щонайменше 10 символів.')
      return
    }
    setBusy(true)
    setError('')
    const lastError = getLastAPIErrorContext()
    try {
      const result = await problemReports.create({
        category,
        description,
        contact: contact.trim() || undefined,
        technical_context: includeTechnical ? {
          route: window.location.pathname,
          app_build: import.meta.env.VITE_APP_VERSION || import.meta.env.MODE,
          platform: getTelegramWebApp()?.platform || navigator.platform || 'web',
          browser: navigator.userAgent,
          telegram: Boolean(getTelegramWebApp()),
          online: navigator.onLine,
          error_code: lastError?.code,
          request_id: lastError?.request_id,
          occurred_at: lastError?.occurred_at,
        } : {},
      })
      setPublicID(result.report.public_id)
      getTelegramWebApp()?.HapticFeedback?.impactOccurred('medium')
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  return (
    <div className="dialog-backdrop problem-report-backdrop" role="presentation" onMouseDown={onClose}>
      <section className="problem-report-dialog" role="dialog" aria-modal="true" aria-labelledby="problem-report-title" onMouseDown={(event) => event.stopPropagation()}>
        <header><div><p className="eyebrow">Зворотний зв’язок</p><h2 id="problem-report-title">Повідомити про проблему</h2></div><button ref={closeRef} className="icon-button" onClick={onClose} aria-label="Закрити"><X /></button></header>
        {publicID ? <div className="problem-report-success"><ShieldCheck size={48} /><h3>Звернення отримано</h3><p>Ми зберегли його під номером <strong>{publicID}</strong>.</p><button className="primary-button" onClick={onClose}>Готово</button></div> : <form onSubmit={submit}>
          <label className="field"><span>Що не працює</span><select value={category} onChange={(event) => setCategory(event.target.value as ProblemReportCategory)}>{Object.entries(reportCategoryLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label>
          <label className="field"><span>Опишіть проблему</span><textarea value={description} onChange={(event) => setDescription(event.target.value)} minLength={10} maxLength={2000} required placeholder="Що ви робили та що сталося?" /><small>{description.length} / 2000</small></label>
          <label className="field"><span>Контакт для відповіді — необов’язково</span><input value={contact} onChange={(event) => setContact(event.target.value)} maxLength={160} placeholder="@username або email" /></label>
          <label className="technical-consent"><input type="checkbox" checked={includeTechnical} onChange={(event) => setIncludeTechnical(event.target.checked)} /><span><strong>Додати технічну інформацію</strong><small>Сторінка, пристрій, версія Zhyvo та код останньої помилки. Без фото, паролів, токенів і назв файлів.</small></span></label>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button type="submit" className="primary-button" disabled={busy}><Bug size={18} /> {busy ? 'Надсилаємо…' : 'Надіслати звернення'}</button>
        </form>}
      </section>
    </div>
  )
}

function ProblemReporter() {
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<{ left: number; top: number } | null>(() => {
    try { return JSON.parse(localStorage.getItem('photodrop.problem-position.v1') ?? 'null') }
    catch { return null }
  })
  const drag = useRef<{ pointer: number; dx: number; dy: number; moved: boolean } | null>(null)
  const suppressClick = useRef(false)
  function pointerDown(event: React.PointerEvent<HTMLButtonElement>) {
    const box = event.currentTarget.getBoundingClientRect()
    drag.current = { pointer: event.pointerId, dx: event.clientX - box.left, dy: event.clientY - box.top, moved: false }
    event.currentTarget.setPointerCapture(event.pointerId)
  }
  function pointerMove(event: React.PointerEvent<HTMLButtonElement>) {
    const state = drag.current
    if (!state || state.pointer !== event.pointerId) return
    const width = event.currentTarget.offsetWidth
    const height = event.currentTarget.offsetHeight
    const next = {
      left: Math.max(8, Math.min(window.innerWidth - width - 8, event.clientX - state.dx)),
      top: Math.max(8, Math.min(window.innerHeight - height - 8, event.clientY - state.dy)),
    }
    if (Math.abs(next.left - event.currentTarget.getBoundingClientRect().left) > 3 || Math.abs(next.top - event.currentTarget.getBoundingClientRect().top) > 3) state.moved = true
    setPosition(next)
  }
  function pointerUp(event: React.PointerEvent<HTMLButtonElement>) {
    const state = drag.current
    if (!state || state.pointer !== event.pointerId) return
    suppressClick.current = state.moved
    drag.current = null
    if (position) localStorage.setItem('photodrop.problem-position.v1', JSON.stringify(position))
  }
  return <><button className="problem-report-trigger" style={position ? { ...position, right: 'auto', bottom: 'auto' } : undefined} onPointerDown={pointerDown} onPointerMove={pointerMove} onPointerUp={pointerUp} onClick={() => { if (suppressClick.current) { suppressClick.current = false; return }; setOpen(true) }} title="Можна перетягнути в зручне місце"><Bug size={16} /> Повідомити про проблему</button>{open && <ProblemReportDialog onClose={() => setOpen(false)} />}</>
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
	const navigate = useNavigate()
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(`photodrop.login-notice.${session.identity.id}`) === 'hidden')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  if (dismissed || session.identity.kind !== 'anonymous' || getTelegramWebApp()) return null

  function login() { setBusy(true); setError(''); navigate(`/auth/telegram/link?returnTo=${encodeURIComponent(window.location.pathname + window.location.search)}`) }

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

function BrowserLinkPage() {
  const navigate = useNavigate()
  const [search] = useSearchParams()
  const session = useSession()
  const [challenge, setChallenge] = useState<{ token: string; expires_at: string } | null>(null)
  const [error, setError] = useState('')
  const started = useRef(false)
  const returnTo = search.get('returnTo')?.startsWith('/') ? search.get('returnTo')! : '/'

  useEffect(() => {
    if (!session || session.identity.kind !== 'anonymous' || started.current) return
    started.current = true
    auth.createBrowserLink().then(setChallenge).catch((cause) => setError(errorMessage(cause)))
  }, [session])

  useEffect(() => {
    if (!challenge) return
    let active = true
    const poll = async () => {
      try {
        const result = await auth.browserLinkStatus(challenge.token)
        if (!active) return
        if (result.status === 'approved') { await auth.exchangeBrowserLink(challenge.token); navigate(returnTo, { replace: true }); return }
        if (result.status === 'expired' || result.status === 'denied') { setError('Запит більше не активний. Поверніться назад і створіть новий.'); return }
        setTimeout(poll, 1800)
      } catch (cause) { if (active) setError(errorMessage(cause)) }
    }
    const timer = setTimeout(poll, 900)
    return () => { active = false; clearTimeout(timer) }
  }, [challenge, navigate, returnTo])

  if (session?.identity.kind === 'telegram') return <Navigate to={returnTo} replace />
  const telegramURL = challenge ? telegramBrowserLink(challenge.token) : ''
  return <main className="link-flow"><section className="link-flow__card"><div className="link-flow__top"><Brand /><div className="link-flow__step">01 / 03</div></div><h1>Підтвердьте вхід у Telegram</h1><p>Відскануйте QR-код або відкрийте Zhyvo в Telegram. Там ви окремо підтвердите підключення цього браузера.</p>{challenge && <><div className="link-flow__qr"><QRCodeSVG value={telegramURL} size={176} level="M" title="Відкрити підтвердження Zhyvo у Telegram" /></div><a className="primary-button" href={telegramURL} target="_blank" rel="noopener noreferrer">Відкрити Telegram</a><div className="link-flow__waiting"><span /> Очікуємо підтвердження</div></>}{!challenge && !error && <div className="loading-line" />}{error && <p className="form-error" role="alert">{error}</p>}<div className="link-flow__alternatives"><button className="text-button" onClick={() => void startTelegramLogin(returnTo)}>Стандартний вхід Telegram</button><Link to={returnTo}>Скасувати</Link></div></section></main>
}

function TelegramLinkConfirmPage() {
	const navigate = useNavigate()
  const [search] = useSearchParams()
  const session = useSession()
  const token = search.get('token') ?? ''
  const approvedTokenKey = 'photodrop.telegram.approved-browser-link.v1'
  const [status, setStatus] = useState<'ready' | 'busy' | 'done'>(() => sessionStorage.getItem(approvedTokenKey) === token ? 'done' : 'ready')
  const [error, setError] = useState('')
  async function approve() {
    setStatus('busy'); setError('')
    try { await auth.approveBrowserLink(token); sessionStorage.setItem(approvedTokenKey, token); consumeTelegramStartParam(); setStatus('done'); getTelegramWebApp()?.HapticFeedback?.impactOccurred('medium') }
    catch (cause) { setError(errorMessage(cause)); setStatus('ready') }
  }
  if (!getTelegramWebApp() || session?.identity.kind !== 'telegram') return <main className="status-page"><Brand /><h1>Відкрийте цей запит у Telegram</h1><p>Підтвердження доступне лише всередині Mini App Zhyvo.</p></main>
  return <main className="link-flow"><section className="link-flow__card"><div className="link-flow__top"><Brand /><div className="link-flow__step">{status === 'done' ? '03 / 03' : '02 / 03'}</div></div>{status === 'done' ? <><ShieldCheck className="link-flow__success" size={52} /><h1>Браузер підключено</h1><p>Вкладка браузера автоматично повернеться на головну Zhyvo.</p><button className="primary-button" onClick={() => { consumeTelegramStartParam(); navigate('/', { replace: true }) }}>На головну Zhyvo</button></> : <><h1>Підключити браузер?</h1><p>Браузер отримає доступ до ваших кімнат Zhyvo. Доступу до повідомлень, контактів і номера Telegram не буде.</p><button className="primary-button" onClick={() => void approve()} disabled={status === 'busy'}><ShieldCheck size={18} />{status === 'busy' ? 'Підтверджуємо…' : 'Підтвердити'}</button>{error && <p className="form-error" role="alert">{error}</p>}</>}</section></main>
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

const ONBOARDING_KEY = 'photodrop.onboarding.v1'

const onboardingSteps = [
  {
    icon: Images,
    title: 'Створіть кімнату',
    body: 'Назвіть подію, виберіть термін від одного до трьох днів і за потреби захистіть вхід PIN-кодом або паролем.',
  },
  {
    icon: Send,
    title: 'Запросіть друзів',
    body: 'Надішліть посилання або покажіть QR-код. Реєстрація не обов’язкова — учасники одразу потраплять до спільної галереї.',
  },
  {
    icon: Clock3,
    title: 'Збережіть потрібне',
    body: 'Фото й відео завантажуються в оригінальній якості. Після завершення вибраного терміну кімната та всі її файли видаляються назавжди.',
  },
] as const

function Onboarding({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [step, setStep] = useState(0)
  const dialogRef = useRef<HTMLElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const current = onboardingSteps[step]
  const StepIcon = current.icon

  useEffect(() => {
    if (!open) return
    setStep(0)
    const previousFocus = document.activeElement as HTMLElement | null
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    closeRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
      if (event.key !== 'Tab') return
      const focusable = dialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]), a[href]')
      if (!focusable?.length) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      document.body.style.overflow = previousOverflow
      previousFocus?.focus()
    }
  }, [onClose, open])

  if (!open) return null
  return (
    <div className="onboarding-backdrop" role="presentation">
      <section ref={dialogRef} className="onboarding" role="dialog" aria-modal="true" aria-labelledby="onboarding-title">
        <header>
          <Brand compact />
          <span>{String(step + 1).padStart(2, '0')} / 03</span>
          <button ref={closeRef} onClick={onClose} aria-label="Закрити знайомство"><X /></button>
        </header>
        <div className="onboarding-content">
          <div className="onboarding-visual" aria-hidden="true"><strong>{String(step + 1).padStart(2, '0')}</strong><StepIcon /></div>
          <div aria-live="polite"><h2 id="onboarding-title">{current.title}</h2><p>{current.body}</p></div>
        </div>
        <footer>
          <div className="onboarding-dots" role="status" aria-label={`Крок ${step + 1} з 3`}>{onboardingSteps.map((_, index) => <span className={index === step ? 'is-active' : ''} key={index} />)}</div>
          {step === 0
            ? <button className="onboarding-skip" onClick={onClose}>Пропустити</button>
            : <button className="onboarding-skip" onClick={() => setStep((value) => value - 1)}>Назад</button>}
          {step < onboardingSteps.length - 1
            ? <button className="onboarding-next" onClick={() => setStep((value) => value + 1)}>Далі</button>
            : <button className="onboarding-next onboarding-next--final" onClick={onClose}>Почати</button>}
        </footer>
      </section>
    </div>
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
  const [onboardingOpen, setOnboardingOpen] = useState(false)
  const onboardingCheckedRef = useRef(false)

  const closeOnboarding = useCallback(() => {
    localStorage.setItem(ONBOARDING_KEY, 'complete')
    setOnboardingOpen(false)
  }, [])

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

  useEffect(() => {
    if (roomsLoading || onboardingCheckedRef.current) return
    onboardingCheckedRef.current = true
    if (localStorage.getItem(ONBOARDING_KEY) === 'complete') return
    if (activeRooms.length > 0) {
      localStorage.setItem(ONBOARDING_KEY, 'complete')
      return
    }
    setOnboardingOpen(true)
  }, [activeRooms.length, roomsLoading])

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
      navigate(`/r/${room.slug}`, { state: { justCreated: true } })
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
        <div className="home-header__actions">
          <button className="how-it-works" onClick={() => setOnboardingOpen(true)} aria-label="Як працює Zhyvo"><CircleHelp size={18} /><span>Як це працює</span></button>
          {session && <ProfileChip session={session} />}
        </div>
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
      <Onboarding open={onboardingOpen} onClose={closeOnboarding} />
    </main>
  )
}

function TelegramLoginCallback() {
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const started = useRef(false)
  useEffect(() => {
    if (started.current) return
    started.current = true
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

function JoinRoom({ preview, onJoined, joinRoom }: { preview: RoomPreview; onJoined: (room: Room) => void; joinRoom?: (secret: string) => Promise<{ room: Room }> }) {
  const session = useSession()
  const [displayName, setDisplayName] = useState('')
  const [secret, setSecret] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const autoJoinStarted = useRef(false)

  useEffect(() => {
    if (!session || preview.access_mode !== 'public' || !preview.accepting_members || autoJoinStarted.current) return
    autoJoinStarted.current = true
    setBusy(true)
    const action = joinRoom ? joinRoom('') : rooms.join(preview.slug, '')
    action.then(({ room }) => onJoined(room)).catch((cause) => {
      setError(errorMessage(cause))
      setBusy(false)
      autoJoinStarted.current = false
    })
  }, [joinRoom, onJoined, preview.access_mode, preview.accepting_members, preview.slug, session])

  async function submit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError('')
    try {
      await ensureIdentity(displayName)
      const { room } = await (joinRoom ? joinRoom(secret) : rooms.join(preview.slug, secret))
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
        {'permission' in preview && <div className="invite-permission-note">{(preview as RoomInvitePreview).permission === 'viewer' ? <><Eye size={18} /><span><strong>Лише перегляд</strong>Ви зможете дивитися та зберігати файли.</span></> : <><Upload size={18} /><span><strong>Можна додавати</strong>Ви зможете завантажувати свої фото й відео.</span></>}</div>}
        <p>Медіа зберігаються до {new Date(preview.expires_at).toLocaleString('uk-UA', { dateStyle: 'long', timeStyle: 'short' })}.</p>
        {!preview.accepting_members ? (
          <div className="joining-closed"><LockKeyhole size={24} /><div><strong>Кімната закрита для нових учасників</strong><span>Власник тимчасово вимкнув приєднання за посиланням.</span></div></div>
        ) : <form onSubmit={submit}>
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
        </form>}
      </section>
    </main>
  )
}

function ManagedInvitePage() {
  const { token = '' } = useParams()
  const navigate = useNavigate()
  const [preview, setPreview] = useState<RoomInvitePreview | null>(null)
  const [error, setError] = useState('')
  useEffect(() => {
    let active = true
    rooms.invitePreview(token).then((result) => { if (active) setPreview(result) }).catch((cause) => { if (active) setError(errorMessage(cause)) })
    return () => { active = false }
  }, [token])
  if (error) return <main className="status-page"><Brand /><h1>Запрошення недійсне</h1><p>{error}</p><Link className="primary-button" to="/">На головну</Link></main>
  if (!preview) return <main className="status-page"><Brand /><div className="loading-line" /><p>Перевіряємо запрошення…</p></main>
  return <JoinRoom preview={preview} joinRoom={(secret) => rooms.joinInvite(token, secret)} onJoined={(room) => navigate(`/r/${room.slug}`, { replace: true })} />
}

function UploadQueue({ uploads, onCancel, onPause, onRetry, onClearCompleted }: {
  uploads: UploadProgress[]
  onCancel: (id: string) => void
  onPause: (id: string) => void
  onRetry: (id: string) => void
  onClearCompleted: () => void
}) {
  const [collapsed, setCollapsed] = useState(true)
  if (!uploads.length) return null
  const completed = uploads.filter((item) => item.state === 'done').length
  const totalBytes = uploads.reduce((sum, item) => sum + item.size_bytes, 0)
  const uploadedBytes = uploads.reduce((sum, item) => sum + item.size_bytes * item.progress / 100, 0)
  const totalProgress = totalBytes ? Math.round(uploadedBytes / totalBytes * 100) : 0
  return (
    <aside className={`upload-queue ${collapsed ? 'upload-queue--collapsed' : ''}`} aria-live="polite">
      <div className="upload-queue__title">
        <div><Upload size={17} /><strong>Завантаження</strong></div>
        <div className="upload-queue__summary">
          <span>{completed} із {uploads.length} · {totalProgress}%</span>
          {completed > 0 && <button type="button" onClick={onClearCompleted} aria-label="Приховати завершені"><Check size={16} /></button>}
          <button type="button" onClick={() => setCollapsed((value) => !value)} aria-label={collapsed ? 'Розгорнути завантаження' : 'Згорнути завантаження'}>{collapsed ? <ChevronLeft size={17} /> : <X size={17} />}</button>
        </div>
      </div>
      <div className="upload-queue__total-progress" aria-label={`Загальний прогрес ${totalProgress}%`}><span style={{ width: `${totalProgress}%` }} /></div>
      {!collapsed && uploads.map((item) => (
        <div className="upload-row" key={item.id}>
          <div>
            <div><strong>{item.filename}</strong><span>{item.state === 'done' ? item.message ?? 'Готово' : item.state === 'error' ? item.message : item.message ?? `${item.progress}%`}</span></div>
            <div className="upload-actions">
              {item.state === 'uploading' && <button type="button" onClick={() => onPause(item.id)}><Pause size={15} /> Пауза</button>}
              {(item.state === 'paused' || item.state === 'error' || item.state === 'waiting_file') && item.canRetry && <button type="button" onClick={() => onRetry(item.id)}>{item.state === 'paused' ? <Play size={15} /> : <RefreshCw size={15} />} {item.state === 'waiting_file' ? 'Вибрати файл' : item.state === 'paused' ? 'Продовжити' : 'Повторити'}</button>}
              {item.state !== 'done' && <button type="button" onClick={() => onCancel(item.id)}>Скасувати</button>}
            </div>
          </div>
          <div className={`progress ${item.state}`}><span style={{ width: `${item.progress}%` }} /></div>
        </div>
      ))}
    </aside>
  )
}

function GalleryCard({ item, selectionMode, selected, onToggle, onDelete, onOpen, onFavorite, onError }: {
  item: GalleryItem
  selectionMode: boolean
  selected: boolean
  onToggle: (item: GalleryItem) => void
  onDelete: (item: GalleryItem) => void
  onOpen: (item: GalleryItem) => void
  onFavorite: (item: GalleryItem) => Promise<void>
  onError: (message: string) => void
}) {
  const [busy, setBusy] = useState(false)
  const [favoriteBusy, setFavoriteBusy] = useState(false)
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
  async function toggleFavorite() {
    setFavoriteBusy(true)
    try { await onFavorite(item) } finally { setFavoriteBusy(false) }
  }
  return (
    <article className={`media-card ${selected ? 'media-card--selected' : ''}`}>
      <button className="media-preview" type="button" onClick={() => selectionMode ? onToggle(item) : onOpen(item)} aria-label={selectionMode ? `${selected ? 'Зняти вибір із' : 'Вибрати'} ${item.original_filename}` : `Відкрити ${item.original_filename}`}>
        {item.thumbnail_url ? <img src={item.thumbnail_url} alt={item.original_filename} loading="lazy" /> : (
          <div className="media-placeholder">{item.media_type === 'video' ? <Video /> : <FileImage />}<span>{item.thumbnail_status === 'failed' ? 'Прев’ю недоступне' : 'Готуємо прев’ю'}</span></div>
        )}
        {item.media_type === 'video' && <span className="video-badge"><Video size={14} /> {mediaDuration(item.duration_ms) || 'Відео'}</span>}
        {isHEIFItem(item) && <span className="format-badge">HEIC</span>}
        {item.is_cover && <span className="cover-badge"><Crown size={13} /> Обкладинка</span>}
        {selectionMode && <span className={`media-select ${selected ? 'is-selected' : ''}`} aria-hidden="true">{selected && <Check size={16} />}</span>}
      </button>
      {!selectionMode && <button className={`favorite-button ${item.favorited ? 'is-active' : ''}`} type="button" disabled={favoriteBusy} aria-pressed={item.favorited} aria-label={`${item.favorited ? 'Прибрати з обраного' : 'Додати в обране'} ${item.original_filename}`} onClick={() => void toggleFavorite()}><Heart size={17} fill={item.favorited ? 'currentColor' : 'none'} /><span>{item.favorite_count}</span></button>}
      <div className="media-meta">
        <div><strong title={item.caption ?? item.original_filename}>{item.caption || item.original_filename}</strong><span>{item.uploaded_by.display_name} · {bytes(item.size_bytes)}</span></div>
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

function MobileSaveDialog({ slug, roomName, initialItems, onClose, onError }: {
  slug: string
  roomName: string
  initialItems?: GalleryItem[]
  onClose: () => void
  onError: (message: string) => void
}) {
  const [items, setItems] = useState<GalleryItem[]>(initialItems ?? [])
  const [offset, setOffset] = useState(0)
  const [prepared, setPrepared] = useState<File[]>([])
  const [preparing, setPreparing] = useState(false)
  const [progress, setProgress] = useState(0)
  const [loading, setLoading] = useState(!initialItems)
  const [savedOneByOne, setSavedOneByOne] = useState(0)
  const closeRef = useRef<HTMLButtonElement>(null)
  const supportsBatchShare = canShareFiles([new File([''], 'zhyvo.jpg', { type: 'image/jpeg' })])

  useEffect(() => {
    let active = true
    async function loadItems() {
      if (initialItems) return
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
    if (!initialItems) void loadItems()
    closeRef.current?.focus()
    return () => { active = false }
  }, [initialItems, onError, slug])

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
          <div><p className="eyebrow">Оригінальна якість</p><h2 id="mobile-save-title">{initialItems ? 'Зберегти вибране' : 'Зберегти на телефон'}</h2></div>
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

function ShareDialog({ room, webURL: fallbackWebURL, telegramURL: fallbackTelegramURL, previewURL: fallbackPreviewURL, onClose, onCopied }: {
  room: Room
  webURL: string
  telegramURL: string
  previewURL: string
  onClose: () => void
  onCopied: (url: string) => void
}) {
  const closeRef = useRef<HTMLButtonElement>(null)
  const shareNavigator = navigator as Navigator & { share?: (data?: ShareData) => Promise<void> }
  const supportsNativeShare = typeof shareNavigator.share === 'function'
  const [inviteList, setInviteList] = useState<RoomInviteList | null>(null)
  const [permission, setPermission] = useState<'contributor' | 'viewer'>('contributor')
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')
  const selectedInvite = inviteList?.invites.find((invite) => !invite.revoked_at && invite.permission === permission)
  const telegramURL = selectedInvite ? telegramInviteLink(selectedInvite.token) : fallbackTelegramURL
  const previewURL = selectedInvite ? managedInviteLink(selectedInvite.token) : fallbackPreviewURL
  const webURL = selectedInvite ? managedBrowserInviteLink(selectedInvite.token) : fallbackWebURL

  const loadInvites = useCallback(async () => {
    setBusy(true)
    setError('')
    try {
      if (room.role !== 'owner') {
        const invite = await rooms.shareInvite(room.slug)
        setInviteList({ invites: [invite], legacy_invites_enabled: false })
        return
      }
      let result = await rooms.invites(room.slug)
      if (!result.invites.some((invite) => !invite.revoked_at && invite.permission === 'contributor')) {
        const created = await rooms.createInvite(room.slug, 'contributor')
        result = { ...result, invites: [created, ...result.invites] }
      }
      setInviteList(result)
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }, [room.role, room.slug])

  useEffect(() => {
    const previousFocus = document.activeElement as HTMLElement | null
    closeRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKeyDown)
    return () => { document.removeEventListener('keydown', onKeyDown); previousFocus?.focus() }
  }, [onClose])

  useEffect(() => { void loadInvites() }, [loadInvites])

  async function choosePermission(next: 'contributor' | 'viewer') {
    setPermission(next)
    if (room.role !== 'owner' || inviteList?.invites.some((invite) => !invite.revoked_at && invite.permission === next)) return
    setBusy(true)
    setError('')
    try {
      const created = await rooms.createInvite(room.slug, next)
      setInviteList((current) => current ? { ...current, invites: [created, ...current.invites] } : current)
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  async function rotateInvite(invite: RoomInvite) {
    if (!window.confirm('Старе посилання одразу перестане працювати. Створити нове?')) return
    setBusy(true)
    setError('')
    try {
      await rooms.revokeInvite(room.slug, invite.token)
      const created = await rooms.createInvite(room.slug, invite.permission)
      setInviteList((current) => current ? { ...current, invites: [created, ...current.invites.map((item) => item.token === invite.token ? { ...item, revoked_at: new Date().toISOString() } : item)] } : current)
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  async function disableLegacy() {
    if (!window.confirm('Старі посилання з кодом кімнати перестануть працювати для нових учасників. Продовжити?')) return
    setBusy(true)
    try {
      await rooms.disableLegacyInvite(room.slug)
      setInviteList((current) => current ? { ...current, legacy_invites_enabled: false } : current)
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  async function nativeShare() {
    if (!shareNavigator.share) return
    try { await shareNavigator.share({ title: room.name, text: `Приєднуйтеся до кімнати «${room.name}» у Zhyvo`, url: previewURL }) } catch { /* User cancelled the share sheet. */ }
  }

  return (
    <div className="dialog-backdrop" role="presentation" onMouseDown={onClose}>
      <section className="share-dialog" role="dialog" aria-modal="true" aria-labelledby="share-title" onMouseDown={(event) => event.stopPropagation()}>
        <header><div><p className="eyebrow">Кімната {room.slug}</p><h2 id="share-title">Запросити друзів</h2></div><button ref={closeRef} className="icon-button" onClick={onClose} aria-label="Закрити"><X /></button></header>
        {room.role === 'owner' && <div className="invite-permission-tabs" role="tablist" aria-label="Права за посиланням"><button role="tab" aria-selected={permission === 'contributor'} onClick={() => void choosePermission('contributor')}><Upload size={17} /> Можуть додавати</button><button role="tab" aria-selected={permission === 'viewer'} onClick={() => void choosePermission('viewer')}><Eye size={17} /> Лише перегляд</button></div>}
        <div className="qr-wrap">{busy && !selectedInvite ? <div className="invite-loading" aria-label="Створюємо захищене посилання"><span /><span /></div> : <QRCodeSVG value={telegramURL} size={224} level="M" title={`QR-код кімнати ${room.slug}`} data-invite-url={telegramURL} />}</div>
        <p className="share-note">{permission === 'viewer' && room.role === 'owner' ? 'За цим QR можна переглядати та зберігати файли, але не завантажувати нові.' : 'QR-код відкриє кімнату прямо в Telegram і дозволить додавати медіа.'} {room.access_mode !== 'public' && 'PIN чи пароль передайте окремо.'}</p>
        {error && <p className="form-error invite-error" role="alert">{error}</p>}
        <div className="share-actions">
          <button className="primary-button share-actions__telegram" disabled={busy} onClick={() => openTelegramInvite(room.name, previewURL)}><Send size={18} /> Надіслати в Telegram</button>
          <button className="secondary-button" disabled={busy} onClick={() => onCopied(previewURL)}><Copy size={18} /> Копіювати запрошення</button>
          {supportsNativeShare && <button className="secondary-button" disabled={busy} onClick={nativeShare}><Share2 size={18} /> Інші застосунки</button>}
        </div>
        <a className="browser-invite-link" href={webURL}>Відкрити кімнату у браузері</a>
        {room.role === 'owner' && selectedInvite && <div className="invite-management"><div><strong>{permission === 'viewer' ? 'Посилання лише для перегляду' : 'Посилання для учасників'}</strong><span>Використано входів: {selectedInvite.join_count}</span></div><button disabled={busy} onClick={() => void rotateInvite(selectedInvite)}><RefreshCw size={16} /> Замінити</button></div>}
        {room.role === 'owner' && inviteList?.legacy_invites_enabled && <button className="legacy-invite-button" disabled={busy} onClick={() => void disableLegacy()}><LockKeyhole size={16} /> Вимкнути старе посилання з кодом {room.slug}</button>}
      </section>
    </div>
  )
}

function MediaViewer({ item, items, canSetCover, onSelect, onClose, onFavorite, onSetCover, onCaption, onError }: {
  item: GalleryItem
  items: GalleryItem[]
  canSetCover: boolean
  onSelect: (item: GalleryItem) => void
  onClose: () => void
  onFavorite: (item: GalleryItem) => Promise<void>
  onSetCover: (item: GalleryItem, enabled: boolean) => Promise<void>
  onCaption: (item: GalleryItem, caption: string) => Promise<void>
  onError: (message: string) => void
}) {
  const closeRef = useRef<HTMLButtonElement>(null)
  const [source, setSource] = useState<{ url: string; filename: string } | null>(null)
  const [loading, setLoading] = useState(true)
  const [favoriteBusy, setFavoriteBusy] = useState(false)
  const [coverBusy, setCoverBusy] = useState(false)
  const [infoOpen, setInfoOpen] = useState(false)
  const [captionEditing, setCaptionEditing] = useState(false)
  const [captionDraft, setCaptionDraft] = useState(item.caption ?? '')
  const [captionBusy, setCaptionBusy] = useState(false)
  const [captionError, setCaptionError] = useState('')
  const index = items.findIndex((candidate) => candidate.id === item.id)
  const previous = index > 0 ? items[index - 1] : null
  const next = index >= 0 && index < items.length - 1 ? items[index + 1] : null
  const heifPreview = isHEIFItem(item)
  const format = item.mime_type.split('/').at(-1)?.toLocaleUpperCase('uk-UA') ?? item.mime_type
  const dimensions = item.width && item.height ? `${item.width} × ${item.height}` : null

  useEffect(() => {
    setCaptionDraft(item.caption ?? '')
    setCaptionEditing(false)
    setCaptionError('')
  }, [item.caption, item.id])

  useEffect(() => setInfoOpen(false), [item.id])

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

  async function saveCaption(event: FormEvent) {
    event.preventDefault()
    const normalized = captionDraft.trim()
    if (normalized === (item.caption ?? '')) {
      setCaptionEditing(false)
      return
    }
    setCaptionBusy(true)
    setCaptionError('')
    try {
      await onCaption(item, normalized)
      setCaptionEditing(false)
    } catch (cause) {
      setCaptionError(errorMessage(cause))
    } finally {
      setCaptionBusy(false)
    }
  }

  return (
    <div className="viewer" role="dialog" aria-modal="true" aria-labelledby="viewer-title">
      <header className="viewer-topbar">
        <div><strong id="viewer-title">{item.original_filename}</strong><span>{item.uploaded_by.display_name} · {bytes(item.size_bytes)}</span></div>
        <div className="viewer-actions">
          <button className={`viewer-favorite ${item.favorited ? 'is-active' : ''}`} disabled={favoriteBusy} aria-pressed={item.favorited} aria-label={item.favorited ? 'Прибрати з обраного' : 'Додати в обране'} onClick={() => { setFavoriteBusy(true); void onFavorite(item).finally(() => setFavoriteBusy(false)) }}><Heart size={19} fill={item.favorited ? 'currentColor' : 'none'} /><span>{item.favorite_count}</span></button>
          <button className={`icon-button ${infoOpen ? 'is-active' : ''}`} aria-pressed={infoOpen} aria-label={infoOpen ? 'Сховати інформацію про файл' : 'Показати інформацію про файл'} onClick={() => setInfoOpen((value) => !value)}><Info /></button>
          {canSetCover && item.media_type === 'image' && <button className={`icon-button ${item.is_cover ? 'is-active' : ''}`} disabled={coverBusy} aria-label={item.is_cover ? 'Прибрати обкладинку кімнати' : 'Зробити фото обкладинкою кімнати'} onClick={() => {
            if (!window.confirm(item.is_cover ? 'Прибрати це фото з обкладинки кімнати?' : 'Зробити це фото обкладинкою кімнати?')) return
            setCoverBusy(true)
            void onSetCover(item, !item.is_cover).finally(() => setCoverBusy(false))
          }}><Images /></button>}
          {source && <button className="icon-button" onClick={() => void saveRemoteFile({ ...source, mimeType: item.mime_type }).catch((cause) => onError(errorMessage(cause)))} aria-label="Завантажити оригінал"><ArrowDownToLine /></button>}
          <button ref={closeRef} className="icon-button" onClick={onClose} aria-label="Закрити перегляд"><X /></button>
        </div>
      </header>
      <div className="viewer-stage">
        {loading && <div className="viewer-loading" role="status">Завантажуємо оригінал…</div>}
        {!loading && source && (item.media_type === 'video'
          ? <video key={source.url} src={source.url} controls playsInline autoPlay aria-label={item.original_filename} />
          : heifPreview
            ? item.thumbnail_url
              ? <img src={item.thumbnail_url} alt={item.original_filename} />
              : <div className="viewer-loading" role="status">Готуємо сумісне прев’ю HEIC…</div>
            : <img src={source.url} alt={item.original_filename} />)}
        {!loading && heifPreview && <div className="viewer-format-note">Показуємо оптимізоване прев’ю HEIC. Оригінал доступний кнопкою завантаження.</div>}
        {previous && <button className="viewer-nav viewer-nav--previous" onClick={() => onSelect(previous)} aria-label="Попередній файл"><ChevronLeft /></button>}
        {next && <button className="viewer-nav viewer-nav--next" onClick={() => onSelect(next)} aria-label="Наступний файл"><ChevronRight /></button>}
      </div>
      {infoOpen && <footer className="viewer-info">
        <div className="viewer-caption">
          {captionEditing ? (
            <form onSubmit={(event) => void saveCaption(event)}>
              <label htmlFor={`caption-${item.id}`}>Підпис</label>
              <textarea id={`caption-${item.id}`} autoFocus maxLength={300} rows={2} value={captionDraft} onChange={(event) => setCaptionDraft(event.target.value)} placeholder="Що відбувається на цьому кадрі?" />
              <div><span>{captionDraft.length} / 300</span><button type="button" onClick={() => { setCaptionDraft(item.caption ?? ''); setCaptionEditing(false); setCaptionError('') }}>Скасувати</button><button type="submit" disabled={captionBusy}>{captionBusy ? 'Зберігаємо…' : 'Зберегти'}</button></div>
              {captionError && <p role="alert">{captionError}</p>}
            </form>
          ) : (
            <div>
              <p>{item.caption || 'Підпис не додано'}</p>
              {item.permissions.can_edit_caption && <button onClick={() => setCaptionEditing(true)}><Pencil size={15} /> {item.caption ? 'Редагувати' : 'Додати підпис'}</button>}
            </div>
          )}
        </div>
        <dl className="viewer-details">
          <div><dt>Автор</dt><dd>{item.uploaded_by.display_name}</dd></div>
          {item.captured_at && <div><dt>Знято</dt><dd>{new Date(item.captured_at).toLocaleString('uk-UA', { dateStyle: 'medium', timeStyle: 'short' })}</dd></div>}
          <div><dt>Завантажено</dt><dd>{new Date(item.created_at).toLocaleString('uk-UA', { dateStyle: 'medium', timeStyle: 'short' })}</dd></div>
          <div><dt>{item.media_type === 'video' ? 'Тривалість' : 'Роздільність'}</dt><dd>{item.media_type === 'video' ? mediaDuration(item.duration_ms) || 'Невідомо' : dimensions || 'Невідомо'}</dd></div>
          <div><dt>Файл</dt><dd>{format} · {bytes(item.size_bytes)}</dd></div>
          <div className="viewer-details__index"><dt>Кадр</dt><dd>{index + 1} / {items.length}</dd></div>
        </dl>
      </footer>}
    </div>
  )
}

function RecapPage() {
  const { slug: routeSlug = '' } = useParams()
  const slug = normalizeSlug(routeSlug)
  const [room, setRoom] = useState<Room | null>(null)
  const [recap, setRecap] = useState<RoomRecap | null>(null)
  const [highlights, setHighlights] = useState<GalleryItem[]>([])
  const [archive, setArchive] = useState<RoomArchive | null>(null)
  const [mobileSaveOpen, setMobileSaveOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    Promise.all([rooms.get(slug), rooms.recap(slug), rooms.highlights(slug)]).then(([roomResult, recapResult, highlightResult]) => {
      if (!active) return
      setRoom(roomResult.room)
      setRecap(recapResult)
      setHighlights(highlightResult.items)
    }).catch((cause) => { if (active) setError(errorMessage(cause)) })
    return () => { active = false }
  }, [slug])

  useEffect(() => {
    if (!archive || !['pending', 'processing'].includes(archive.status)) return
    const timer = window.setInterval(() => void archives.get(archive.id).then((result) => setArchive(result.archive)).catch((cause) => setError(errorMessage(cause))), 1500)
    return () => window.clearInterval(timer)
  }, [archive])

  async function saveAll() {
    if (!room || busy) return
    if (isMobileDevice()) { setMobileSaveOpen(true); return }
    setBusy(true)
    setError('')
    try {
      let current = archive
      if (!current || current.status === 'failed') {
        current = (await archives.request(slug)).archive
        setArchive(current)
      }
      if (current.status === 'ready') {
        const download = await archives.download(current.id)
        const anchor = document.createElement('a')
        anchor.href = download.url
        anchor.download = download.filename
        anchor.rel = 'noopener'
        anchor.click()
      }
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  if (error && !room) return <main className="status-page"><Brand /><h1>Не вдалося відкрити підсумок</h1><p>{error}</p><Link className="primary-button" to={`/r/${slug}`}>До кімнати</Link></main>
  if (!room || !recap) return <main className="status-page"><Brand /><div className="loading-line" /><p>Збираємо підсумок події…</p></main>
  const archiveLabel = archive?.status === 'ready' ? 'Завантажити ZIP' : archive?.status === 'failed' ? 'Повторити ZIP' : archive ? `${archive.processed_files} із ${archive.total_files}` : isMobileDevice() ? 'Зберегти на телефон' : 'Підготувати ZIP'
  return (
    <main className="recap-shell">
      <header className="recap-topbar"><Brand compact /><Link to={`/r/${slug}`}><ArrowLeft size={18} /> До галереї</Link></header>
      <section className="recap-hero">
        <p className="eyebrow">Підсумок події</p>
        <h1>{room.name}</h1>
        <p>{new Date(recap.created_at).toLocaleDateString('uk-UA', { dateStyle: 'long' })} · зберігається до {new Date(recap.expires_at).toLocaleString('uk-UA', { dateStyle: 'long', timeStyle: 'short' })}</p>
      </section>
      <section className="recap-stats" aria-label="Статистика події">
        <div><strong>{recap.media_count}</strong><span>файлів</span></div>
        <div><strong>{recap.member_count}</strong><span>учасників</span></div>
        <div><strong>{recap.contributor_count}</strong><span>авторів</span></div>
        <div><strong>{recap.favorite_count}</strong><span>вподобань</span></div>
      </section>
      <section className="recap-breakdown"><span><Images size={18} /> {recap.image_count} фото</span><span><Video size={18} /> {recap.video_count} відео</span><span><Archive size={18} /> {bytes(recap.total_bytes)}</span></section>
      <section className="recap-highlights">
        <header><div><p className="eyebrow">Вибір учасників</p><h2>Найкращі моменти</h2></div><span>{highlights.length}</span></header>
        {highlights.length ? <div className="recap-grid">{highlights.map((item, index) => <Link to={`/r/${slug}?media=${item.id}`} className="recap-frame" key={item.id}><span>{String(index + 1).padStart(2, '0')}</span>{item.thumbnail_url ? <img src={item.thumbnail_url} alt={item.caption || item.original_filename} /> : <div><FileImage /></div>}<small><Heart size={13} fill={item.favorite_count ? 'currentColor' : 'none'} /> {item.favorite_count}</small></Link>)}</div> : <div className="recap-empty"><Images /><p>Найкращі моменти з’являться після перших завантажень.</p></div>}
      </section>
      <section className="recap-save"><div><p className="eyebrow">До автовидалення</p><h2>Збережіть оригінали</h2><p>Після завершення строку відновити файли буде неможливо.</p></div><button className="primary-button" disabled={busy || archive?.status === 'pending' || archive?.status === 'processing'} onClick={() => void saveAll()}><ArrowDownToLine size={19} /> {archiveLabel}</button></section>
      {error && <p className="form-error recap-error" role="alert">{error}</p>}
      {mobileSaveOpen && <MobileSaveDialog slug={slug} roomName={room.name} onClose={() => setMobileSaveOpen(false)} onError={setError} />}
    </main>
  )
}

function RoomPage() {
  const { slug: routeSlug = '' } = useParams()
  const slug = normalizeSlug(routeSlug)
  const navigate = useNavigate()
  const location = useLocation()
  const session = useSession()
  const [searchParams, setSearchParams] = useSearchParams()
  const inputRef = useRef<HTMLInputElement>(null)
  const resumeInputRef = useRef<HTMLInputElement>(null)
  const resumeTargetRef = useRef<string | null>(null)
  const uploadFilesRef = useRef(new Map<string, File>())
  const uploadControllersRef = useRef(new Map<string, AbortController>())
  const pausedUploadsRef = useRef(new Set<string>())
  const uploadsRef = useRef<UploadProgress[]>([])
  const uploadsHydratedKeyRef = useRef('')
  const galleryRefreshRef = useRef(false)
  const galleryRefreshPendingRef = useRef(false)
  const galleryRef = useRef<GalleryItem[]>([])
  const galleryAnchorRef = useRef<HTMLElement>(null)
  const announcedMediaRef = useRef(new Set<string>())
  const roomRef = useRef<Room | null>(null)
  const membersOpenRef = useRef(false)
  const membersTabRef = useRef<'members' | 'activity'>('members')
  const loadedPastFirstPageRef = useRef(false)
  const settingsCloseRef = useRef<HTMLButtonElement>(null)
  const membersCloseRef = useRef<HTMLButtonElement>(null)
  const [room, setRoom] = useState<Room | null>(null)
  const [preview, setPreview] = useState<RoomPreview | null>(null)
  const [gallery, setGallery] = useState<GalleryItem[]>([])
  const [galleryFilter, setGalleryFilter] = useState<GalleryFilter>('all')
  const [selectionMode, setSelectionMode] = useState(false)
  const [selectedMediaIDs, setSelectedMediaIDs] = useState<Set<string>>(new Set())
  const [batchBusy, setBatchBusy] = useState(false)
  const [cursor, setCursor] = useState<string | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [uploads, setUploads] = useState<UploadProgress[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)
  const [settings, setSettings] = useState(false)
  const [settingsName, setSettingsName] = useState('')
  const [settingsAccessMode, setSettingsAccessMode] = useState<AccessMode>('public')
  const [settingsSecret, setSettingsSecret] = useState('')
  const [settingsAccessDirty, setSettingsAccessDirty] = useState(false)
  const [settingsLifetime, setSettingsLifetime] = useState(1)
  const [settingsBusy, setSettingsBusy] = useState(false)
  const [settingsError, setSettingsError] = useState('')
  const [notificationSettings, setNotificationSettings] = useState<RoomNotificationSettings | null>(null)
  const [notificationBusy, setNotificationBusy] = useState(false)
  const [shareDialog, setShareDialog] = useState(false)
  const [membersOpen, setMembersOpen] = useState(false)
  const [members, setMembers] = useState<RoomMember[]>([])
  const [blockedMembers, setBlockedMembers] = useState<BlockedRoomMember[]>([])
  const [activity, setActivity] = useState<RoomActivityEvent[]>([])
  const [roomArchive, setRoomArchive] = useState<RoomArchive | null>(null)
  const [archiveBusy, setArchiveBusy] = useState(false)
  const [mobileSaveOpen, setMobileSaveOpen] = useState(false)
  const [mobileSaveItems, setMobileSaveItems] = useState<GalleryItem[] | undefined>()
  const [membersTab, setMembersTab] = useState<'members' | 'activity'>('members')
  const [memberActionID, setMemberActionID] = useState<string | null>(null)
  const [membersLoading, setMembersLoading] = useState(false)
  const [membersError, setMembersError] = useState('')
  const [realtimeConnected, setRealtimeConnected] = useState(false)
  const [newMediaCount, setNewMediaCount] = useState(0)
  const [activationVisible, setActivationVisible] = useState(Boolean((location.state as { justCreated?: boolean } | null)?.justCreated))
  const [activationShared, setActivationShared] = useState(false)
  const [clockNow, setClockNow] = useState(Date.now())
  const selectedMediaID = searchParams.get('media')
  const roomLoaded = room !== null

  useEffect(() => { uploadsRef.current = uploads }, [uploads])
  useEffect(() => { galleryRef.current = gallery }, [gallery])
  useEffect(() => { roomRef.current = room }, [room])
  useEffect(() => { membersOpenRef.current = membersOpen }, [membersOpen])
  useEffect(() => { membersTabRef.current = membersTab }, [membersTab])

  useEffect(() => {
    if (!(location.state as { justCreated?: boolean } | null)?.justCreated) return
    navigate(`${location.pathname}${location.search}`, { replace: true, state: null })
  }, [location.pathname, location.search, location.state, navigate])

  useEffect(() => {
    if (gallery.length > 0) setActivationVisible(false)
  }, [gallery.length])

  useEffect(() => {
    if (!room) return
    const timer = window.setInterval(() => setClockNow(Date.now()), 30_000)
    return () => window.clearInterval(timer)
  }, [room])

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
    setGallery((current) => sortGallery(append ? [...current, ...page.items] : page.items))
    loadedPastFirstPageRef.current = append
    setCursor(page.next_cursor)
    setHasMore(page.has_more)
  }, [slug])

  const refreshGallery = useCallback(async () => {
    if (galleryRefreshRef.current) {
      galleryRefreshPendingRef.current = true
      return
    }
    galleryRefreshRef.current = true
    try {
      do {
        galleryRefreshPendingRef.current = false
        const page = await media.gallery(slug)
        setGallery((current) => {
          const freshIDs = new Set(page.items.map((item) => item.id))
          if (current.length <= 50) return sortGallery(page.items)
          return sortGallery([...page.items, ...current.slice(50).filter((item) => !freshIDs.has(item.id))])
        })
        if (!loadedPastFirstPageRef.current) {
          setCursor(page.next_cursor)
          setHasMore(page.has_more)
        }
      } while (galleryRefreshPendingRef.current)
    } finally {
      galleryRefreshRef.current = false
    }
  }, [slug])

  const refreshRoom = useCallback(async () => {
    const result = await rooms.get(slug)
    setRoom(result.room)
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
    if (!session || !roomLoaded) return
    const controller = new AbortController()
    void streamRoomEvents(slug, {
      onConnected: () => setRealtimeConnected(true),
      onDisconnected: () => setRealtimeConnected(false),
      onAccessRevoked: () => navigate('/', { replace: true }),
      onEvent: (event) => {
        if (event.type === 'media_ready' && event.entity_id && !galleryRef.current.some((item) => item.id === event.entity_id)) {
          const pastGalleryHeading = (galleryAnchorRef.current?.getBoundingClientRect().top ?? 1) < 0
          if (pastGalleryHeading && !announcedMediaRef.current.has(event.entity_id)) {
            announcedMediaRef.current.add(event.entity_id)
            setNewMediaCount((count) => count + 1)
          }
        }
        if (['media_ready', 'media_updated', 'media_deleted', 'favorite_changed'].includes(event.type)) {
          void refreshGallery().catch(() => undefined)
        }
        if (['media_ready', 'media_deleted', 'room_updated', 'members_changed'].includes(event.type)) {
          void refreshRoom().catch(() => undefined)
        }
        if (event.type === 'members_changed' && roomRef.current?.role === 'owner' && membersOpenRef.current) {
          void Promise.all([
            rooms.members(slug),
            membersTabRef.current === 'activity' ? rooms.activity(slug) : Promise.resolve(null),
          ]).then(([memberResult, activityResult]) => {
            setMembers(memberResult.members)
            setBlockedMembers(memberResult.blocked_members)
            if (activityResult) setActivity(activityResult.events)
          }).catch(() => undefined)
        }
      },
    }, controller.signal)
    return () => {
      controller.abort()
      setRealtimeConnected(false)
    }
  }, [navigate, refreshGallery, refreshRoom, roomLoaded, session, slug])

  useEffect(() => {
    if (!room) return
    const interval = window.setInterval(() => {
      if (document.visibilityState === 'visible') void refreshGallery().catch(() => undefined)
    }, realtimeConnected ? 60_000 : 8000)
    return () => window.clearInterval(interval)
  }, [realtimeConnected, refreshGallery, room])

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
    if (room) {
      setSettingsName(room.name)
      setSettingsAccessMode(room.access_mode)
      setSettingsSecret('')
      setSettingsAccessDirty(false)
      setSettingsLifetime(Math.max(1, Math.min(3, Math.round((new Date(room.expires_at).getTime() - new Date(room.created_at).getTime()) / (24 * 60 * 60 * 1000)))))
      setSettingsError('')
    }
    const previousFocus = document.activeElement as HTMLElement | null
    settingsCloseRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => { if (event.key === 'Escape') setSettings(false) }
    document.addEventListener('keydown', onKeyDown)
    return () => { document.removeEventListener('keydown', onKeyDown); previousFocus?.focus() }
  }, [room, settings])

  useEffect(() => {
    if (!settings || !room) return
    let active = true
    rooms.notifications(slug).then((result) => { if (active) setNotificationSettings(result) }).catch((cause) => {
      if (active) setSettingsError(errorMessage(cause))
    })
    return () => { active = false }
  }, [room, settings, slug])

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

  const processUpload = useCallback(async (queueID: string, file: File, initialItem?: UploadProgress, verifyStoredChecksum = false) => {
    const controller = uploadControllersRef.current.get(queueID) ?? new AbortController()
    uploadControllersRef.current.set(queueID, controller)
    if (controller.signal.aborted) return
    const queueItem = initialItem ?? uploadsRef.current.find((item) => item.id === queueID)
    if (!queueItem) return
    setUploads((current) => current.map((item) => item.id === queueID ? { ...item, state: 'uploading', message: undefined, canRetry: false } : item))
    try {
      let checksum = queueItem.checksum
      if ((!checksum || verifyStoredChecksum) && !(shouldAvoidPreReadingMobileFile() && !verifyStoredChecksum)) {
        setUploads((current) => current.map((item) => item.id === queueID ? { ...item, message: 'Перевіряємо файл', progress: 0 } : item))
        try {
          const calculated = await checksumFile(file, controller.signal, (progress) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, progress: Math.max(1, Math.round(progress / 10)) } : item)))
          if (verifyStoredChecksum && checksum && calculated !== checksum) throw new Error('Це інший файл — його вміст не збігається')
          checksum = calculated
          setUploads((current) => current.map((item) => item.id === queueID ? { ...item, checksum } : item))
        } catch (cause) {
          if (verifyStoredChecksum || controller.signal.aborted) throw cause
          checksum = undefined
          setUploads((current) => current.map((item) => item.id === queueID ? { ...item, progress: 0, message: 'Передаємо без попередньої перевірки' } : item))
        }
      }
      await uploadFile(slug, file, {
        signal: controller.signal,
        idempotencyKey: queueItem.idempotency_key,
        mimeType: queueItem.mime_type,
        capturedAt: queueItem.captured_at,
        checksum,
        completedParts: queueItem.completed_parts,
        onProgress: (progress) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, progress, message: undefined } : item)),
        onStatus: (message) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, message } : item)),
        onCheckpoint: ({ uploadID, completedParts }) => setUploads((current) => current.map((item) => item.id === queueID ? { ...item, upload_id: uploadID, completed_parts: completedParts } : item)),
        shouldPreserveOnAbort: () => pausedUploadsRef.current.has(queueID),
      })
      setUploads((current) => current.map((item) => item.id === queueID ? { ...item, state: 'done', progress: 100, message: undefined } : item))
      setRoomArchive(null)
      uploadFilesRef.current.delete(queueID)
      uploadControllersRef.current.delete(queueID)
    } catch (cause) {
      const cancelled = controller.signal.aborted
      const paused = cancelled && pausedUploadsRef.current.has(queueID)
      const sessionExpired = cause instanceof ApiError && ['UPLOAD_EXPIRED', 'UPLOAD_NOT_FOUND'].includes(cause.code)
      const duplicate = cause instanceof ApiError && cause.code === 'MEDIA_DUPLICATE'
      setUploads((current) => current.map((item) => item.id === queueID ? {
        ...item,
        state: duplicate ? 'done' : paused ? 'paused' : 'error',
        progress: duplicate ? 100 : item.progress,
        message: duplicate ? 'Вже є в галереї' : paused ? 'Призупинено' : cancelled ? 'Скасовано' : sessionExpired ? 'Сесія завершилася — почнемо файл заново' : errorMessage(cause),
        canRetry: duplicate ? false : paused || !cancelled,
        ...(sessionExpired ? { idempotency_key: uuid(), upload_id: undefined, completed_parts: [] } : {}),
      } : item))
      if (duplicate) uploadFilesRef.current.delete(queueID)
      uploadControllersRef.current.delete(queueID)
    }
  }, [slug])

  const openMedia = useCallback((item: GalleryItem) => setSearchParams({ media: item.id }), [setSearchParams])
  const closeMedia = useCallback(() => setSearchParams({}, { replace: true }), [setSearchParams])

  async function acceptFiles(files: FileList | null) {
    if (!files?.length || !room) return
    const incoming = await stabilizeAndroidGalleryFiles(Array.from(files))
    const existingFingerprints = new Set(uploadsRef.current.map((item) => `${item.filename}:${item.size_bytes}`))
    const seen = new Set(existingFingerprints)
    const rejected: UploadProgress[] = []
    const accepted = incoming.filter((file) => {
      const mimeType = uploadMimeType(file)
      const fingerprint = `${file.name}:${file.size}`
      const message = mimeType.startsWith('image/') && file.size > 50 * 1024 * 1024
        ? 'Фото перевищує ліміт 50 МБ'
        : file.size > 2 * 1024 * 1024 * 1024
        ? 'Файл перевищує ліміт 2 ГБ'
        : !mimeType.startsWith('image/') && !mimeType.startsWith('video/')
          ? 'Підтримуються лише фото та відео'
          : seen.has(fingerprint) ? 'Цей файл уже додано до черги' : ''
      if (!message) { seen.add(fingerprint); return true }
      rejected.push({ id: uuid(), filename: file.name, size_bytes: file.size, mime_type: file.type, last_modified: file.lastModified, idempotency_key: uuid(), created_at: new Date().toISOString(), progress: 0, state: 'error', message, canRetry: false })
      return false
    })
    const capturedDates = shouldAvoidPreReadingMobileFile() ? accepted.map(() => null) : await Promise.all(accepted.map(mediaCapturedAt))
    const queue: UploadProgress[] = accepted.map((file, index) => ({
      id: uuid(),
      filename: file.name,
      size_bytes: file.size,
      mime_type: uploadMimeType(file),
      last_modified: file.lastModified,
      idempotency_key: uuid(),
      created_at: new Date().toISOString(),
      progress: 0,
      state: 'queued',
      captured_at: capturedDates[index],
    }))
    queue.forEach((item, index) => {
      uploadFilesRef.current.set(item.id, accepted[index])
      uploadControllersRef.current.set(item.id, new AbortController())
    })
    setUploads((current) => [...current.filter((item) => item.state !== 'done'), ...queue, ...rejected])
    if (!queue.length) { if (inputRef.current) inputRef.current.value = ''; return }
    setActivationVisible(false)
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
    pausedUploadsRef.current.delete(id)
    const item = uploadsRef.current.find((candidate) => candidate.id === id)
    uploadControllersRef.current.get(id)?.abort()
    if (item?.state === 'paused' && item.upload_id) void media.abort(item.upload_id).catch(() => undefined)
    uploadFilesRef.current.delete(id)
    setUploads((current) => current.map((item) => item.id === id ? { ...item, state: 'error', message: 'Скасовано', canRetry: false } : item))
    window.setTimeout(() => setUploads((current) => current.filter((item) => item.id !== id)), 1800)
  }

  function pauseUpload(id: string) {
    pausedUploadsRef.current.add(id)
    uploadControllersRef.current.get(id)?.abort()
  }

  function retryUpload(id: string) {
    const file = uploadFilesRef.current.get(id)
    if (!file) {
      resumeTargetRef.current = id
      resumeInputRef.current?.click()
      return
    }
    const wasPaused = uploadsRef.current.find((item) => item.id === id)?.state === 'paused'
    pausedUploadsRef.current.delete(id)
    uploadControllersRef.current.set(id, new AbortController())
    setUploads((current) => current.map((item) => item.id === id ? { ...item, progress: wasPaused ? item.progress : 0, state: 'queued', message: undefined, canRetry: false } : item))
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
    void processUpload(id, file, resumed, true).then(async () => {
      await refreshGallery().catch(() => undefined)
      const refreshedRoom = await rooms.get(slug).catch(() => null)
      if (refreshedRoom) setRoom(refreshedRoom.room)
    })
  }

  async function copyLink(url = roomInviteLink(slug)) {
    await navigator.clipboard.writeText(url)
    setActivationShared(true)
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

  async function toggleMembers() {
    if (!room) return
    try {
      const result = await rooms.update(slug, { accepting_members: !room.accepting_members })
      setRoom(result.room)
    } catch (cause) { setSettingsError(errorMessage(cause)) }
  }

  async function saveRoomSettings() {
    if (!room || settingsBusy) return
    const trimmedName = settingsName.trim()
    if (!trimmedName) { setSettingsError('Вкажіть назву кімнати'); return }
    if (settingsAccessDirty && settingsAccessMode !== 'public') {
      const valid = settingsAccessMode === 'pin' ? /^\d{4,8}$/.test(settingsSecret) : settingsSecret.length >= 6
      if (!valid) { setSettingsError(settingsAccessMode === 'pin' ? 'PIN має містити 4–8 цифр' : 'Пароль має містити щонайменше 6 символів'); return }
    }
    const currentLifetime = Math.max(1, Math.min(3, Math.round((new Date(room.expires_at).getTime() - new Date(room.created_at).getTime()) / (24 * 60 * 60 * 1000))))
    const input: Partial<{ name: string; access: { mode: AccessMode; secret: string }; lifetime_days: number }> = {}
    if (trimmedName !== room.name) input.name = trimmedName
    if (settingsAccessDirty) input.access = { mode: settingsAccessMode, secret: settingsAccessMode === 'public' ? '' : settingsSecret }
    if (settingsLifetime > currentLifetime) input.lifetime_days = settingsLifetime
    if (!Object.keys(input).length) { setSettings(false); return }
    setSettingsBusy(true)
    setSettingsError('')
    try {
      const result = await rooms.update(slug, input)
      setRoom(result.room)
      setSettings(false)
    } catch (cause) {
      setSettingsError(errorMessage(cause))
    } finally {
      setSettingsBusy(false)
    }
  }

  async function toggleNotifications() {
    if (!notificationSettings || notificationBusy) return
    setNotificationBusy(true)
    setSettingsError('')
    try {
      const result = await rooms.updateNotifications(slug, { telegram_enabled: !notificationSettings.telegram_enabled })
      setNotificationSettings(result)
    } catch (cause) {
      setSettingsError(errorMessage(cause))
    } finally {
      setNotificationBusy(false)
    }
  }

  async function updateNotificationPreference(input: Partial<Pick<RoomNotificationSettings, 'new_media_enabled' | 'expiry_enabled' | 'member_joined_enabled'>>) {
    if (!notificationSettings || notificationBusy) return
    setNotificationBusy(true)
    setSettingsError('')
    try {
      setNotificationSettings(await rooms.updateNotifications(slug, input))
    } catch (cause) {
      setSettingsError(errorMessage(cause))
    } finally {
      setNotificationBusy(false)
    }
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

  function toggleMediaSelection(item: GalleryItem) {
    setSelectedMediaIDs((current) => {
      const next = new Set(current)
      if (next.has(item.id)) next.delete(item.id)
      else next.add(item.id)
      return next
    })
  }

  function closeSelection() {
    setSelectionMode(false)
    setSelectedMediaIDs(new Set())
  }

  async function saveSelected(items: GalleryItem[]) {
    if (!items.length) return
    if (isMobileDevice()) {
      setMobileSaveItems(items)
      setMobileSaveOpen(true)
      return
    }
    setBatchBusy(true)
    setError('')
    try {
      for (const item of items) {
        const remote = await media.download(item.id)
        await saveRemoteFile({ ...remote, mimeType: item.mime_type })
      }
      closeSelection()
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBatchBusy(false)
    }
  }

  async function deleteSelected(items: GalleryItem[]) {
    const deletable = items.filter((item) => item.permissions.can_delete)
    if (!deletable.length || !window.confirm(`Видалити ${deletable.length} вибраних ${deletable.length === 1 ? 'файл' : 'файлів'} назавжди?`)) return
    setBatchBusy(true)
    setError('')
    try {
      for (const item of deletable) await media.remove(item.id)
      const deletedIDs = new Set(deletable.map((item) => item.id))
      setGallery((current) => current.filter((item) => !deletedIDs.has(item.id)))
      setRoom((current) => current ? {
        ...current,
        used_files: Math.max(0, current.used_files - deletable.length),
        used_storage_bytes: Math.max(0, current.used_storage_bytes - deletable.reduce((sum, item) => sum + item.size_bytes, 0)),
      } : current)
      setRoomArchive(null)
      closeSelection()
    } catch (cause) {
      setError(errorMessage(cause))
      await refreshGallery().catch(() => undefined)
    } finally {
      setBatchBusy(false)
    }
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

  async function toggleFavorite(item: GalleryItem) {
    setError('')
    try {
      const result = await media.favorite(item.id, !item.favorited)
      setGallery((current) => current.map((candidate) => candidate.id === item.id
        ? { ...candidate, favorite_count: result.favorite_count, favorited: result.favorited }
        : candidate))
    } catch (cause) {
      setError(errorMessage(cause))
      throw cause
    }
  }

  async function setRoomCover(item: GalleryItem, enabled: boolean) {
    setError('')
    try {
      if (enabled) await media.setCover(slug, item.id)
      else await media.clearCover(slug)
      setGallery((current) => current.map((candidate) => ({ ...candidate, is_cover: enabled && candidate.id === item.id })))
    } catch (cause) {
      setError(errorMessage(cause))
      throw cause
    }
  }

  async function updateCaption(item: GalleryItem, caption: string) {
    setError('')
    try {
      const result = await media.updateCaption(item.id, caption)
      setGallery((current) => current.map((candidate) => candidate.id === item.id
        ? { ...candidate, caption: result.caption, caption_updated_at: result.caption_updated_at }
        : candidate))
    } catch (cause) {
      setError(errorMessage(cause))
      throw cause
    }
  }

  if (loading) return <main className="status-page"><Brand /><div className="loading-line" /><p>Відкриваємо кімнату…</p></main>
  if (error && !room && !preview) return <main className="status-page"><Brand /><h1>Не вдалося відкрити кімнату</h1><p>{error}</p><Link className="primary-button" to="/">На головну</Link></main>
  if (preview && !room) return <JoinRoom preview={preview} onJoined={(joined) => { setPreview(null); setRoom(joined); void loadGallery() }} />
  if (!room) return null

  const ttl = remaining(room.expires_at)
  const expiryRemainingMs = new Date(room.expires_at).getTime() - clockNow
  const expiryWarning = expiryRemainingMs > 0 && expiryRemainingMs <= 6 * 60 * 60 * 1000
  const expiryWarningHours = Math.max(1, Math.ceil(expiryRemainingMs / (60 * 60 * 1000)))
  const roomLifetimeDays = Math.max(1, Math.min(3, Math.round((new Date(room.expires_at).getTime() - new Date(room.created_at).getTime()) / (24 * 60 * 60 * 1000))))
  const usedPercent = Math.min(100, (room.used_storage_bytes / room.max_storage_bytes) * 100)
  const filteredGallery = gallery.filter((item) => {
    if (galleryFilter === 'mine') return item.uploaded_by.id === session?.identity.id
    if (galleryFilter === 'favorites') return item.favorited
    if (galleryFilter === 'image' || galleryFilter === 'video') return item.media_type === galleryFilter
    return true
  }).sort((left, right) => galleryFilter === 'best'
    ? right.favorite_count - left.favorite_count || mediaDate(right).getTime() - mediaDate(left).getTime()
    : mediaDate(right).getTime() - mediaDate(left).getTime())
  const galleryGroups = galleryFilter === 'best' && filteredGallery.length
    ? [{ key: 'best', label: 'Найкращі кадри', items: filteredGallery }]
    : filteredGallery.reduce<Array<{ key: string; label: string; items: GalleryItem[] }>>((groups, item) => {
    const day = galleryDay(item)
    const existing = groups.at(-1)
    if (existing?.key === day.key) existing.items.push(item)
    else groups.push({ ...day, items: [item] })
    return groups
  }, [])
  const selectedMedia = filteredGallery.find((item) => item.id === selectedMediaID) ?? null
  const filterCounts: Record<GalleryFilter, number> = {
    all: gallery.length,
    image: gallery.filter((item) => item.media_type === 'image').length,
    video: gallery.filter((item) => item.media_type === 'video').length,
    mine: gallery.filter((item) => item.uploaded_by.id === session?.identity.id).length,
    favorites: gallery.filter((item) => item.favorited).length,
    best: gallery.length,
  }
  const selectedItems = gallery.filter((item) => selectedMediaIDs.has(item.id))
  const selectedDeletableCount = selectedItems.filter((item) => item.permissions.can_delete).length
  const coverItem = gallery.find((item) => item.is_cover && item.thumbnail_url)
  const shareURL = `${window.location.origin}/r/${slug}`
  const telegramInviteURL = telegramRoomLink(slug)
  const previewInviteURL = roomInviteLink(slug)

  return (
    <main className="room-shell">
      <header className="room-topbar">
        <Brand compact />
        <div className="room-topbar__actions">
          {gallery.length > 0 && <Link className="recap-button" to={`/r/${slug}/recap`}><Images size={17} /><span>Підсумок</span></Link>}
          <button className="share-button" onClick={() => setShareDialog(true)}>{copied ? <Check size={17} /> : <Share2 size={17} />}{copied ? 'Скопійовано' : 'Запросити'}</button>
          <button className="icon-button" onClick={() => setSettings(true)} aria-label="Налаштування кімнати"><Menu /></button>
        </div>
      </header>

      <section className={`room-heading ${coverItem ? 'room-heading--has-cover' : ''}`}>
        {coverItem && <img className="room-heading__cover" src={coverItem.thumbnail_url ?? ''} alt="" aria-hidden="true" />}
        <div>
          <p className="eyebrow">Кімната {room.slug}</p>
          <h1>{room.name}</h1>
          <div className="room-facts"><span><ShieldCheck size={16} /> {room.access_mode === 'public' ? 'Без пароля' : room.access_mode === 'pin' ? 'Захищено PIN' : 'Захищено паролем'}</span><span>{room.used_files} із {room.max_files} файлів</span></div>
        </div>
        <div className="ttl-display"><strong>{ttl.value}</strong><div><span>{ttl.unit}</span><small>{ttl.detail}</small></div></div>
      </section>

      {error && <div className="page-error" role="alert"><span>{error}</span><button onClick={() => setError('')} aria-label="Закрити"><X size={17} /></button></div>}

      {expiryWarning && (
        <section className="expiry-warning" aria-labelledby="expiry-warning-title">
          <div className="expiry-warning__marker" aria-hidden="true"><strong>{String(expiryWarningHours).padStart(2, '0')}</strong><span>{expiryWarningHours === 1 ? 'година' : 'годин'}</span></div>
          <div className="expiry-warning__copy">
            <p className="eyebrow">Автовидалення наближається</p>
            <h2 id="expiry-warning-title">{expiryWarningHours === 1 ? 'Менше години, щоб зберегти файли' : `Менше ${expiryWarningHours} годин, щоб зберегти файли`}</h2>
            <p>Кімната й усі оригінали будуть видалені назавжди {new Date(room.expires_at).toLocaleString('uk-UA', { dateStyle: 'long', timeStyle: 'short' })}.</p>
          </div>
          {(gallery.length > 0 || (room.role === 'owner' && roomLifetimeDays < 3)) && <div className="expiry-warning__actions">
            {gallery.length > 0 && <button className="primary-button" onClick={() => isMobileDevice() ? setMobileSaveOpen(true) : void handleArchive()}><ArrowDownToLine size={18} /> Зберегти файли</button>}
            {gallery.length > 0 && <Link className="secondary-button" to={`/r/${slug}/recap`}><Images size={18} /> Підсумок події</Link>}
            {room.role === 'owner' && roomLifetimeDays < 3 && <button className="secondary-button" onClick={() => setSettings(true)}><Clock3 size={18} /> Продовжити строк</button>}
          </div>}
        </section>
      )}

      {newMediaCount > 0 && (
        <aside className="new-media-shelf" aria-live="polite">
          <Images size={18} />
          <strong>Нових файлів: {newMediaCount}</strong>
          <button onClick={() => {
            setNewMediaCount(0)
            announcedMediaRef.current.clear()
            galleryAnchorRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
          }}>Показати</button>
        </aside>
      )}

      <section ref={galleryAnchorRef} className="gallery-toolbar">
        <div><h2>Галерея</h2><p>{bytes(room.used_storage_bytes)} використано</p></div>
        <div className="gallery-toolbar__actions">
          {gallery.length > 0 && (
            <button className="secondary-button primary-button--fit archive-button" onClick={() => isMobileDevice() ? setMobileSaveOpen(true) : void handleArchive()} disabled={!isMobileDevice() && (archiveBusy || roomArchive?.status === 'pending' || roomArchive?.status === 'processing')} aria-label="Завантажити всю галерею">
              {isMobileDevice() ? <Smartphone size={19} /> : <Archive size={19} />}
              <small>{isMobileDevice() ? 'Усе' : 'ZIP'}</small>
              <span>{isMobileDevice() ? 'Зберегти на телефон' : roomArchive?.status === 'ready' ? 'Завантажити ZIP' : roomArchive?.status === 'failed' ? 'Повторити ZIP' : roomArchive ? `${roomArchive.processed_files}/${roomArchive.total_files}` : 'Завантажити все'}</span>
            </button>
          )}
          {room.accepting_uploads && room.can_upload ? (
          <>
            <input ref={inputRef} type="file" accept="image/jpeg,image/png,image/webp,image/heic,image/heif,image/avif,image/gif,video/mp4,video/quicktime,video/webm,video/x-m4v,video/3gpp" multiple hidden onChange={(event) => void acceptFiles(event.target.files)} />
            <button className="primary-button primary-button--fit upload-button" onClick={() => inputRef.current?.click()} aria-label="Додати фото або відео"><ImagePlus size={21} /><span>Додати медіа</span></button>
          </>
          ) : <span className="uploads-closed"><LockKeyhole size={16} /> {room.accepting_uploads ? 'Доступ лише для перегляду' : 'Завантаження закриті'}</span>}
        </div>
      </section>
      <input ref={resumeInputRef} data-resume-upload type="file" hidden onChange={(event) => resumeUpload(event.target.files)} />

      <div className="storage-line"><span style={{ width: `${usedPercent}%` }} /></div>

      {gallery.length > 0 && (
        <div className="gallery-controls">
          <nav className="gallery-filters" aria-label="Фільтр галереї">
            {([
              ['all', 'Усі'],
              ['image', 'Фото'],
              ['video', 'Відео'],
              ['mine', 'Мої'],
              ['favorites', 'Обрані'],
              ['best', 'Найкращі'],
            ] as Array<[GalleryFilter, string]>).map(([value, label]) => (
              <button key={value} className={galleryFilter === value ? 'is-active' : ''} aria-pressed={galleryFilter === value} onClick={() => setGalleryFilter(value)}>
                <span>{label}</span><small>{filterCounts[value]}</small>
              </button>
            ))}
          </nav>
          <button className={`select-media-button ${selectionMode ? 'is-active' : ''}`} onClick={() => selectionMode ? closeSelection() : setSelectionMode(true)}>{selectionMode ? 'Готово' : 'Вибрати'}</button>
        </div>
      )}

      {roomArchive && <section className={`archive-status archive-status--${roomArchive.status}`} aria-live="polite">
        <div><Archive size={20} /><div><strong>{roomArchive.status === 'ready' ? 'Архів готовий' : roomArchive.status === 'failed' ? 'Не вдалося створити архів' : roomArchive.status === 'pending' ? 'Архів у черзі' : 'Збираємо оригінали'}</strong><span>{roomArchive.status === 'ready' ? `${roomArchive.total_files} файлів · ${bytes(roomArchive.size_bytes ?? roomArchive.total_bytes)}` : roomArchive.status === 'failed' ? 'Натисніть «Повторити ZIP»' : `${roomArchive.processed_files} із ${roomArchive.total_files} файлів`}</span></div></div>
        <div className="archive-progress"><span style={{ width: `${roomArchive.total_files ? (roomArchive.processed_files / roomArchive.total_files) * 100 : 0}%` }} /></div>
      </section>}

      {filteredGallery.length ? (
        <>
          <div className="gallery-groups">
            {galleryGroups.map((group) => (
              <section className="gallery-group" key={group.key} aria-labelledby={`gallery-day-${group.key}`}>
                <header><h3 id={`gallery-day-${group.key}`}>{group.label}</h3><span>{group.items.length}</span></header>
                <div className="gallery-grid">
                  {group.items.map((item) => <GalleryCard key={item.id} item={item} selectionMode={selectionMode} selected={selectedMediaIDs.has(item.id)} onToggle={toggleMediaSelection} onDelete={deleteItem} onOpen={openMedia} onFavorite={toggleFavorite} onError={setError} />)}
                </div>
              </section>
            ))}
          </div>
          {hasMore && <button className="secondary-button load-more" onClick={() => void loadGallery(true, cursor)}>Показати більше</button>}
        </>
      ) : activationVisible && room.role === 'owner' && room.accepting_uploads ? (
        <section className="room-activation" aria-labelledby="room-activation-title">
          <header>
            <div><p className="eyebrow">Кімната готова</p><h2 id="room-activation-title">Запустіть спільну галерею</h2></div>
            <button onClick={() => setActivationVisible(false)} aria-label="Закрити підказки"><X /></button>
          </header>
          <div className="room-activation__steps">
            <article className={activationShared ? 'is-complete' : ''}>
              <span>01</span>
              <div><Users /><h3>Запросіть друзів</h3><p>Поділіться посиланням або QR-кодом. Кожен зможе додати оригінали зі свого телефона.</p></div>
              <button className="secondary-button" onClick={() => { setActivationShared(true); setShareDialog(true) }}>{activationShared ? <Check size={18} /> : <Share2 size={18} />}{activationShared ? 'Запросити ще' : 'Відкрити запрошення'}</button>
            </article>
            <article>
              <span>02</span>
              <div><Upload /><h3>Додайте перший кадр</h3><p>Виберіть одразу кілька фото чи відео — вони завантажаться у фоновій черзі без стиснення.</p></div>
              <button className="primary-button" onClick={() => inputRef.current?.click()}><ImagePlus size={18} /> Вибрати медіа</button>
            </article>
          </div>
          <footer><ShieldCheck size={16} /><span>Кімната видалиться автоматично: {new Date(room.expires_at).toLocaleString('uk-UA', { dateStyle: 'long', timeStyle: 'short' })}</span></footer>
        </section>
      ) : (
        <section className="empty-gallery">
          <ImagePlus size={36} strokeWidth={1.5} />
          <h2>{gallery.length ? 'Тут поки порожньо' : 'Тут ще немає медіа'}</h2>
          <p>{gallery.length ? galleryFilter === 'favorites' ? 'Додайте серце кадрам, які хочете зберегти.' : 'У вибраному фільтрі немає файлів.' : room.accepting_uploads && room.can_upload ? 'Додайте перші фото або відео з події.' : room.accepting_uploads ? 'Це запрошення дозволяє лише переглядати галерею.' : 'Власник кімнати закрив завантаження.'}</p>
          {gallery.length ? <button className="text-link" onClick={() => setGalleryFilter('all')}>Показати всі</button> : room.accepting_uploads && room.can_upload && <button className="text-link" onClick={() => inputRef.current?.click()}>Вибрати з галереї</button>}
        </section>
      )}

      <UploadQueue
        uploads={uploads}
        onCancel={cancelUpload}
        onPause={pauseUpload}
        onRetry={retryUpload}
        onClearCompleted={() => setUploads((current) => current.filter((item) => item.state !== 'done'))}
      />

      {selectionMode && selectedItems.length > 0 && (
        <aside className="selection-bar" aria-live="polite">
          <div><strong>{selectedItems.length} вибрано</strong><span>{bytes(selectedItems.reduce((sum, item) => sum + item.size_bytes, 0))}</span></div>
          <div>
            <button onClick={() => void saveSelected(selectedItems)} disabled={batchBusy}><ArrowDownToLine size={18} /><span>Зберегти</span></button>
            {selectedDeletableCount > 0 && <button className="selection-bar__danger" onClick={() => void deleteSelected(selectedItems)} disabled={batchBusy}><Trash2 size={18} /><span>Видалити{selectedDeletableCount < selectedItems.length ? ` ${selectedDeletableCount}` : ''}</span></button>}
            <button className="selection-bar__close" onClick={closeSelection} disabled={batchBusy} aria-label="Скасувати вибір"><X size={19} /></button>
          </div>
        </aside>
      )}

      {shareDialog && <ShareDialog room={room} webURL={shareURL} telegramURL={telegramInviteURL} previewURL={previewInviteURL} onClose={() => setShareDialog(false)} onCopied={(url) => void copyLink(url)} />}
      {mobileSaveOpen && <MobileSaveDialog slug={slug} roomName={room.name} initialItems={mobileSaveItems} onClose={() => { setMobileSaveOpen(false); setMobileSaveItems(undefined) }} onError={setError} />}

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
                      <div><strong>{member.display_name}</strong><span>Приєднався {new Date(member.joined_at).toLocaleString('uk-UA', { dateStyle: 'medium', timeStyle: 'short' })}{member.role !== 'owner' ? ` · ${member.can_upload ? 'може додавати' : 'лише перегляд'}` : ''}</span></div>
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

      {selectedMedia && <MediaViewer item={selectedMedia} items={filteredGallery} canSetCover={room.role === 'owner'} onSelect={openMedia} onClose={closeMedia} onFavorite={toggleFavorite} onSetCover={setRoomCover} onCaption={updateCaption} onError={setError} />}

      {settings && (
        <div className="dialog-backdrop" role="presentation" onMouseDown={() => setSettings(false)}>
          <section className="settings-dialog" role="dialog" aria-modal="true" aria-labelledby="settings-title" onMouseDown={(event) => event.stopPropagation()}>
            <header><h2 id="settings-title">{room.role === 'owner' ? 'Налаштування' : 'Налаштування кімнати'}</h2><button ref={settingsCloseRef} className="icon-button" onClick={() => setSettings(false)} aria-label="Закрити"><X /></button></header>
            {room.role === 'owner' && <div className="settings-editor">
              <label className="field"><span>Назва кімнати</span><input value={settingsName} maxLength={120} onChange={(event) => setSettingsName(event.target.value)} /></label>
              <fieldset className="field"><legend>Доступ</legend><div className="segmented segmented--three">
                {([['public', 'Без пароля'], ['pin', 'PIN'], ['password', 'Пароль']] as const).map(([value, label]) => <button type="button" className={settingsAccessMode === value ? 'is-active' : ''} onClick={() => { setSettingsAccessMode(value); setSettingsAccessDirty(value !== room.access_mode || settingsSecret.length > 0) }} key={value}>{label}</button>)}
              </div></fieldset>
              {settingsAccessMode !== 'public' && <label className="field"><span>{settingsAccessMode === room.access_mode ? `Новий ${settingsAccessMode === 'pin' ? 'PIN-код' : 'пароль'} (необов’язково)` : settingsAccessMode === 'pin' ? 'Новий PIN-код' : 'Новий пароль'}</span><input type={settingsAccessMode === 'pin' ? 'tel' : 'password'} inputMode={settingsAccessMode === 'pin' ? 'numeric' : undefined} value={settingsSecret} maxLength={settingsAccessMode === 'pin' ? 8 : 72} placeholder={settingsAccessMode === room.access_mode ? 'Залиште порожнім без змін' : settingsAccessMode === 'pin' ? '4–8 цифр' : 'Щонайменше 6 символів'} onChange={(event) => { setSettingsSecret(settingsAccessMode === 'pin' ? event.target.value.replace(/\D/g, '') : event.target.value); setSettingsAccessDirty(true) }} /></label>}
              <fieldset className="field lifetime-field"><legend>Автовидалення від створення</legend><div className="lifetime-value"><strong>{settingsLifetime}</strong><span>{settingsLifetime === 1 ? 'день' : 'дні'}</span></div><input className="lifetime-slider" type="range" min={Math.max(1, Math.round((new Date(room.expires_at).getTime() - new Date(room.created_at).getTime()) / (24 * 60 * 60 * 1000)))} max="3" step="1" value={settingsLifetime} disabled={settingsLifetime >= 3} onChange={(event) => setSettingsLifetime(Number(event.target.value))} style={{ '--slider-progress': `${(settingsLifetime - 1) * 50}%` } as CSSProperties} /><div className="slider-labels"><span>Поточний строк</span><span>Максимум 3 дні</span></div></fieldset>
              {settingsError && <p className="form-error" role="alert">{settingsError}</p>}
              <button className="primary-button" onClick={() => void saveRoomSettings()} disabled={settingsBusy}>{settingsBusy ? 'Зберігаємо…' : 'Зберегти зміни'}</button>
            </div>}
            {room.role === 'owner' && <div className="setting-row"><div><strong>Учасники кімнати</strong><span>Перегляньте всіх, хто приєднався за посиланням або QR-кодом.</span></div><button className="secondary-button primary-button--fit" onClick={() => void openMembers()}><Users size={17} /> Переглянути</button></div>}
            <div className="setting-row"><div><strong>Сповіщення в Telegram</strong><span>{notificationSettings?.telegram_available ? 'Отримуйте лише вибрані події цієї кімнати в чаті із Zhyvo.' : 'Підключіть Telegram, щоб бот міг повідомляти про активність і наближення автовидалення.'}</span></div>{notificationSettings?.telegram_available ? <button className={`switch ${notificationSettings.telegram_enabled ? 'on' : ''}`} role="switch" aria-label="Сповіщення в Telegram" aria-checked={notificationSettings.telegram_enabled} disabled={notificationBusy} onClick={() => void toggleNotifications()}><span /></button> : <button className="secondary-button primary-button--fit" onClick={() => navigate(`/auth/telegram/link?returnTo=${encodeURIComponent(`/r/${slug}`)}`)}><Send size={17} /> Підключити</button>}</div>
            {notificationSettings?.telegram_available && notificationSettings.telegram_enabled && <div className="notification-options">
              <div><span><strong>Нові файли</strong><small>Одне згруповане повідомлення замість серії.</small></span><button className={`switch ${notificationSettings.new_media_enabled ? 'on' : ''}`} role="switch" aria-label="Сповіщати про нові файли" aria-checked={notificationSettings.new_media_enabled} disabled={notificationBusy} onClick={() => void updateNotificationPreference({ new_media_enabled: !notificationSettings.new_media_enabled })}><span /></button></div>
              <div><span><strong>Завершення кімнати</strong><small>Нагадування за 6 годин і за 1 годину.</small></span><button className={`switch ${notificationSettings.expiry_enabled ? 'on' : ''}`} role="switch" aria-label="Нагадувати про завершення кімнати" aria-checked={notificationSettings.expiry_enabled} disabled={notificationBusy} onClick={() => void updateNotificationPreference({ expiry_enabled: !notificationSettings.expiry_enabled })}><span /></button></div>
              <div><span><strong>Нові учасники</strong><small>Коли до кімнати приєднається інша людина.</small></span><button className={`switch ${notificationSettings.member_joined_enabled ? 'on' : ''}`} role="switch" aria-label="Сповіщати про нових учасників" aria-checked={notificationSettings.member_joined_enabled} disabled={notificationBusy} onClick={() => void updateNotificationPreference({ member_joined_enabled: !notificationSettings.member_joined_enabled })}><span /></button></div>
            </div>}
            {room.role !== 'owner' && settingsError && <p className="form-error notification-error" role="alert">{settingsError}</p>}
            {room.role === 'owner' && <div className="setting-row"><div><strong>Приймати нових учасників</strong><span>Вимкніть, щоб нові люди не могли приєднатися за старим посиланням.</span></div><button className={`switch ${room.accepting_members ? 'on' : ''}`} role="switch" aria-checked={room.accepting_members} onClick={() => void toggleMembers()}><span /></button></div>}
            {room.role === 'owner' && <div className="setting-row"><div><strong>Приймати нові файли</strong><span>Учасники бачитимуть галерею, але не зможуть завантажувати медіа.</span></div><button className={`switch ${room.accepting_uploads ? 'on' : ''}`} role="switch" aria-checked={room.accepting_uploads} onClick={() => void toggleUploads()}><span /></button></div>}
            {room.role === 'owner' && <div className="setting-row setting-row--danger"><div><strong>Видалити кімнату</strong><span>Усі оригінали та дані буде видалено без можливості відновлення.</span></div><button className="danger-button" onClick={deleteRoom}><Trash2 size={17} /> Видалити</button></div>}
          </section>
        </div>
      )}
    </main>
  )
}

function AdminPage() {
  const session = useSession()
  const navigate = useNavigate()
  const { reportID } = useParams()
  const [stats, setStats] = useState<AdminStats | null>(null)
  const [reports, setReports] = useState<ProblemReport[]>([])
  const [selected, setSelected] = useState<ProblemReport | null>(null)
  const [statusFilter, setStatusFilter] = useState('')
  const [categoryFilter, setCategoryFilter] = useState('')
  const [status, setStatus] = useState<ProblemReportStatus>('new')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [forbidden, setForbidden] = useState(false)
  const [telegramID, setTelegramID] = useState<number | null>(session?.identity.telegram_user_id ?? null)
  const [idCopied, setIDCopied] = useState(false)

  useEffect(() => {
    if (session?.identity.kind !== 'telegram' || telegramID) return
    auth.me().then((result) => setTelegramID(result.identity.telegram_user_id ?? null)).catch(() => undefined)
  }, [session, telegramID])

  const load = useCallback(async (quiet = false) => {
    if (!session) return
    if (!quiet) setBusy(true)
    try {
      const [statsResult, listResult, detailResult] = await Promise.all([
        admin.stats(), admin.reports({ status: statusFilter, category: categoryFilter }), reportID ? admin.report(reportID) : Promise.resolve(null),
      ])
      setStats(statsResult)
      setReports(listResult.reports)
      if (detailResult) {
        setSelected(detailResult.report)
        setStatus(detailResult.report.status)
        setNote(detailResult.report.admin_note ?? '')
      } else setSelected((current) => current && !listResult.reports.some((report) => report.id === current.id) ? null : current)
      setForbidden(false)
      setError('')
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 403) setForbidden(true)
      else setError(errorMessage(cause))
    } finally { if (!quiet) setBusy(false) }
  }, [categoryFilter, reportID, session, statusFilter])

  useEffect(() => { void load(); const timer = window.setInterval(() => void load(true), 15_000); return () => window.clearInterval(timer) }, [load])

  function openReport(report: ProblemReport) {
    setSelected(report)
    setStatus(report.status)
    setNote(report.admin_note ?? '')
    navigate(`/admin/reports/${report.id}`)
  }

  async function saveReport() {
    if (!selected) return
    setBusy(true)
    try {
      const result = await admin.updateReport(selected.id, status, note)
      setSelected(result.report)
      setReports((current) => current.map((report) => report.id === result.report.id ? result.report : report))
      await load(true)
    } catch (cause) { setError(errorMessage(cause)) } finally { setBusy(false) }
  }

  if (!session) return <main className="admin-gate"><Brand /><BarChart3 size={52} /><h1>Панель Zhyvo</h1><p>Відкрийте панель із повідомлення бота або спочатку підключіть Telegram на головній.</p><Link className="primary-button" to="/">На головну</Link></main>
  if (session.identity.kind !== 'telegram') return <main className="admin-gate"><Brand /><ShieldCheck size={52} /><h1>Потрібен Telegram</h1><p>Адміністративний доступ перевіряється за вашим Telegram ID.</p><button className="primary-button" onClick={() => navigate(`/auth/telegram/link?returnTo=${encodeURIComponent(location.pathname)}`)}><Send size={18} /> Підключити Telegram</button></main>
  if (forbidden) return <main className="admin-gate"><Brand /><Ban size={52} /><h1>Доступ закрито</h1><p>Цей Telegram-акаунт не входить до списку адміністраторів Zhyvo.</p>{telegramID && <div className="admin-own-id"><span>Ваш Telegram ID</span><strong>{telegramID}</strong><button onClick={async () => { await navigator.clipboard.writeText(String(telegramID)); setIDCopied(true) }}>{idCopied ? <Check size={16} /> : <Copy size={16} />}{idCopied ? 'Скопійовано' : 'Скопіювати'}</button></div>}<Link className="primary-button" to="/">На головну</Link></main>

  return <main className="admin-shell">
    <header className="admin-topbar"><Brand compact /><div><span>Внутрішня панель</span><strong>{session.identity.display_name}</strong></div><Link to="/">Відкрити Zhyvo</Link></header>
    <section className="admin-heading"><div><p className="eyebrow">Стан продукту</p><h1>Операційна панель</h1></div><button className="secondary-button" onClick={() => void load()} disabled={busy}><RefreshCw size={17} /> Оновити</button></section>
    {stats && <section className="admin-stats" aria-label="Показники Zhyvo">
      <div><strong>{stats.active_rooms}</strong><span>активних кімнат</span><small>зараз</small></div>
      <div><strong>{stats.ready_media}</strong><span>готових файлів</span><small>+{stats.uploads_today} сьогодні</small></div>
      <div><strong>{stats.total_users}</strong><span>користувачів</span><small>+{stats.new_users_today} сьогодні</small></div>
      <div><strong>{bytes(stats.stored_bytes)}</strong><span>оригіналів</span><small>у сховищі</small></div>
      <div className={stats.new_reports ? 'is-alert' : ''}><strong>{stats.new_reports}</strong><span>нових звернень</span><small>{stats.reports_today} сьогодні</small></div>
    </section>}
    {stats && <section className="admin-pipeline" aria-label="Стан завантажень">
      <header><div><h2>Завантаження</h2><p>Поточний стан медіаконвеєра за даними PostgreSQL.</p></div><span className={stats.upload_failures_today || stats.thumbnail_failures ? 'has-warning' : ''}>{stats.upload_failures_today || stats.thumbnail_failures ? 'Потрібна увага' : 'Працює нормально'}</span></header>
      <div><strong>{stats.uploads_today}</strong><span>завершено сьогодні</span></div>
      <div><strong>{stats.uploads_in_progress}</strong><span>завантажується зараз</span></div>
      <div className={stats.upload_failures_today ? 'has-warning' : ''}><strong>{stats.upload_failures_today}</strong><span>відхилено сьогодні</span></div>
      <div className={stats.thumbnail_failures ? 'has-warning' : ''}><strong>{stats.thumbnail_failures}</strong><span>помилок прев’ю</span></div>
      <div><strong>{bytes(stats.reserved_bytes)}</strong><span>зарезервовано</span></div>
    </section>}
    {error && <p className="form-error admin-error" role="alert">{error}</p>}
    <section className="admin-workspace">
      <div className="admin-inbox">
        <header><div><Inbox size={21} /><h2>Звернення</h2></div><span>{reports.length}</span></header>
        <div className="admin-filters"><label><span>Статус</span><select value={statusFilter} onChange={(event) => setStatusFilter(event.target.value)}><option value="">Усі</option>{Object.entries(reportStatusLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label><label><span>Категорія</span><select value={categoryFilter} onChange={(event) => setCategoryFilter(event.target.value)}><option value="">Усі</option>{Object.entries(reportCategoryLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label></div>
        <div className="admin-report-list">{busy && !stats ? <div className="loading-line" /> : reports.length ? reports.map((report) => <button className={selected?.id === report.id ? 'is-active' : ''} onClick={() => openReport(report)} key={report.id}><span className={`report-status report-status--${report.status}`}>{reportStatusLabels[report.status]}</span><strong>{report.public_id}</strong><p>{report.description}</p><small>{reportCategoryLabels[report.category]} · {new Date(report.created_at).toLocaleString('uk-UA', { dateStyle: 'short', timeStyle: 'short' })}</small></button>) : <div className="admin-empty"><Check size={34} /><strong>Звернень немає</strong><span>За вибраними фільтрами нічого не знайдено.</span></div>}</div>
      </div>
      <div className="admin-detail">{selected ? <>
        <header><div><span className={`report-status report-status--${selected.status}`}>{reportStatusLabels[selected.status]}</span><h2>{selected.public_id}</h2></div><button className="icon-button" onClick={() => { setSelected(null); navigate('/admin/reports') }} aria-label="Закрити звернення"><X /></button></header>
        <div className="admin-report-copy"><p>{selected.description}</p><dl><div><dt>Категорія</dt><dd>{reportCategoryLabels[selected.category]}</dd></div><div><dt>Створено</dt><dd>{new Date(selected.created_at).toLocaleString('uk-UA', { dateStyle: 'long', timeStyle: 'short' })}</dd></div><div><dt>Автор</dt><dd>{selected.reporter_name ?? 'Анонімний відвідувач'}</dd></div><div><dt>Контакт</dt><dd>{selected.contact ?? 'Не вказано'}</dd></div></dl></div>
        <section className="admin-context"><h3>Технічна інформація</h3><dl>{Object.entries(selected.technical_context).map(([key, value]) => <div key={key}><dt>{({ route: 'Сторінка', app_build: 'Збірка', platform: 'Платформа', browser: 'Браузер', telegram: 'Telegram Mini App', online: 'Мережа', error_code: 'Код помилки', request_id: 'Request ID', occurred_at: 'Час помилки' } as Record<string, string>)[key] ?? key}</dt><dd>{typeof value === 'boolean' ? value ? 'Так' : 'Ні' : String(value)}</dd></div>)}</dl></section>
        <div className="admin-resolution"><label className="field"><span>Статус</span><select value={status} onChange={(event) => setStatus(event.target.value as ProblemReportStatus)}>{Object.entries(reportStatusLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label><label className="field"><span>Внутрішня нотатка</span><textarea maxLength={2000} value={note} onChange={(event) => setNote(event.target.value)} placeholder="Що перевірено або виправлено" /></label><button className="primary-button" disabled={busy} onClick={() => void saveReport()}>{busy ? 'Зберігаємо…' : 'Зберегти зміни'}</button></div>
      </> : <div className="admin-detail-empty"><Inbox size={52} /><h2>Виберіть звернення</h2><p>Тут з’явиться опис, безпечний технічний контекст і робоча нотатка.</p></div>}</div>
    </section>
  </main>
}

export default function App() {
  const telegramError = getTelegramBootstrapError()
  return (
    <>
      <TelegramNavigation />
      {telegramError && <div className="telegram-error" role="alert">{telegramError}</div>}
      <ProblemReporter />
      <Routes>
        <Route path="/" element={<HomePage />} />
        <Route path="/r/:slug" element={<RoomPage />} />
        <Route path="/r/:slug/recap" element={<RecapPage />} />
        <Route path="/i/:token" element={<ManagedInvitePage />} />
        <Route path="/auth/telegram/callback" element={<TelegramLoginCallback />} />
		<Route path="/auth/telegram/link" element={<BrowserLinkPage />} />
		<Route path="/auth/telegram/link-confirm" element={<TelegramLinkConfirmPage />} />
        <Route path="/admin/reports" element={<AdminPage />} />
        <Route path="/admin/reports/:reportID" element={<AdminPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </>
  )
}
