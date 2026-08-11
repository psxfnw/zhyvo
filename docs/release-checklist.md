# Zhyvo MVP release checklist

Use this checklist before a demo or deployment. It avoids relying only on a successful build while a user flow is broken.

## Automated checks

```powershell
go test ./...

Set-Location frontend
npm run lint
npm run build
npm run test:e2e
```

The E2E test expects the Docker stack to be running. On this Windows preview machine, if public Tailscale DNS does not resolve to the local preview in headless Chrome, run only the test process with:

```powershell
$env:CHROME_HOST_RESOLVER_RULES='MAP zhyvo-preview.tail5b4bc9.ts.net 185.40.234.198'
npm run test:e2e
Remove-Item Env:CHROME_HOST_RESOLVER_RULES
```

This changes name resolution only inside the launched test browser and does not modify Windows or work Tailscale settings.

## Manual phone check

- Open the home page in a fresh browser profile, complete the three onboarding steps, and confirm they do not reopen after refresh.
- Reopen onboarding through `Як це працює`, then confirm a direct room invite never shows it before the room.
- Create a room and confirm the owner sees the two-step invite/upload panel; reload and confirm it does not return.
- Open the same empty room through a direct invitation and confirm the activation panel is not shown.
- Open the Mini App from `@zhyvoappbot` and from a room invite link.
- Create a room, leave it, and confirm it remains under `Мої кімнати`.
- Join from a second Telegram account and confirm the room opens directly.
- Create separate `Можуть додавати` and `Лише перегляд` links. Confirm the latter can browse and save but receives `UPLOAD_NOT_ALLOWED` when attempting an upload.
- Replace a managed link and confirm its old URL no longer opens; confirm an already joined member keeps room access.
- Disable the legacy room-code link and confirm active members still share the current managed contributor link.
- Upload several photos, one video, and—when available—an HEIC photo.
- Upload the same bytes under another filename and confirm Zhyvo reports `Вже є в галереї` without increasing room storage.
- Confirm thumbnails have the right orientation and a video shows its duration.
- Add favorites from two accounts, verify the shared count, and open `Обрані` and `Найкращі`.
- Open `Підсумок події`, verify its full-room counts and highlights, then start the device-appropriate save-all flow from that page.
- Keep the room open on two devices, change a favorite and upload a file on the second one, and confirm the first updates without reloading.
- Scroll below the gallery heading before the second-device upload and confirm the `Нових файлів` shelf returns to the fresh media.
- Add and edit a caption in the viewer, confirm another open device updates live, and verify a regular member cannot edit someone else's caption.
- Confirm the viewer shows the real author, upload time, capture time when present, dimensions or duration, format, and original size.
- As the owner, set an image as the room cover; confirm a regular member cannot replace it.
- Download one original file and use the mobile multi-file save/share flow.
- Close joining and uploads independently; confirm existing members still see the gallery.
- Confirm the owner sees members and room activity, and can delete any media while a member can delete only their own.
- Verify Telegram notifications only after the owner explicitly enables them.
- For a room with less than six hours remaining, verify every member sees the exact deletion warning and can start the device-appropriate save flow.
- Enable Telegram notifications and confirm the outbox contains one deadline-scoped reminder for six hours and one for one hour; disabling notifications removes unsent expiry reminders.
- Open `Повідомити про проблему`, submit at least ten characters, and verify the returned `ZHY-…` reference. Disable technical context once and confirm no device or route fields are stored.
- Submit a report from a room and an invite route; confirm stored routes are `/r/:room` and `/i/:invite`, with no room code or token.
- Confirm an ordinary Telegram identity receives `403` for `/api/v1/admin/*`, while an ID from `ADMIN_TELEGRAM_IDS` can open the metrics and report queue.
- Confirm the configured administrator receives the bot notification and its button opens the same report in the Mini App.

## Operational checks

- `docker compose --profile preview ps` shows healthy `api` and `frontend` services.
- `http://localhost:8080/health/ready` returns ready.
- No bot token, OIDC secret, Tailscale auth key, or real `.env` file is tracked by Git.
- `ADMIN_TELEGRAM_IDS` contains only intended administrators and is never exposed to the frontend bundle.
- PostgreSQL and object-storage volumes are backed up before any destructive migration.
- The preview limitation is understood: the public URL stops when this PC or Docker Desktop is off.
- Nginx keeps buffering disabled for `/api/v1/rooms/{slug}/events`; otherwise SSE updates may arrive in delayed batches.

## Current MVP boundaries

- Rooms expire one to three days after creation; extending never exceeds the three-day cap.
- Browser downloads depend on platform capabilities. Desktop receives ZIP; mobile prefers native share/save in bounded batches.
- The browser cannot silently write many originals into the iOS/Android photo library. A future Capacitor client will provide that native capability.
- The current preview is not production hosting and has no uptime guarantee.
