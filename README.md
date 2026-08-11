# Zhyvo

MVP сервісу для тимчасового обміну оригінальними фото та відео. Кімнати мають TTL 1–3 дні, після чого worker видаляє медіа зі S3-сумісного сховища та записи кімнати з PostgreSQL.

Backend складається з трьох окремих процесів:

- `api` — REST API, auth, rooms, presigned upload/download URLs і gallery;
- `worker` — TTL cleanup, очищення покинутих uploads, FFmpeg thumbnails і потокове створення ZIP-архівів;
- `migrate` — застосування версійних PostgreSQL-міграцій перед стартом API.

## Локальний запуск

Потрібні Docker Desktop і Docker Compose.

```powershell
Copy-Item .env.example .env
docker compose up --build
```

Після запуску:

- Zhyvo PWA: http://localhost:3000
- API readiness: http://localhost:8080/health/ready
- MinIO API: http://localhost:9000
- MinIO Console: http://localhost:9001
- PostgreSQL: `localhost:5432`

Перший доступний API flow:

```text
POST   /api/v1/auth/anonymous
POST   /api/v1/auth/telegram
POST   /api/v1/auth/refresh
GET    /api/v1/auth/me
DELETE /api/v1/auth/session
POST   /api/v1/rooms
GET    /api/v1/rooms
GET    /api/v1/rooms/{slug}/preview
POST   /api/v1/rooms/{slug}/join
GET    /api/v1/rooms/{slug}
GET    /api/v1/rooms/{slug}/members
DELETE /api/v1/rooms/{slug}/members/{identityID}
DELETE /api/v1/rooms/{slug}/blocked-members/{identityID}
POST   /api/v1/rooms/{slug}/ownership
GET    /api/v1/rooms/{slug}/activity
PATCH  /api/v1/rooms/{slug}
DELETE /api/v1/rooms/{slug}
POST   /api/v1/rooms/{slug}/uploads
POST   /api/v1/uploads/{uploadID}/parts
POST   /api/v1/uploads/{uploadID}/complete
DELETE /api/v1/uploads/{uploadID}
GET    /api/v1/rooms/{slug}/media
PUT    /api/v1/media/{mediaID}/favorite
DELETE /api/v1/media/{mediaID}/favorite
PUT    /api/v1/rooms/{slug}/cover
POST   /api/v1/media/{mediaID}/download-url
DELETE /api/v1/media/{mediaID}
POST   /api/v1/rooms/{slug}/archive
GET    /api/v1/archives/{archiveID}
POST   /api/v1/archives/{archiveID}/download-url
```

Зупинка:

```powershell
docker compose down
```

Дані PostgreSQL і MinIO зберігаються в Docker volumes між перезапусками.

### Щоденний запуск локального preview

Preview працює з вашого комп’ютера, тому він доступний лише коли комп’ютер увімкнений і Docker Desktop запущений. Окремий Tailscale-контейнер ізольований від встановленого у Windows робочого Tailscale та не змінює його налаштувань.

Зазвичай після входу у Windows достатньо запустити Docker Desktop: контейнери з політикою `restart: unless-stopped` піднімуться автоматично. Якщо сайт недоступний, у корені проєкту виконайте:

```powershell
docker compose --profile preview up -d
docker compose --profile preview ps
```

