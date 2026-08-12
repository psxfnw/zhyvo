# Product backlog

## Upload trust boundary and operational metrics

Status: implemented in preview.

Room capacity is reserved transactionally before a presigned upload and released after cancellation, expiry, size mismatch, or content rejection. Photos are limited to 50 MiB, videos to 2 GiB, and each room to its database-configured file and storage quota. Room creation and upload operations have separate identity/IP rate limits.

After the client uploads directly to object storage, the API reads at most the first 64 KiB and validates actual JPEG, PNG, GIF, WebP, HEIC/HEIF, AVIF, MP4/MOV/M4V, WebM, or 3GP signatures before making media visible. A disguised or malformed object is removed, marked failed, and never enters the gallery. The admin panel exposes completed, in-progress, rejected, thumbnail-failed, and reserved-storage counters from PostgreSQL without inspecting room media.

## Report a problem

Status: implemented in preview.

A persistent but quiet `Повідомити про проблему` entry opens an in-app form with a required description and optional contact. With explicit consent, Zhyvo attaches only technical context needed for diagnosis: sanitized route, app build, platform/browser, Telegram Mini App flag, network state, latest API error code, request ID, and timestamp. It never attaches room media, PIN/password values, Telegram init data, access tokens, invitation tokens, room codes, or filenames. Submissions enter a rate-limited server-side inbox and receive a reference such as `ZHY-AB12CD34`.

The internal `/admin/reports` panel is protected by a server-side Telegram ID allowlist. It combines the report queue, statuses, internal notes, and real PostgreSQL metrics for active rooms, ready media, stored originals, users, and today's activity. New reports are placed in a durable Telegram outbox; the bot sends each configured administrator a private notification with a button that opens the exact report inside the Mini App.

## Managed room invitations

Status: implemented in preview.

Owners have separate revocable links for contributors and view-only guests. A view-only member can browse, favorite, and save originals but the API—not only the interface—rejects uploads. Replacing a link immediately invalidates its old token without removing anyone already in the room. Existing room-code links remain available during migration and can be permanently disabled by the owner. Active members can share the current contributor link, while only the owner can create, rotate, or revoke invitations.

## Event recap

Status: implemented in preview.

Every non-empty room has a compact end-of-event page with total files, participants, contributors, favorites, image/video split, original storage size, and server-ranked highlights across the complete gallery. It links back to each highlighted frame and uses the same device-aware save flow as the gallery: asynchronous ZIP on desktop and native batched sharing or per-file saving on mobile.

## Expiry warnings and Telegram reminders

Status: implemented in preview.

Every room member receives an in-room warning during the final six hours, with the exact deletion time and device-appropriate save action. Owners who opt into the existing Telegram notification setting receive durable reminders at six hours and one hour before deletion. Reminder rows use deadline-scoped deduplication, are rescheduled when the room name or lifetime changes, are removed when notifications are disabled, and never send after the room expires. Transferring ownership clears pending owner notifications and requires the new owner to opt in. A realtime-trigger fix also ensures cascaded membership deletion cannot block permanent TTL cleanup.

## New-room activation

Status: implemented in preview.

Immediately after room creation, the owner sees two concrete next actions inside the otherwise empty gallery: invite participants through the existing share/QR flow and select the first original media. The panel is driven by one-time navigation state, so refreshes, direct invitations, returning owners, and ordinary members are never interrupted. It dismisses as soon as files enter the upload queue.

## First-run onboarding

Status: implemented in preview.

New visitors see a compact three-step walkthrough that explains room creation, link/QR invitations, original-quality uploads, and permanent expiry before they commit to an action. Completion is stored locally, existing users with active rooms are not interrupted, and `Як це працює` in the home header reopens the walkthrough at any time. Direct room invitations remain direct and never place onboarding in front of joining.

## Media captions and details

Status: implemented in preview.

Uploaders can add, edit, or clear a plain-text caption of up to 300 Unicode characters; the room owner has the same capability for moderation. The full-screen viewer exposes the real uploader, capture and upload times, dimensions or video duration, format, original size, and gallery position. Caption changes reuse the durable `media_updated` realtime event and appear on other open devices without reloading.

## Live room synchronization

Status: implemented in preview.

PostgreSQL stores room changes in a durable, room-scoped event log and emits a transactional notification after commit. Each API process keeps one `LISTEN` connection and fans wake-ups only to subscribers of the affected room. Authenticated SSE clients resume with `Last-Event-ID`, receive heartbeats through buffering-disabled Nginx, and fall back to periodic gallery refresh when disconnected. Media, thumbnails, favorites, room settings, covers, and membership changes update without reloading; a compact shelf announces new files without moving someone who is browsing older frames.

