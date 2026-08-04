# Product backlog

## Download all media

Status: discuss before implementation.

The MVP currently downloads original files one at a time. A room-level “Download all” action is useful, but the implementation affects server cost, reliability and mobile browser behavior.

Options to evaluate:

1. Backend ZIP streaming — simplest user experience, but expensive for CPU, temporary disk and outbound traffic on large rooms.
2. Asynchronous ZIP job in object storage — reliable for large rooms, but requires job state, expiration and duplicate-request control.
3. Client-side sequential downloads — cheap for the backend, but unreliable on iOS and browsers that block multiple downloads.

Recommended future direction: asynchronous ZIP generation with a short TTL, size limit and one cached archive per room snapshot. Keep it outside the current iteration until production storage and plan limits are finalized.
