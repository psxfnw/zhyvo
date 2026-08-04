# Telegram Mini App

## Що вже реалізовано

- офіційний Telegram Web App bridge підключений до frontend;
- Mini App викликає `ready()` та `expand()`;
- підтримуються Telegram light/dark theme, viewport і content safe area;
- на внутрішніх екранах вмикається нативна Telegram Back Button;
- `startapp=room_CODE` відкриває відповідну кімнату;
- backend перевіряє HMAC-SHA-256 підпис і строк дії сирого `initData`;
- Telegram identity повторно використовується за стабільним `telegram_user_id`;
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

Bot token є секретом рівня пароля. Якщо він випадково потрапив у повідомлення, Git або screenshot, його треба одразу перевипустити через BotFather.

## Deep link кімнати

Після створення short name для Mini App пряме посилання матиме вигляд:

```text
https://t.me/<bot_username>/<app_short_name>?startapp=room_<ROOM_CODE>
```

Параметр використовується лише для навігації. Доступ до кімнати все одно контролюють серверна сесія, membership та PIN/пароль.

## Локальна перевірка

Без `TELEGRAM_BOT_TOKEN` API і PWA запускаються як раніше. Endpoint `/api/v1/auth/telegram` повертає `503 TELEGRAM_AUTH_NOT_CONFIGURED`, а браузер використовує анонімну identity.