## Favorite frames and room cover

Status: implemented in preview.

Every room member can add one private-to-identity favorite reaction per media item. The gallery exposes the total count, the current member's state, an `Обрані` view, and a popularity ordering. Existing batch selection lets users save just the resulting shortlist. A room owner can promote a ready image to the room cover; deleting that image safely clears the cover.

## Capture-date gallery timeline

Status: implemented in preview.

The client extracts capture time from common EXIF metadata before upload. The gallery groups media by event date with a safe fallback to upload time, while `Фото`, `Відео`, and `Усі` filters keep large mixed galleries scannable.

## Batch gallery actions

Status: implemented in preview.

Users can enter selection mode, select multiple gallery items, save them in the device-appropriate flow, and remove only the items they are authorized to delete. Destructive actions show the exact affected count.

## Room lifecycle controls

Status: implemented in preview.

Owners can rename a room, extend its lifetime up to three days from creation, change access protection, pause uploads, close joining, inspect activity, remove or unblock members, and transfer ownership. Closing joining does not remove current members.

## Reliable upload queue

Status: implemented in the preview.

The mobile-first queue uploads two files concurrently, retries transient API and object-storage failures with exponential backoff, waits for the network to return, persists multipart checkpoints, and supports pause, resume, cancel, and cross-reload file recovery. It shows byte-weighted overall progress and rejects unsupported media, files over 2 GiB, and duplicate name/size pairs already present in the active queue.

## Content-based duplicate prevention

Status: implemented in preview.

The browser calculates SHA-256 in a dedicated Web Worker using bounded 8 MiB chunks. The API checks the digest while holding the room upload lock, and PostgreSQL enforces a partial unique index for active media. Identical bytes are rejected before any object-storage PUT even when filenames differ. A paused upload reuses its digest; after a reload, the reselected file is hashed again and must match the stored digest before multipart upload can resume.

## Download all media

Status: implemented in two device-specific flows.

Desktop uses an asynchronous, revision-cached ZIP in object storage. Mobile devices use the Web Share API to hand original files to the native system share/save sheet in bounded batches (up to 10 files or 128 MiB); Telegram falls back to its native per-file download prompt when multi-file sharing is unavailable.

Future native Capacitor builds should replace Web Share with direct Photo Library / Files APIs while preserving the same batched UI and backend download-url contract.

## Browser identity linking through the Telegram bot

Status: implemented in the preview as the primary browser sign-in flow. Telegram OIDC remains an optional fallback.

The browser creates a short-lived, single-use linking challenge and displays both a QR code and an `Open Telegram` deep link. The user confirms the request inside `@zhyvoappbot` or its Mini App. The browser polls a status endpoint and exchanges the approved challenge for a Zhyvo session, preserving rooms and uploaded media from the anonymous browser identity.

Security requirements:

- Store only a hash of the single-use challenge, expire it within five minutes, and invalidate it immediately after exchange.
- Bind the challenge to the initiating anonymous identity and show the browser/device plus approximate request time before confirmation.
- Require an explicit approve/deny action in Telegram; never approve merely by opening a link.
- Rate-limit creation, polling, confirmation, and exchange endpoints; keep a security audit event for each result.
- Do not request Telegram phone, chat access, or permission to message the user as part of identity linking.
- Provide Zhyvo session management with current-device logout and revoke-all-devices actions.

This flow avoids creating a persistent `Telegram Widgets` browser session, makes the trust boundary visible to the user, and fits the product better because Telegram already acts as Zhyvo's verified companion app.

## Opt-in Telegram room notifications

Status: implemented in preview.

Room owners with a linked Telegram identity can opt in per room. Member joins are delivered immediately; file uploads are grouped into a short per-uploader digest. Delivery uses a PostgreSQL transactional outbox processed by the existing worker with bounded retries, so notification failures never roll back joins or media uploads.

## Mobile media compatibility

Status: implemented in preview.

The thumbnail worker uses FFmpeg/FFprobe with explicit autorotation, bounded processing time, display-aware dimensions and video duration metadata. Video posters are sampled near 10% of the clip to avoid black opening frames. HEIC/HEIF files use a libheif fallback and remain downloadable in original quality while browsers receive a compatible JPEG preview. Command errors are sanitized before persistence so presigned storage URLs never enter diagnostic fields.

## Next product candidates

These are intentionally not part of the current MVP release gate:

1. Native Photo Library / Files integration after the Capacitor wrapper is introduced.
2. Storage and upload analytics before paid plans or larger limits are introduced.
3. Server-side paginated ranking once rooms regularly exceed the first 50 loaded media items.
