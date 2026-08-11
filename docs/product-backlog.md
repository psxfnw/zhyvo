# Product backlog

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
