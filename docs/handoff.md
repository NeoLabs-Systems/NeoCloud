# Send to device

Send files from one of your devices to another, signed in to the same account,
anywhere: phone to desktop, desktop to browser, browser to phone. It needs no
shared network. The route reference is in [api.md](api.md#send-to-device-apihandoff).

## Security, plainly

- Transport is TLS, like everything else in NeoCloud.
- The files pass **through the server**. While they wait they are staged as
  ordinary blobs, **encrypted at rest with the account key the server holds**,
  the same as your other files. This is **not end-to-end encryption between
  your devices**: the server (and so its administrator) can read a file in
  transit just as it can read your other files, apart from vaults. Files in
  vaults cannot be sent.
- An offer is addressed to exactly one of your own devices. Another account
  gets `404` for every id. Another device of the same account cannot see the
  offer in its lists or download its files; only the sender sees its status.
- The staged files are deleted as soon as the offer is delivered, declined,
  cancelled or expired, or the receiving device is signed out.
- File names are sanitised on the server for every receiver: no path
  separators, control or bidirectional characters, no Windows reserved names,
  at most 200 bytes, unique within the send. Receivers still confine what they
  write to the folder the person chose (the desktop client re-checks).
- The audit log gets one entry per send (`handoff.sent`: counts, bytes, kind of
  receiving device); never file names.

## Model

- **Presence.** A device is online while it holds a live connection:
  the web app through the socket.io socket (one device per browser session,
  however many tabs), signed-in apps through `GET /api/handoff/events`
  (server-sent events, bearer token). Native apps have no socket.io client, so
  they use the event stream; both register in the same in-memory presence
  module, and a restart empties it until clients reconnect. Presence is per
  account; nothing is visible across accounts.
- **Offer.** Created for one target device. Files are uploaded to a staging
  area (or an existing NeoCloud file is referenced: a copy that shares its
  blob). Statuses: `uploading` → `pending` → `accepted` → `delivered`, or
  `declined`, `cancelled`, `expired`.
- **Delivery.** The receiver is pushed `handoff:offer`. A device that is
  offline but was seen in the last 7 days can still be chosen; the offer stays
  `pending` and is pushed when it connects. The receiver accepts (or declines),
  downloads (ranged, resumable; or one ZIP) and confirms with `complete`. A
  browser never confirms, so the server also counts a send as delivered once
  every file was streamed to its last byte.
  Downloads are authorised by the offer itself: it must be addressed to the
  asking device and accepted, so there is no separate link that could leak.
- **Limits.** 50 files and 2 GiB per send; 10 open sends and 4 GiB staged per
  account; 24 h to be received (1 h to finish uploading). Staged bytes **count
  against the account's quota while they wait** (they are normal blobs) and are
  released the moment the send ends; sending an existing file costs nothing.
  Finished sends stay listed for the sender for 3 days.
- **Revoking a device** closes its connections and cancels every open send to
  or from it. Deleting the account removes all sends and staged files. The
  account export does not include sends (they are transient).

## Clients

- **Web:** sends (pick files, or "Send to device" on files in NeoCloud) and
  receives. An incoming send opens a dialog; with browser notifications on
  (Settings, Devices; permission is asked only when you switch it on) it also
  shows a browser notification. Accept downloads a single file directly and
  several files as one ZIP, because browsers ask before a page starts many
  downloads.
- **Phone app:**
  - *Android:* NeoCloud is in the share menu for any file type, several files
    allowed. The app copies the shared files into its private cache (streaming)
    and shows "Send to…" (online devices, live) beside "Save to my NeoCloud"
    (the normal folder picker and upload queue). Plain text with no file
    behind it is not taken.
  - *iOS:* the Dart side is in place, but the system share menu needs a Share
    Extension, which is a new Xcode target that has not been added (it cannot be
    verified without building iOS). Until then iOS can send from inside the app
    ("Send to device" on a file, or "Choose files").
  - Receiving works while the app is open: a dialog, then each file goes
    through the system's save dialog. There are no push notifications for a
    closed app yet. "Let my other devices send files here" in Settings, Devices
    keeps the phone out of other devices' lists.
  - Uploading a send is one streamed request per file (not resumable); the
    download side is resumable. A failed upload cancels the send.
- **Desktop (lite client, `clients/lite/internal/handoff`):** "Receive from my
  devices" in the tray menu (on by default; off keeps the computer out of the
  lists). An incoming send shows a notification (once per send) and a native
  dialog: Save to Downloads, Choose folder, or Decline. Without a dialog
  program on Linux (zenity or kdialog) it saves to Downloads. Downloads resume
  after a dropped connection, must match the announced size, never overwrite
  an existing file (`name (1).ext`), and every name is re-sanitised and
  confined to the chosen folder.
