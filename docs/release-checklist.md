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

- Open the Mini App from `@zhyvoappbot` and from a room invite link.
- Create a room, leave it, and confirm it remains under `Мої кімнати`.
- Join from a second Telegram account and confirm the room opens directly.
- Upload several photos, one video, and—when available—an HEIC photo.
- Confirm thumbnails have the right orientation and a video shows its duration.
- Download one original file and use the mobile multi-file save/share flow.
- Close joining and uploads independently; confirm existing members still see the gallery.
- Confirm the owner sees members and room activity, and can delete any media while a member can delete only their own.
- Verify Telegram notifications only after the owner explicitly enables them.

## Operational checks

- `docker compose --profile preview ps` shows healthy `api` and `frontend` services.
- `http://localhost:8080/health/ready` returns ready.
- No bot token, OIDC secret, Tailscale auth key, or real `.env` file is tracked by Git.
- PostgreSQL and object-storage volumes are backed up before any destructive migration.
- The preview limitation is understood: the public URL stops when this PC or Docker Desktop is off.

## Current MVP boundaries

- Rooms expire one to three days after creation; extending never exceeds the three-day cap.
- Browser downloads depend on platform capabilities. Desktop receives ZIP; mobile prefers native share/save in bounded batches.
- The browser cannot silently write many originals into the iOS/Android photo library. A future Capacitor client will provide that native capability.
- The current preview is not production hosting and has no uptime guarantee.
