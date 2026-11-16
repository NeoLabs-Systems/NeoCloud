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
    through the system's save dialog. "Let my other devices send files here" in
    Settings, Devices keeps the phone out of other devices' lists (and is
    remembered across restarts). With the app closed or in the background, see
    "Receiving with the app closed" below.
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

### Sending from the desktop (lite client)

The lite client sends too, to any other device that is online and accepting
files (the list is the server's, minus this computer). Sending does not depend
on "Receive from my devices": that switch only decides whether others may send
*here*. Only files go: a folder is refused with a message (put it in a zip
file), as are more than 50 files or 2 GiB per send. Every file is streamed
from disk as one request, so a large file never sits in memory; a failed or
cancelled upload cancels the send on the server, so nothing stays on the other
device. The client follows the send until `delivered`, `declined`, `expired`
or `cancelled` (the event stream wakes it; it polls every few seconds as a
fallback).

- **Tray menu, "Send files to device…"** (the same on Windows, macOS and
  Linux): a native file dialog (several files), then a native device list of
  the devices online now; with exactly one the question is a one-click
  "Send x to Pixel 8?"; with none, "No other device is online right now". The
  send runs in the background. A notification tells how it ended ("Delivered
  to Pixel 8", "Declined", "Not received"); with notifications set to
  "Everything" there is also one when it starts, and with "Off" none. On Linux
  the dialogs need zenity or kdialog; without either the client sends to the
  only online device without asking and otherwise says to use the command line.
- **App window, "Send to device" card:** drop files on the window or "Choose
  files", then press Send beside an online device (live list). Each send shows
  its progress, can be cancelled, and the last results stay until dismissed.
  Dropped files are copied into a private temporary folder first (a browser
  never reveals a file's path) and deleted once the send ended.
- **Command line:** `neocloud-lite send [--to NAME|ID] [--no-wait] <file>...`
  with the stored sign-in, whether or not the tray app runs. With several
  devices online and no `--to` it lists them and asks on a terminal (without
  one it exits with an error); without a terminal at all (a file manager
  launched it) it asks in dialogs instead. It waits for the other device's
  answer and exits non-zero unless the files were delivered (`--no-wait`
  returns after the upload). Ctrl-C cancels the send.
- **File manager integration (opt-in)**, Settings, "Add to the right-click
  Send to menu": on Windows a "Send to my NeoCloud device" shortcut in the
  Send to menu; on Linux "Send to my device" in Open with (and the launcher,
  where it opens the file dialog), a Nautilus and a Nemo script and a Dolphin
  service menu. All are per-user files that run `neocloud-lite send`; nothing
  needs administrator rights. **macOS has none**: a Finder extension or Quick
  Action needs a signed app, and this one is ad-hoc signed; use the tray menu,
  the window or the command line.

## Receiving with the app closed (Android)

Opt in under Settings, Devices, "Receive files in the background" (also offered
once, the first time you send or accept files). It needs no Firebase or any
push service, which a self-hosted server doesn't have: a foreground service
keeps the same event stream open that the app uses while it is open.

- **What the person sees.** A heads-up notification "Anna's laptop wants to
  send you 2 files" with Accept and Decline (tapping it opens the app, which
  asks in its own dialog). Accept downloads without opening the app: a progress
  notification, then "2 files received" with Open. The files land in
  Downloads/NeoCloud. A failure is a notification too ("Couldn't receive the
  files"). With the app open the dialog asks and no notification is shown; a
  notification is never shown twice for the same send (ids are remembered for
  three days, across service restarts and the 15-minute check).
- **Service.** `HandoffService` (`flutter_app/packages/neocloud_background`,
  `handoff/`) is a foreground service, type `remoteMessaging` on Android 14+
  (made for passing messages and files between a person's devices: no daily
  time limit like data sync's six hours on Android 15, and allowed to start
  after a reboot), `dataSync` on Android 10 to 13, untyped before. Its
  notification is silent and minimal ("NeoCloud is ready to receive files",
  the lowest importance). It reconnects with backoff (1 s doubling to 60 s),
  authenticates with the stored device token and sends `receive=1`, so the
  phone shows as online.
- **Fallback.** A WorkManager periodic job (every ~15 minutes, needs a network)
  fetches `GET /api/handoff/offers`, notifies for waiting sends and tries to
  bring the service back. Doze stops background network access for apps that
  aren't exempt from battery optimisation, so without the exemption (asked for
  in the permission sheet) offers may take up to ~15 minutes.
- **Stops cleanly.** Signing out, switching it off, switching "Let my other
  devices send files here" off, or the server answering 401 (token revoked)
  stops the service and the check, removes its notifications and forgets the
  token. A remote wipe's one-time `401 DEVICE_WIPE_REQUESTED` can reach the
  background receiver before the app: it is kept and the app erases itself the
  next time it starts.
- **Storage of the token.** The token is sealed with an Android Keystore key
  (AES-GCM) in the app's private preferences, never plain text; the app's own
  copy stays in the secure storage.
- **Download rules** mirror the desktop receiver: names re-sanitised (no path
  parts, control or bidirectional characters, FAT-forbidden characters, at most
  200 bytes), never overwriting (`name (1).ext`), confined to the folder
  (Android 10+ saves through MediaStore, which needs no storage permission;
  Android 9 and older use the app's own Downloads folder), a `.ncpart` file in
  the cache that grows with Range requests and moves into Downloads only when
  exactly the announced size arrived, more bytes than announced abort, retries
  continue from the part, already-saved files of a send are not saved again.
- **Permissions.** Notifications (Android 13+), the battery exemption and the
  switch itself are items in the same checklist sheet as camera upload
  (Settings, Camera upload, "Background reliability", or when turning the
  switch on); the sheet's title and text follow which features need it.
- **iOS.** Not possible without Apple's push service (APNs): iOS suspends the
  app and closes the connection, and a server can't wake it. Receiving works
  while the app is open, as before; the switch is not shown on iOS.