У колонці `STATUS` сервіси `api` та `frontend` мають стати `healthy`. Потім перевірте [локальний readiness](http://localhost:8080/health/ready) і preview `https://zhyvo-preview.tail5b4bc9.ts.net`. Повна перебудова потрібна лише після змін коду:

```powershell
docker compose --profile preview up -d --build
```

Для діагностики без зміни Tailscale у Windows:

```powershell
docker compose logs --tail 100 api worker frontend tailscale-preview
```

## Frontend

Мобільний клієнт розташований у `frontend/` і збирається як React/TypeScript PWA. Через Docker він доступний на `http://localhost:3000`, а Nginx проксіює REST-запити до API. Окремо встановлювати Node.js для звичайного локального запуску через Docker не потрібно.

Для режиму розробки з автоматичним оновленням потрібен Node.js 22+:

```powershell
Set-Location frontend
npm install
npm run dev
```

Vite відкриється на `http://localhost:5173` і сам проксіюватиме `/api` до backend.

Браузерний smoke-test перевіряє мобільну кімнату, Telegram/QR-запрошення, upload із п'ятьма штучними обривами та відновленням після reload, фільтри й пакетний вибір, фоновий ZIP і його реальне завантаження, viewer, зміну назви та TTL, закриття входу, керування учасниками, передачу власності, touch targets і адаптивність на 375/768/1024/1440 px. Для нього Docker-оточення має працювати, а Chrome/Chromium — бути встановленим:

```powershell
Set-Location frontend
npm run test:e2e
```

Під час першого відкриття головної сторінки Zhyvo коротко пояснює основний сценарій у трьох кроках. Проходження зберігається лише в поточному браузері; інструкцію завжди можна знову відкрити через `Як це працює`, а прямі запрошення одразу ведуть до кімнати.

Повний перелік перевірок перед показом або релізом є у `docs/release-checklist.md`.

Актуальні UI-токени та UX-правила зафіксовані в `design-system/photodrop/MASTER.md`.

### Завантажити все

`POST /rooms/{slug}/archive` створює фонове завдання для поточної версії галереї або повертає вже готовий ZIP. Worker потоково читає оригінали зі S3-сумісного сховища й одразу пише архів назад у сховище, тому розмір кімнати не дублюється в оперативній пам'яті чи на локальному диску. Після зміни складу галереї її версія збільшується, а наступний запит створює новий архів. Усі ZIP-файли лежать під префіксом кімнати й видаляються разом із нею після TTL.

## Telegram Mini App

Frontend автоматично визначає запуск усередині Telegram, застосовує Telegram theme/safe-area, показує нативну Back Button та обмінює підписаний `Telegram.WebApp.initData` на звичайну Zhyvo-сесію. У браузері залишається анонімний flow без Telegram.

Для реального Telegram-запуску додайте отриманий від BotFather токен лише до backend `.env`:

```env
TELEGRAM_BOT_TOKEN=123456789:replace_with_real_token
TELEGRAM_INIT_DATA_TTL=10m
ADMIN_TELEGRAM_IDS=123456789
```

`ADMIN_TELEGRAM_IDS` — це один або кілька числових Telegram ID через кому. Лише ці акаунти можуть відкрити `/admin/reports` і отримують від бота повідомлення про нові звернення. Токен та список адміністраторів не можна додавати до `frontend/`, Vite-змінних або Git. Покрокове підключення бота описане в `docs/telegram-mini-app.md`.

## Media pipeline

Файли завантажуються напряму в MinIO/R2 через presigned URLs. API резервує заявлений розмір у ліміті кімнати, перевіряє фактичний розмір після завершення й переводить медіа у стан `ready`. Worker асинхронно створює JPEG thumbnail шириною до 480 px. Поки thumbnail обробляється, gallery повертає `thumbnail_status: pending`.

Frontend повторює тимчасово невдалі API/S3-запити до п'яти разів з exponential backoff і чекає відновлення мережі. Незавершена черга, idempotency key та ETag готових multipart-частин зберігаються локально до 20 годин. Самі фото й відео в browser storage не копіюються: після перезапуску Mini App користувач повторно вибирає той самий файл за назвою та розміром, після чого клієнт продовжує чинну upload-сесію. Явне «Скасувати» завершує серверну сесію та звільняє зарезервований ліміт кімнати.

Перед стартом upload браузер обчислює SHA-256 у фоновому Web Worker частинами по 8 МБ. API та частковий унікальний індекс PostgreSQL не дозволяють додати однаковий вміст у кімнату вдруге, навіть якщо файл перейменували; дублікати відсікаються до передачі в object storage.

## Live-кімнати

Зміни медіа, thumbnail, обраного, налаштувань та учасників записуються в транзакційний журнал PostgreSQL і доставляються в PWA через SSE. Один PostgreSQL `LISTEN` на API-процес роздає сигнали тільки підписникам відповідної кімнати; самі події читаються з журналу за монотонним ID, тому клієнт відновлюється через `Last-Event-ID` після короткого обриву. Nginx вимикає buffering для stream-маршруту, а повільний polling залишається резервним механізмом.

У переглядачі автор файлу або власник кімнати може додати plain-text підпис до 300 символів. Поруч відображаються фактичний автор, час зйомки й завантаження, роздільність або тривалість, формат та розмір оригіналу. Зміни підпису надходять іншим учасникам через той самий live-потік.
