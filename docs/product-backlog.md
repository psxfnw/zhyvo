# Product backlog

## Download all media

Status: implemented in two device-specific flows.

Desktop uses an asynchronous, revision-cached ZIP in object storage. Mobile devices use the Web Share API to hand original files to the native system share/save sheet in bounded batches (up to 10 files or 128 MiB); Telegram falls back to its native per-file download prompt when multi-file sharing is unavailable.

Future native Capacitor builds should replace Web Share with direct Photo Library / Files APIs while preserving the same batched UI and backend download-url contract.

## Browser identity linking through the Telegram bot

Status: planned as the primary browser sign-in flow. Telegram OIDC remains an optional fallback.

The browser creates a short-lived, single-use linking challenge and displays both a QR code and an `Open Telegram` deep link. The user confirms the request inside `@zhyvoappbot` or its Mini App. The browser polls a status endpoint and exchanges the approved challenge for a Zhyvo session, preserving rooms and uploaded media from the anonymous browser identity.

Security requirements:

- Store only a hash of the single-use challenge, expire it within five minutes, and invalidate it immediately after exchange.
- Bind the challenge to the initiating anonymous identity and show the browser/device plus approximate request time before confirmation.
- Require an explicit approve/deny action in Telegram; never approve merely by opening a link.
- Rate-limit creation, polling, confirmation, and exchange endpoints; keep a security audit event for each result.
- Do not request Telegram phone, chat access, or permission to message the user as part of identity linking.
- Provide Zhyvo session management with current-device logout and revoke-all-devices actions.

This flow avoids creating a persistent `Telegram Widgets` browser session, makes the trust boundary visible to the user, and fits the product better because Telegram already acts as Zhyvo's verified companion app.
