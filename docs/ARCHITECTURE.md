# Lanvia Architecture

> Applies to v0.14.x. Product features are in [PRODUCT.md](PRODUCT.md) and the categorized feature table in [FEATURES.md](FEATURES.md); building, testing and releasing are in [DEVELOPMENT.md](DEVELOPMENT.md).

## 1. Overview

```
 iPhone ─┐  SSE + HTTP (encrypted)          signed HTTP relay (encrypted)
 iPad  ──┼──────────────▶  Mac node  ◀──────────────────▶  Windows node ◀── Android browser
         │                    ▲                                  ▲
         └ scan to pair        └────── UDP broadcast discovery (8687) ──┘
```

- Every computer runs a **peer node** (Go, single process, web assets embedded with `go:embed`). There is no central server.
- The desktop app runs the node in the same process; its Wails windows load the page the node serves at `http://localhost:8686`, so the desktop windows and phone browsers use the same front end.
- Phones and tablets are browser clients only. They connect to one computer node, receive pushes over SSE and send over HTTP.

### Stack

| Layer | Choice |
| --- | --- |
| Node | Go 1.26, standard `net/http` (1.22+ routing); the only dependency is `rsc.io/qr` |
| Desktop shell | [Wails v3](https://v3.wails.io) (beta): tray, windows, notifications, launch at login, single instance |
| Front end | Plain JS (no framework, no build step), split into several `<script>` files sharing the global scope |
| Clipboard | macOS: NSPasteboard via cgo (falls back to `pbpaste`/`pbcopy` without cgo); Windows: Win32 API; Linux: `wl-clipboard` / `xclip` / `xsel` |
| Crypto | Go standard library AES-GCM / HMAC-SHA256; the web page ships its own pure JS implementation (http pages have no `crypto.subtle`) |

## 2. Repository layout

```
cmd/lanvia/             CLI entry point: flags, start the node, print the pairing code and QR code
cmd/lanvia-desktop/     Desktop app (Wails v3)
  main.go               tray, quick panel, on-demand main / preview windows, tray menu
  notify.go             notifications (merging, Copy / Show in folder actions)
  updater.go            update checks, background download, install when idle
  i18n.go lang_*.go     tray menu translations, system language detection
  icon_*.go             application icon
  assets/               tray icons
  build/                Info.plist, icons, Mac installer templates (pkg/), DMG install guide
core/                   The node (shared by both entry points)
  node.go               public API: Open / Start / Close, Options, version
  app.go                App: config, the single entry point for content changes (ingest / remove / removeMany / clear / merge), online devices, periodic cleanup
  config.go             config file, Settings switchable from the page
  store.go              history store: in-memory list + per-file persistence, incremental snapshots
  server.go             HTTP routes, auth middleware, handlers
  auth.go               pairing code, key derivation, request signatures v1/v2, anti-replay, local admin check
  crypto.go             framed AES-256-GCM streams, encrypted response writer
  peers.go              UDP discovery, probing and heartbeats, history sync, relaying, manual devices
  compat.go             forward compatibility: keep and forward unknown fields / operations
  delivery.go           delivery receipts
  hub.go                SSE clients and broadcast
  replicate.go          fetching files across nodes, background backup
  storage.go            storage usage and quota
  files.go              save to Downloads/Lanvia, open / show in folder
  versions.go           edits and version history
  todo.go tags.go comments.go   checklists, tags, comments and their merge rules
  search.go             full-text search of long text
  thumb.go              image thumbnails (EXIF orientation)
  clipboard*.go         clipboard watcher and per-platform read / write
  clipimage.go          clipboard images, Windows DIB ↔ PNG
  update.go             desktop update state (download / install live in the desktop app)
  netutil.go            LAN address enumeration (excluding VPN / virtual adapters)
  reuse*.go             UDP port reuse (several processes can receive broadcasts)
  banner.go lang*.go    terminal output, log language
  web/                  built-in front end (section 9)
internal/update/        automatic updates: find releases, download and verify SHA-256, replace the app, relaunch
tools/genicons/         generate app and tray icons from source.png
scripts/build.sh        build every release artifact into dist/
scripts/release.sh      publish to GitHub Releases
```

## 3. Node lifecycle

```go
app, _ := core.Open(core.Options{...}) // read / create config, open the store; not listening yet
app.Start()                            // listen on HTTP, start background tasks
defer app.Close()                      // flush unsaved history
```

Background tasks started by `Start`:

| goroutine | Period | Purpose |
| --- | --- | --- |
| `peers.listen` / `beaconLoop` | 3 s | receive / send UDP beacons (not started with `--no-discovery`) |
| `peers.pollLoop` | 4 s | refresh connected nodes' `/api/info` (3 failures in a row = offline), retry manual addresses |
| `clip.run` | 0.5 s | poll the clipboard (macOS / Windows compare the change count first, so it costs almost nothing) |
| `presenceLoop` | 1 s | push the online device list to pages when it changes |
| `pruneLoop` | 1 min | remove expired items, auto-resume pause; quota and missing downloads every 5 min |
| `fetchLoop` ×2 | queue | download files from other devices in the background (two workers so big files don't block small ones) |
| `store.saver` | 300 ms after a change | coalesce writes to disk |

## 4. Data model

### 4.1 Item

`Item` in `core/store.go`, main fields:

| Field | Meaning |
| --- | --- |
| `id` | random 16 hex chars (validated by `^[a-f0-9]{8,64}$`) |
| `kind` | `text` / `file` / `todo` |
| `text` / `name` / `mime` / `size` | body; file name, type, size |
| `from` / `origin` / `ts` | display name of the source device; node ID holding the original file; creation time (ms) |
| `rev` / `editedAt` / `editedBy` | number of edits, time and author of the current version |
| `expiresAt` / `keep` / `metaTs` | per-item retention and when it was changed |
| `fav` / `title` / `favTs` | favorite, snippet name and when it was changed |
| `tags` | tag → timestamp (positive = added, negative = removal tombstone) |
| `todo` / `textTs` | checklist entries; when the checklist title was changed |
| `comments` | number of live comments (computed locally, never trusted from peers) |
| `textLen` | only in copies pushed to pages: full length when the body is truncated (>16 KB) |
| `Extra` | fields this version doesn't know, kept and forwarded as-is (section 8) |

### 4.2 On-disk layout

The data directory defaults to `Lanvia/` under the system config directory (macOS `~/Library/Application Support/Lanvia`, Windows `%AppData%\Lanvia`, Linux `~/.config/Lanvia`) and can be set with `--data`.

```
config.json            device ID, name, pairing code, port, manual devices, token, Settings (0600)
history.json           index: item metadata and short bodies, deletion tombstones, clear time, each item's local "last changed" time
texts/<id>.txt         bodies over 16 KB
versions/<id>.json     edit history (only for edited items)
comments/<id>.json     comments (including deletion tombstones)
blobs/<id>             file contents (original or copy)
blobs/<id>.*.part      temporary files for downloads / uploads (removed after 24 h)
blobs/thumbs/<id>.jpg  thumbnails (480 px on the long side)
```

**Write order**: changed side files (bodies / versions / comments) are written first; only when all succeed is `history.json` replaced atomically (write `.tmp`, then rename). So after a power cut every file the index references exists; if a side file fails, the index isn't updated and the write is retried. On startup, files without a matching item are removed and old formats (long bodies and versions inside `history.json`) are migrated.

### 4.3 In-memory structure and concurrency

- `Store.items` is sorted by `TS`, newest first, with `byID` as the index; `indexOf` looks up the time by ID and then binary-searches.
- Items are **copy-on-write**: a change replaces the `Item` with a modified copy, so pointers returned by `List()` / `Get()` are safe to read outside the lock.
- `App.cfg` is guarded by `App.mu` (RWMutex); read with `cfgSnapshot()`, change with `updateCfg()`, which also re-derives keys, pushes a `settings` event and saves.
- Quota work (`reserve` / `makeRoom`) is serialized by `quotaMu` so concurrent downloads can't exceed the limit together.

### 4.4 Retention and cleanup

- `expired()`: favorites, keep-forever items and checklists never expire; `expiresAt` wins when set; otherwise the local global retention applies.
- Global retention is local only — **no tombstones, no notifications to peers**; `Store.Add` rejects expired items, so they don't sync back.
- Above 300 items, the oldest non-pinned items are removed.
- Deletion tombstones are kept for 7 days.

## 5. Networking and sync

### 5.1 Discovery

- Every 3 seconds a UDP beacon (port 8687) goes to `255.255.255.255` and each physical adapter's subnet broadcast address:
  `{"app":"lanvia","v":1,"id":…,"name":…,"os":…,"port":8686,"space":<space ID>}`
- Space ID = `hex(SHA256("lanvia-space:"+code))[:16]`: public and irreversible.
- On a beacon from the same space, the node probes the sender with a signed `/api/info` request, which only passes signature verification if the pairing codes match. Other spaces are recorded as "nearby devices" (dropped after 15 s unseen).
- The UDP port uses `SO_REUSEADDR` / `SO_REUSEPORT` (`reuse*.go`) so several processes on one computer can all receive broadcasts.
- LAN address enumeration excludes VPN, container, VM and proxy fake-ip adapters (`netutil.go`) and ranks private addresses on common physical adapters first.

### 5.2 Node connections

- **Manual addresses** are saved in the config; `pollLoop` retries unconnected ones every 4 seconds.
- **Connecting back**: node requests carry `X-Lanvia-Port`; when a node with the same code that we don't know yet sends us a signed request, we connect back (`noticeCaller`), so a manual address only has to be added on one side.
- **Signature negotiation**: v2 by default; on a 401 the node retries once with v1 (compatibility with v0.7 and earlier) and sticks to v2 once the peer's `/api/info` reports `auth: 2`.

### 5.3 Relaying changes

Every content change goes through a few `App` methods (`ingest` / `remove` / `removeMany` / `clear` / `afterEdit` / `afterMeta` / `afterFav` / `afterTags` / `afterTodo` / `afterComment`), each of which:

1. writes to the Store;
2. pushes to the browsers connected to this node over SSE;
3. wraps the change in an `Envelope` and POSTs it to the other nodes' `/api/relay`.

```go
type Envelope struct {
    Op      string       // add | delete | clear | edit | meta | fav | tags | todo | comment | sync
    EID     string       // message ID for de-duplication
    Item    *Item
    ID      string
    TS      int64
    Version *TextVersion // edit
    Comment *Comment     // comment
    Snap    *storeFile   // sync: a batch merged from elsewhere that must be forwarded
}
```

**Flooding with de-duplication**: a node only forwards changes it sees for the first time (`add` by item ID; other operations because the merge function reports whether anything changed; unknown operations by `EID`), and never back to the sender. The network therefore needn't be fully connected (e.g. a manual chain C–A–B) and loops are impossible.

**Bulk delete** (`removeMany`): writes one tombstone per item, sends the page a single `deletes` event, and relays one `delete` envelope per item through `relaySeq` — one goroutine per peer sending them in order, so thousands of deletions don't open thousands of connections. Old versions understand plain `delete`. If a peer fails one message, the rest of that sequence is not sent to it; it gets the tombstones the next time it pulls our history after reconnecting.

### 5.4 History sync

- When a node connects, we pull its `GET /api/history` and `merge()` it: missing items are added, metadata merged, the clipboard untouched.
- **Incremental sync**: the Store records each item's "last changed here" time (`seen`, local clock). The response includes `now`; the next request sends `?since=<previous now − 30 s>` and only gets later changes. All times come from the responder's own clock, so clock skew between devices doesn't matter. Old versions ignore `since` and return everything.
- Newly merged items are forwarded to other nodes as a `sync` operation (in 1 MB batches), so history also crosses chained topologies.

### 5.5 Merge rules for concurrent changes

All rules are **deterministic**: every node ends up with the same result whatever order it receives things in.

| Data | Rule |
| --- | --- |
| Items | De-duplicated by ID; deletion writes a tombstone (7 days); clear records a point in time and rejects anything older |
| Edits (`versions.go`) | The version set only grows and is unioned by ID; the current text is the version with the largest `(TS, ID)`. The original text becomes the first version on the first edit, with the item's ID, so every node generates the same one. Up to 50 versions per item |
| Per-item retention | Newer `metaTs` wins; on a tie keep-forever wins, then the later expiry |
| Favorites | Newer `favTs` wins; on a tie favorited wins, then the larger title |
| Tags (`tags.go`) | Per tag, the larger absolute timestamp wins; on a tie "added" wins |
| Checklists (`todo.go`) | Entries unioned by ID; text and checked state each take the newer change; deletions are tombstones (earliest deletion time); title by `textTs` |
| Comments (`comments.go`) | Unioned by ID; deletions are tombstones and the earlier non-zero deletion time wins; up to 200 per item |

### 5.6 File transfer and copies

- Upload: the file is written to the receiving node's `blobs/`; that node becomes the `origin` (holder of the original).
- Download (`GET /api/blob/{id}`): served locally if present (Range supported); otherwise the node asks the origin, then other nodes (`X-Lanvia-Hop: 1` prevents a second hop), forwarding the response while caching a copy.
- Background backup (`replicate.go`): file entries from other nodes within the auto-download limit and from the last 3 days are queued; failures aren't retried for 2 minutes, and missing files are re-queued every 5 minutes.
- Quota (`storage.go`): `reserve(size)` before downloading, deleting the oldest copies first if needed (pinned last); originals are never removed automatically. After "Clear copies", files aren't re-downloaded during that run.

### 5.7 Delivery receipts (`delivery.go`)

- When a locally created item is sent, there is one target per online node (`peer:<id>`) and per remote browser (`web:<id>`), all `pending`.
- Nodes: a 200 from `/api/relay` means `received`, otherwise `failed`; `blocked` when encrypted-only is on and the peer can't encrypt.
- Browsers: after the `delivery-request` event they call `POST /api/deliveries/ack`; no confirmation within 10 s means `failed`. A browser reconnecting after a short drop keeps its client ID.
- Only the last 128 receipts are kept, in memory, never synced; "retry" contacts only unconfirmed targets.

## 6. HTTP API

All endpoints are on the node's HTTP port (8686 by default). Auth is described in section 7:

- **auth**: local admin (localhost), a signed request (other nodes / phone browsers) or a Bearer token.
- **local**: the local management page only.

| Method and path | Auth | Description |
| --- | --- | --- |
| `GET /` and static assets | none | built-in front end |
| `GET /api/time` | none | server time (the page calibrates its clock) |
| `GET /api/info` | auth | node info; local requests also get the pairing code, token, settings, devices, storage, … |
| `GET /api/events` | auth | SSE: `hello` (full snapshot), `item`, `update`, `delete`, `deletes`, `clear`, `devices`, `settings`, `delivery`, `delivery-request`, `updater` |
| `GET /api/history?since=` | auth | history snapshot (node-to-node sync) |
| `POST /api/relay` | auth | receive an `Envelope` from another node |
| `GET /api/latest` | auth | latest text item (plain text) |
| `POST /api/clip` | auth | send text: JSON `{text, from}` or a plain-text body |
| `POST /api/upload?name=&type=&from=` | auth | upload a file (body = file contents, up to 4 GB) |
| `GET /api/blob/{id}?dl=1` | auth | download a file, Range supported |
| `GET /api/thumb/{id}` | auth | image thumbnail |
| `GET /api/items/{id}` | auth | full item |
| `DELETE /api/items/{id}` | auth | delete |
| `POST /api/items/delete` | auth | bulk delete `{ids: [...]}` (up to 5,000), returns `{count}` |
| `POST /api/items/{id}/edit` | auth | edit `{text, from}` |
| `GET /api/items/{id}/versions` | auth | version history |
| `POST /api/items/{id}/fav` | auth | favorite `{fav, title}` |
| `POST /api/items/{id}/expiry` | auth | per-item retention `{ttl seconds, keep}` |
| `POST /api/items/{id}/tags` / `GET /api/tags` | auth | add / remove tags `{add, remove}` / all tags in use |
| `GET/POST /api/items/{id}/comments`, `DELETE …/comments/{cid}` | auth | comments |
| `POST /api/todo`, `POST /api/items/{id}/todo` | auth | create a checklist / change entries |
| `GET /api/search?q=` | auth | full-text search of long text |
| `POST /api/clear` | auth | clear all history up to now (every device) |
| `POST /api/deliveries/ack`, `POST /api/deliveries/{id}/retry` | auth | delivery receipt, retry |
| `POST /api/settings` | local | change settings (only the fields being changed) |
| `POST /api/join` | local | switch code `{code}` or regenerate `{reset: true}` |
| `POST /api/peers` | local | add / remove a manual device `{addr, remove}` |
| `POST /api/pause` | local | pause `{minutes}`: >0 minutes, -1 until resumed, 0 resume |
| `GET /api/clipboard`, `POST /api/send-clipboard` | local | local clipboard state, send manually |
| `POST /api/copy/{id}` | local | write an item to the local clipboard |
| `POST /api/save/{id}?then=open\|reveal`, `POST /api/reveal` | local | save to Downloads/Lanvia, open or show in folder |
| `POST /api/storage/clear-replicas` | local | clear copies |
| `POST /api/token/reset` | local | regenerate the token |
| `GET /api/qr?text=` | local | QR code bitmap |
| `GET /api/update`, `POST /api/update/{check\|install}` | local | update state / actions (desktop) |
| `POST /api/desktop/{action}?id=` | local | ask the desktop shell to act: `hide`, `main`, `preview`, `edit`, `history`, `comments`, `pin`, … |

Every response carries `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY` and `Referrer-Policy: no-referrer`.

## 7. Security design

### 7.1 Key derivation (`auth.go`)

The pairing code (10 characters from a 31-character alphabet, ~50 bits) **never travels over the network**.

```
key   = SHA256("lanvia-key:"   + code)          // HMAC signing key
space = hex(SHA256("lanvia-space:" + code))[:16] // public space ID
token = hex(HMAC(key, "lanvia-api-token"))[:32]  // automation token (can be replaced by a random regenerated one)
enc   = HMAC(key, "lanvia-content-encryption")  // content encryption key
```

### 7.2 Request signatures

- **v2** (since v0.8): `HMAC(key, "lanvia2\n" + ts + "\n" + nonce + "\n" + method + "\n" + path + "\n" + canonical query)`, in headers `X-Lanvia-TS / -Nonce / -Sig / -Auth: 2`; for EventSource and download links, which can't set headers, in query parameters `_t / _n / _s / _v`. The query can be encrypted as a whole into `_q`.
- **v1** (compatibility): signs only `ts / method / path`, no replay protection.
- Timestamps are valid for ±5 minutes (the page calibrates with `/api/time` first).
- **Anti-replay**: a v2 write request's nonce can be used once within the validity window (`nonceCache`); replays get 409.
- **Local admin** (`isLocalAdmin`): loopback source, `Host` is localhost / 127.0.0.1 / ::1, and for writes `Origin` matches Host — protecting against DNS rebinding and cross-site requests.
- **Token**: `Authorization: Bearer`, constant-time comparison; can be disabled in settings.

### 7.3 Content encryption (`crypto.go` / `web/crypto.js`)

- Framed AES-256-GCM stream: a random 12-byte nonce prefix, then frames of `[4-byte length, top bit = final frame][ciphertext]`, at most 64 KB of plaintext each.
- Frame i's nonce = prefix with its last 8 bytes XOR i; additional data = frame number + final flag. Tampering, reordering and truncation are all detected, and large files and Range requests never need to be held in memory.
- Between nodes: request bodies encrypted (`X-Lanvia-Body-Enc: 1`), responses encrypted (`X-Lanvia-Enc: 1`).
- Web: responses to v2-signed requests are always encrypted (except `_plain=1`, used for files over 256 MB on phones and refused with encrypted-only); SSE event data is encrypted per event. The page's SHA-256 / HMAC / AES-GCM are pure JS, with large files handled in a Web Worker (`crypto-worker.js`); `webcrypto_test.go` checks Go ↔ JS interoperability with node.
- **Encrypted connections only** (`strictEnc`): refuses unencrypted old nodes and rejects plaintext responses to our own requests, so a man in the middle can't strip encryption.

### 7.4 Other protections

- Uploaded files are served with `Content-Security-Policy: sandbox`; SVG / HTML are always attachments.
- File names go through `sanitizeName`; "show in folder" only accepts paths inside `Downloads/Lanvia`.
- Endpoints reachable by other devices limit request body size (uploads 4 GB, text 2 MB, everything else 4 MB or less).
- The pairing code in the QR URL is after `#` and never sent to the server.

### 7.5 Known limitations

See [PRODUCT.md section 5](PRODUCT.md#5-security-for-users): the page is served over http and can be altered by an active attacker; the token and its content are plain text.

## 8. Forward compatibility (`compat.go`)

So that future features survive passing through older devices:

- `Item` has custom JSON encoding: unknown fields are kept in `Extra` (at most 32 fields / 16 KB per item) and carried when saving, relaying and syncing; when we lack them and a peer has them, they are adopted (`adoptExtra`).
- An unknown `Envelope.Op` with a valid `EID` is forwarded to other nodes unchanged, de-duplicated by EID.

**Rule for new synced features**: put per-item state in new `Item` fields or use a new operation carrying an EID, and make the merge rule deterministic (the same set of inputs merged in any order gives the same result).

## 9. Front end (`core/web/`)

No framework, no build step. `index.html` loads the scripts in order; later scripts use globals defined by earlier ones:

| File | Responsibility |
| --- | --- |
| `i18n.js` | Chinese / English: Chinese is the source text; English is applied by translating text nodes and attributes from a dictionary |
| `highlight.js` | lightweight code highlighting |
| `zip.js` | zip a folder in the browser |
| `crypto.js` / `crypto-worker.js` | pure JS SHA-256 / HMAC / AES-GCM; background thread for large files |
| `base.js` | helpers, signing and request wrappers (`prepare` / `api` / `signedURL`), device detection, icons, clipboard |
| `delivery.js` | delivery receipt state and display |
| `list.js` | list rendering, search, SSE connection, sending text / files / folders |
| `preview.js` | preview: view, highlight, edit, version history and diff |
| `features.js` | send mode, dialogs, favorites, per-item retention, storage and manual devices, text size |
| `settings.js` / `updater.js` | settings, pairing QR code and flow; About & updates |
| `inbox.js` | main window and browsers: stream cards (click to copy), device beam, sync capsule menu, Omni-Bar (type to filter, Enter to send, list detection), drawer tabs and actions, multi-select / bulk delete / clear, settings categories |
| `window-chrome.js` | Windows integrated title bar |
| `main.js` | event wiring, quick panel, language and startup (loaded last) |
| `style.css` / `inbox.css` / `panel.css` | design tokens and base components / single-canvas stream layout and drawer / tray panel |

**Layout** (see [FRONTEND_GUIDE.md](FRONTEND_GUIDE.md)): the main window and browsers show one centered stream — a frosted top bar with the device beam and the `SYNC · E2E` capsule, a sticky Omni-Bar, filter pills and cards; `#previewDlg` opens as a right-hand drawer (full screen below 650 px, where the Omni-Bar floats at the bottom). The tray panel and the standalone preview window reuse the same elements with their own CSS.

**Mode** comes from the query string: none = browser (phone or local dashboard); `?desktop=panel` tray quick panel; `?desktop=main` main window (plus `&chrome=windows` on Windows); `?desktop=preview&id=…&mode=view|edit|history|comments` preview window. The page asks the desktop shell for window actions via `POST /api/desktop/{action}`.

**Pairing**: the QR URL is `http://<ip>:8686/#k=<code>`; the page reads and removes it from the address bar, derives the keys locally and stores them; from then on every request is signed and encrypted in the browser.

**Truncated bodies**: each item in the pushed list carries at most 16 KB of text (`textLen` holds the full length); preview and edit fetch the full text with `GET /api/items/{id}`, and long-text search goes through `/api/search`.

## 10. Desktop app (`cmd/lanvia-desktop/`)

- **Windows**: only the tray quick panel (380×600, frameless, hides on blur, always on top) is created at startup. The main and preview windows are created on first use (each is a full web page and would cost memory if kept around) and read through `openedWindow()` under `winMu`. Closing a window only hides it.
- On macOS the app uses `ActivationPolicyAccessory` (menu bar only, no Dock icon) and installs a hidden Edit menu so ⌘C/⌘V/⌘A work in the WebView.
- **Single instance**: launching again opens the running instance's main window.
- **Tray menu**: rebuilt on the node's `OnChange` callback (coalesced over 300 ms).
- **Notifications**: from the node's `OnIncoming` callback; items within 800 ms are merged; suppressed while any Lanvia window is in front.
- **Automatic updates** (`updater.go` + `internal/update`):
  - Finding releases: follow the redirect of `github.com/<repo>/releases/latest` to get the tag, then read that release's `SHA256SUMS.txt` to list its packages (avoiding the GitHub API limit of 60 requests per hour); fall back to the API on failure.
  - Packages: the universal `Lanvia-macos.zip` on Mac, `Lanvia-windows-<arch>.exe` on Windows.
  - Replacement: on Mac, unzip, rename the old app to a backup, move the new app in place, delete the backup, restoring on failure; without write permission, replace through `osascript` with administrator rights and hand ownership back to the user. On Windows, rename the running exe to `.old` (deleted on next start) and move the new one in. A background shell then relaunches the new version after this process exits.
  - A version whose automatic install failed is recorded as `failedTag` and not retried automatically.
  - `LANVIA_UPDATE_URL` can point at a fake release endpoint for testing.
