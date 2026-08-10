# Telegram Mini App

## Що вже реалізовано

- офіційний Telegram Web App bridge підключений до frontend;
- Mini App викликає `ready()` та `expand()`;
- підтримуються Telegram light/dark theme, viewport і content safe area;
- на внутрішніх екранах вмикається нативна Telegram Back Button;
- `startapp=room_CODE` відкриває відповідну кімнату;
- backend перевіряє HMAC-SHA-256 підпис і строк дії сирого `initData`;
- Telegram identity повторно використовується за стабільним `telegram_user_id`;
- старі або прострочені локальні сесії автоматично перевидаються з актуального `initData`;
- одиночні файли у Mini App завантажуються через нативний `Telegram.WebApp.downloadFile`;
- браузерну анонімну identity можна без втрати кімнат прив'язати до Telegram через OIDC + PKCE;
- браузерний анонімний режим працює незалежно від Telegram.

## Створення production-бота Zhyvo

Production username: `@zhyvoappbot` (створено 2026-08-03).

1. Відкрити офіційного `@BotFather` у Telegram.
2. Надіслати `/newbot`.
3. Display name: `Zhyvo`.
4. Username: `zhyvoappbot`.
5. Скопіювати отриманий token у менеджер паролів. Не надсилати його в чат, не додавати до frontend і не комітити до Git.
6. Розгорнути застосунок на публічному HTTPS URL. `localhost` Telegram відкрити не зможе.
7. Додати token на сервер у `.env` як `TELEGRAM_BOT_TOKEN`.
8. У BotFather відкрити `/mybots` → `Zhyvo` → `Bot Settings` → `Configure Mini App` → `Enable Mini App` і вказати production HTTPS URL.
9. Налаштувати Main Mini App, щоб у профілі бота з'явилася кнопка запуску. Текст кнопки: `Відкрити Zhyvo`.
10. Відкрити Mini App кнопкою бота. Ім'я користувача має з'явитися автоматично без поля реєстрації.

Перевірка через Bot API `getMe` має повертати `has_main_web_app: true`. Налаштування лише `Menu Button` недостатньо для посилань `?startapp=...`: воно відкриває фіксований URL без параметра кімнати.

Bot token є секретом рівня пароля. Якщо він випадково потрапив у повідомлення, Git або screenshot, його треба одразу перевипустити через BotFather.

## Deep link кімнати

Для налаштованого Main Mini App пряме посилання має вигляд:

```text
https://t.me/zhyvoappbot?startapp=room_<ROOM_CODE>
```

Frontend генерує це посилання для QR-коду, копіювання, системного меню «Поділитися» і Telegram share picker. Значення `room_<ROOM_CODE>` надходить у `start_param`/`tgWebAppStartParam` та відкриває потрібну кімнату. Доступ усе одно контролюють серверна сесія, membership та PIN/пароль.

Для надсилання використовується проміжна адреса `/invite/<ROOM_CODE>` з Open Graph title, description і зображенням 1200×630. Telegram показує її як картку кімнати, а після натискання сторінка перенаправляє користувача у `startapp` Mini App. QR-код залишається прямим Telegram deep link.

Клієнт читає параметр із `initDataUnsafe.start_param`, підписаного raw `initData` та GET/hash-параметрів Telegram. Це покриває відмінності між Telegram iOS, Android і Desktop, але працює лише якщо для бота дійсно ввімкнено Main Mini App у BotFather.

Username можна перевизначити під час frontend build через `VITE_TELEGRAM_BOT_USERNAME`; типовим значенням є `zhyvoappbot`.

## Вхід через Telegram у звичайному браузері

Використовується актуальний Telegram OpenID Connect Authorization Code Flow з PKCE. API перевіряє `RS256`-підпис через офіційний JWKS, `iss`, `aud`, `exp`, `iat` і `nonce`. Після підтвердження анонімна identity транзакційно об'єднується з Telegram identity: переносяться власність кімнат, membership, авторство медіа та активні upload sessions.

Одноразове налаштування в `@BotFather`:

1. `/mybots` → `Zhyvo` → `Bot Settings` → `Web Login`.
2. Додати Allowed URL `https://zhyvo-preview.tail5b4bc9.ts.net`.
3. Додати redirect URI `https://zhyvo-preview.tail5b4bc9.ts.net/auth/telegram/callback`.
4. Скопіювати Client ID та Client Secret у `.env` як `TELEGRAM_LOGIN_CLIENT_ID` і `TELEGRAM_LOGIN_CLIENT_SECRET`.
5. Перезапустити `api`: `docker compose up -d --build api frontend`.

Client Secret не можна додавати до frontend або Git. Без обох OIDC-змінних Mini App auth продовжує працювати, а браузерний endpoint конфігурації повертає `enabled: false`.

## Локальна перевірка

Без `TELEGRAM_BOT_TOKEN` API і PWA запускаються як раніше. Endpoint `/api/v1/auth/telegram` повертає `503 TELEGRAM_AUTH_NOT_CONFIGURED`, а браузер використовує анонімну identity. Без `TELEGRAM_LOGIN_CLIENT_ID`/`TELEGRAM_LOGIN_CLIENT_SECRET` вимкнений лише браузерний OIDC-вхід.

## Сповіщення власника

Власник Telegram-identity може окремо ввімкнути сповіщення в налаштуваннях кожної кімнати. За замовчуванням вони вимкнені. Worker читає транзакційний outbox і повідомляє про нових учасників та нові файли; послідовні завантаження одного учасника об'єднуються в короткий дайджест. Помилки Telegram API повторюються з exponential backoff, а `400/403` позначаються як постійна помилка без нескінченних повторів.

Повідомлення не надають застосунку доступу до контактів, телефону або інших чатів. Кнопка в повідомленні відкриває конкретну кімнату через `startapp=room_<ROOM_CODE>`.
