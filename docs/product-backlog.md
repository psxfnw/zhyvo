# Product backlog

## Download all media

Status: implemented in two device-specific flows.

Desktop uses an asynchronous, revision-cached ZIP in object storage. Mobile devices use the Web Share API to hand original files to the native system share/save sheet in bounded batches (up to 10 files or 128 MiB); Telegram falls back to its native per-file download prompt when multi-file sharing is unavailable.

Future native Capacitor builds should replace Web Share with direct Photo Library / Files APIs while preserving the same batched UI and backend download-url contract.
