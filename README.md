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

## Frontend

Мобільний клієнт розташований у `frontend/` і збирається як React/TypeScript PWA. Через Docker він доступний на `http://localhost:3000`, а Nginx проксіює REST-запити до API. Окремо встановлювати Node.js для звичайного локального запуску через Docker не потрібно.

Для режиму розробки з автоматичним оновленням потрібен Node.js 22+:

```powershell
Set-Location frontend
npm install
npm run dev
```

Vite відкриється на `http://localhost:5173` і сам проксіюватиме `/api` до backend.

Браузерний smoke-test перевіряє мобільну кімнату, Telegram/QR-запрошення, upload, фоновий ZIP і його реальне завантаження, viewer, керування учасниками, передачу власності, touch targets і адаптивність на 375/768/1024/1440 px. Для нього Docker-оточення має працювати, а Chrome/Chromium — бути встановленим:

```powershell
Set-Location frontend
npm run test:e2e
```

Актуальні UI-токени та UX-правила зафіксовані в `design-system/photodrop/MASTER.md`.

### Завантажити все

`POST /rooms/{slug}/archive` створює фонове завдання для поточної версії галереї або повертає вже готовий ZIP. Worker потоково читає оригінали зі S3-сумісного сховища й одразу пише архів назад у сховище, тому розмір кімнати не дублюється в оперативній пам'яті чи на локальному диску. Після зміни складу галереї її версія збільшується, а наступний запит створює новий архів. Усі ZIP-файли лежать під префіксом кімнати й видаляються разом із нею після TTL.

## Telegram Mini App

Frontend автоматично визначає запуск усередині Telegram, застосовує Telegram theme/safe-area, показує нативну Back Button та обмінює підписаний `Telegram.WebApp.initData` на звичайну Zhyvo-сесію. У браузері залишається анонімний flow без Telegram.

Для реального Telegram-запуску додайте отриманий від BotFather токен лише до backend `.env`:

```env
TELEGRAM_BOT_TOKEN=123456789:replace_with_real_token
TELEGRAM_INIT_DATA_TTL=10m
```

Токен не можна додавати до `frontend/`, Vite-змінних або Git. Покрокове підключення бота описане в `docs/telegram-mini-app.md`.

## Media pipeline

Файли завантажуються напряму в MinIO/R2 через presigned URLs. API резервує заявлений розмір у ліміті кімнати, перевіряє фактичний розмір після завершення й переводить медіа у стан `ready`. Worker асинхронно створює JPEG thumbnail шириною до 480 px. Поки thumbnail обробляється, gallery повертає `thumbnail_status: pending`.
