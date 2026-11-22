# Lanvia Feature Inventory

> Applies to v0.14.x. Every feature, grouped by category, with the platforms it runs on, whether it syncs across devices, and where it lives in the code — for looking features up, writing test plans and planning work.
> How to use each feature is in [PRODUCT.md](PRODUCT.md); how it works is in [ARCHITECTURE.md](ARCHITECTURE.md).

## 0. How to read this

**Feature IDs**: category letter + number, e.g. `B3`. New features take the next number in their category; existing IDs never change.

**Platforms**

| Mark | Meaning |
| --- | --- |
| Desk | Desktop app (macOS / Windows, Wails) |
| CLI | Command-line version (terminal + local browser at `localhost:8686`) |
| Web | Remote browser (phone, tablet, or another computer that scanned the QR code) |
| All | All three |

**Sync scope**

| Mark | Meaning |
| --- | --- |
| Synced | Changes reach every device in the space |
| Local | Only affects this computer |
| — | No data involved (pure UI or local action) |

## 1. Overview

```
Lanvia
├── A Connection & pairing   pairing code · scan · discovery · manual add · online devices
├── B Clipboard sync         auto copy / auto paste · images · sensitive filter · blocklist · pause · manual send
├── C Sending & receiving    text · files · folders · code / link detection · delivery receipts
├── D Organizing             Omni-Bar · filters · device beam · click to copy · tags · favorites · checklists
├── E Viewing & editing      preview · highlighting · editing · versions · comments · reading pane tabs
├── F Retention & storage    retention · item limit · delete / bulk delete / clear · file backup · quota
├── G Desktop app            quick panel · tray menu · preview window · main window · notifications · auto-update
├── H Interface              language · theme · text size · panes · home screen
├── I Security & privacy     signatures · anti-replay · encryption · encrypted-only · local protection · tokens
├── J Automation             token API · iOS Shortcuts · Android · curl
└── K Deployment & ops       CLI flags · terminal QR · data directory · storage migration · compatibility
```

## 2. Features by category

### A. Connection & pairing

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| A1 | Pairing code | 10 characters (~50 bits), generated on first launch, shown as `XXXXX-XXXXX`; never sent over the network | Desk CLI | Local | `core/auth.go` |
| A2 | Scan to join | The code sits after `#` in the QR URL; the page removes it from the address bar right after reading it | Web | — | `core/web/main.js`, `settings.js` |
| A3 | Join another computer | Enter its pairing code (or `--join`) to switch to its space | Desk CLI | Local | `server.go` `handleJoin` |
| A4 | Regenerate pairing code | New code; all paired devices are disconnected | Desk CLI | Local | `handleJoin` |
| A5 | Automatic discovery | UDP broadcast every 3 s (port 8687); computers in the same space connect; VPN / virtual adapters excluded | Desk CLI | — | `core/peers.go`, `netutil.go` |
| A6 | Nearby devices | Computers seen in broadcasts with a different code; join in one click | Desk CLI | — | `peers.go` `Nearby` |
| A7 | Add device manually | Enter an IP when broadcast is blocked or across subnets; saved and retried; adding on one side is enough, the other connects back | Desk CLI | Local | `peers.go` `AddManualPeer`, `noticeCaller` |
| A8 | Online devices | Online computers and browsers, system type, whether the connection is encrypted; updates live | All | — | `app.go` `devices` |
| A9 | Offline detection | 3 failed checks in a row (~12 s) mark a computer offline | Desk CLI | — | `peers.go` `refresh` |

### B. Clipboard sync

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| B1 | Auto copy | On by default: copying on the computer sends it | Desk CLI | Local setting | `core/clipboard.go` |
| B2 | Auto paste | On by default: received text goes into the clipboard | Desk CLI | Local setting | `app.go` `ingest` |
| B3 | Clipboard images | Copied / captured images are sent as image files; received images (≤20 MB) go into the clipboard. Not on Linux | Desk CLI (Mac / Win) | Local setting | `clipimage.go`, `clipboard_*.go` |
| B4 | Sensitive content filter | Content marked sensitive by password managers is not sent | Desk CLI | — | `clipboard_*.go` `skipClipboardChange` |
| B5 | Copied files not sent | Files copied in Finder / Explorer are not sent (drag them into the window) | Desk CLI | — | same |
| B6 | App blocklist | Nothing copied in listed apps is sent; pick from "recent copy sources" | Desk CLI | Local | `clipboard.go` `noteApp` / `blocked` |
| B7 | Pause syncing | 15 min / 1 h / until resumed; resumes automatically; what was copied meanwhile is not sent | Desk CLI | Local | `clipboard.go` `Pause` |
| B8 | Send clipboard manually | With auto copy off, send the current clipboard from the tray or the page; the page previews it and says whether it can be sent | Desk CLI | — | `SendClipboard`, `ClipboardState` |
| B9 | Echo suppression | Content written by a remote device is remembered and not sent back out as a new copy | Desk CLI | — | `ClipWatcher.Apply` |
| B10 | One-click paste | The local page (secure context) can read and paste the clipboard; phones paste manually | Desk CLI | — | `core/web/main.js` |

### C. Sending & receiving

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| C1 | Send text | Up to 2 MB; Enter sends, Shift+Enter adds a line (touch devices use the button) | All | Synced | `handleClip`, `list.js` |
| C2 | Send files | Pick, drag or paste screenshots, with progress; up to 4 GB each | All | Synced | `handleUpload`, `list.js` |
| C3 | Send folders | Packed into a zip (no compression) in the browser | All | Synced | `core/web/zip.js` |
| C4 | Verification code detection | Short text mentioning a code / OTP gets a "Copy code" button | All | — | `list.js` |
| C5 | Link detection | Text that is a link gets "Open link" | All | — | `list.js` |
| C6 | Delivery receipts | "Waiting for confirmation · 1/2", "Received · 2 devices", …; only means the item was received | All | Local (not synced) | `core/delivery.go`, `web/delivery.js` |
| C7 | Retry sending | One click retries the devices that haven't confirmed | All | — | `handleDeliveryRetry` |
| C8 | Live push | Browsers receive updates over SSE, reconnect when back in the foreground, keep-alive every 20 s | Web | — | `server.go` `handleEvents` |

### D. Organizing

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| D1 | Type navigation | All / Text & links / Files / Checklists / Favorites, with counts | All | — | `index.html`, `list.js` |
| D2 | Omni-Bar search | One field searches and sends: typing filters the list, Enter sends; space-separated keywords must all match; ⌘K / Ctrl+K focuses it (the tray panel keeps a separate search box) | All | — | `list.js`, `inbox.js` `bindOmni` |
| D3 | Fuzzy matching | Falls back to fuzzy matching when nothing matches exactly: the letters only need to appear in order (`lnva` → "lanvia") | All | — | `list.js` `fuzzyScore` |
| D4 | Full-text search of long text | The list only carries the first 16 KB; the rest is searched on the computer | All | — | `core/search.go` |
| D5 | Filter by device / time | All devices or one; today / last 7 days / last 30 days | All | — | `list.js` |
| D6 | Device beam | The top bar shows online devices joined by a flowing line (more than 4 fold into `+N`); click one to filter by it, click again to clear; the line stops while paused or offline | All (hidden below 650 px) | — | `web/inbox.js` `renderBeam` |
| D7 | Tags | Tag items; filter from the tag bar, or `#tag` in the search box | All | Synced | `core/tags.go` |
| D8 | Favorite snippets | Favorite and name (e.g. "Home Wi-Fi"), names are searchable; never expire, exempt from the item limit | All | Synced | `app.go` `SetFavorite` |
| D9 | Shared checklists | Create lists; tick, add, edit, delete items, clear done ones; concurrent edits merge per item | All | Synced | `core/todo.go` |
| D10 | Lines to checklist | Multi-line text whose lines start with `-` / `*` / `1.` becomes a checklist when sent; the checklist button does it for any multi-line text | All | Synced | `inbox.js` `looksLikeList`, `list.js` `newTodo` |
| D11 | Click to copy | A single click on a text or checklist card copies it (with a pulse on the card); selecting text or clicking a control inside the card doesn't; file cards open the drawer | All | — | `inbox.js` `cardMainAction` |

### E. Viewing & editing

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| E1 | Preview | Read long content in full; Enter copies everything, selection copies just that; images and videos preview directly | All | — | `core/web/preview.js` |
| E2 | Code highlighting | Detects JSON, JS, Go, Python, Shell, SQL, HTML, CSS, YAML, with line numbers | All | — | `web/highlight.js` |
| E3 | JSON formatting | Format JSON in the preview with one click | All | — | `preview.js` |
| E4 | Editing | ⌘E edits, ⌘S saves a new version | All | Synced | `core/versions.go` |
| E5 | Version history | Who changed what and when, line-by-line diff, copy or restore any version; up to 50 per item | All | Synced | `versions.go`, `preview.js` |
| E6 | Comments | Add / delete comments, live in an open preview; up to 200 per item | All | Synced | `core/comments.go` |
| E7 | Drawer | “Details” on a card (or double-click) slides in a drawer from the right with Content / History / Comments tabs and a bottom bar (more actions, favorite, edit, copy); Esc or a click outside closes it; full screen on phones | All | — | `web/inbox.js`, `preview.js` |

### F. Retention & storage

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| F1 | History retention | 1 / 7 / 30 (default) days / forever; expired items won't sync back | Desk CLI | Local | `store.go` `Prune` |
| F2 | Per-item retention | Delete after 1 h / 1 day / 7 days / 30 days, or keep forever | All | Synced | `app.go` `SetItemExpiry` |
| F3 | Item limit | At most 300 items, oldest first; favorites, keep-forever and checklists exempt | Desk CLI | Local | `store.go` `Add` |
| F4 | Delete | Delete one item on every device | All | Synced | `app.go` `remove` |
| F5 | Bulk delete | "Select" mode: tick items or "Select all", delete up to 5,000 at once; the page refreshes once, peers receive the deletions in order | All (wide layout) | Synced | `app.go` `removeMany`, `peers.go` `relaySeq`, `web/inbox.js` |
| F6 | Clear history | Keeps favorites and checklists by default (the rest are bulk-deleted); untick to clear everything up to now | All | Synced | `app.go` `clear`, `web/inbox.js` `askClearAll` |
| F7 | Fetch files across devices | If this computer lacks a file it asks the sender, then the other computers; Range supported (video scrubbing) | All | — | `core/replicate.go` |
| F8 | Background file backup | Downloads files from other devices (last 3 days) up to 0 / 10 MB / 100 MB (default) / 1 GB / all | Desk CLI | Local | `replicate.go` `fetcher` |
| F9 | Storage limit | 500 MB – 10 GB / unlimited (default 2 GB); only copies are removed, never originals | Desk CLI | Local | `core/storage.go` |
| F10 | Clear copies | Remove all copies; they aren't re-downloaded during this run | Desk CLI | Local | `ClearReplicas` |
| F11 | Thumbnails | The list only loads 480 px thumbnails, rotated by EXIF orientation | All | — | `core/thumb.go` |
| F12 | Save to Downloads | Desktop saves files to `Downloads/Lanvia/` and can open them or show them in the folder | Desk CLI | — | `core/files.go` |

### G. Desktop app

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| G1 | Quick panel | Click the tray icon: type to search, ↑↓, Enter copies and closes, Esc closes; hides when it loses focus | Desk | — | `cmd/lanvia-desktop/main.go`, `web/main.js` |
| G2 | Tray menu | 6 latest items, Favorites submenu, auto copy / paste, send clipboard, pause, launch at login, pairing code, check for updates, quit | Desk | — | `main.go` `fillMenu` |
| G3 | Preview window | Space or ⌘Y opens a floating preview; movable, resizable, pin / unpin on top | Desk | — | `main.go` `openPreviewMode` |
| G4 | Main window | Single centered stream: device beam, sync capsule (sync switches, pause, appearance, text size, tags, settings), Omni-Bar, filter pills, cards, drawer; pairing QR code; settings grouped by category | Desk (browsers too) | — | `web/inbox.js`, `inbox.css` |
| G5 | Notifications | "Copy" for text, "Show in folder" for files, click opens the preview; bursts merged; none while a Lanvia window is in front | Desk | Local setting | `notify.go` |
| G6 | Launch at login | Toggle in the tray menu | Desk | Local | `main.go` |
| G7 | Single instance | Launching again just opens the running instance's main window | Desk | — | `main.go` |
| G8 | Close hides | Closing doesn't quit, sync continues; the main window opens on first use | Desk | — | `main.go` `hideOnClose` |
| G9 | Menu bar feedback | After a manual send / copy, a short message next to the menu bar icon (tooltip on Windows) | Desk | — | `main.go` `flash` |
| G10 | Windows title bar | Integrated title bar on Windows, keeping native snap | Desk (Windows) | — | `web/window-chrome.js` |
| G11 | Update checks | 20 s after launch and every 6 h; manual check in settings or the tray | Desk | — | `updater.go`, `internal/update` |
| G12 | Automatic updates | Background download with SHA-256 check, installs and relaunches when idle, then notifies; a failed version isn't retried automatically | Desk | Local setting | same |
| G13 | Port-in-use message | Explains the problem (e.g. the CLI is running) instead of exiting silently | Desk | — | `main.go` `fatalWindow` |

### H. Interface

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| H1 | Interface language | Chinese / English / follow system; shared by local windows, tray menu and logs | All | Local | `web/i18n.js`, `desktop/i18n.go` |
| H2 | Theme | Follow system / light / dark, chosen in the sync capsule menu | All | Per device | `web/inbox.js`, `base.js` |
| H3 | Text size | 80%–160% slider in the sync capsule menu; ⌘/Ctrl + − 0 on desktop | All | Per device | `web/inbox.js`, `features.js` |
| H5 | Add to home screen | Web App Manifest, opens like an app on phones | Web | — | `web/manifest.webmanifest` |
| H6 | Device name | Changed in settings, shown on other devices | Desk CLI | Local | `handleSettings` |

### I. Security & privacy

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| I1 | Request signatures | Every request is HMAC-signed with a key derived from the pairing code, valid for 5 minutes | All | — | `core/auth.go` |
| I2 | Anti-replay | A write request's nonce can be used only once | All | — | `auth.go` `nonceCache` |
| I3 | Content encryption | Framed AES-256-GCM between computers and between browsers and computers, including SSE and files | All | — | `core/crypto.go`, `web/crypto.js` |
| I4 | Encrypted connections only | Rejects old versions without encryption and prevents stripping it | Desk CLI | Local | `server.go` `authed` |
| I5 | Local management protection | Management endpoints accept only localhost and check Host and Origin against DNS rebinding / CSRF | Desk CLI | — | `auth.go` `isLocalAdmin` |
| I6 | Upload sandbox | Uploads are served with `CSP: sandbox`; SVG / HTML only as attachments | All | — | `server.go` `blobHeaders` |
| I7 | Token management | Disable token access, regenerate the token (the old one stops working immediately) | Desk CLI | Local | `server.go` |

### J. Automation

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| J1 | Token HTTP API | `Authorization: Bearer <token>` for `/api/clip` (send), `/api/latest` (latest text), `/api/upload` (files); plain text | Any HTTP client | Synced | `server.go` |
| J2 | iOS Shortcuts | Send the clipboard, fetch the latest; bind to Back Tap or the share sheet | iPhone / iPad | Synced | same |
| J3 | Android automation | HTTP Shortcuts / Tasker with the same endpoints | Android | Synced | same |

### K. Deployment & ops

| ID | Feature | Description | Platforms | Sync | Code |
| --- | --- | --- | --- | --- | --- |
| K1 | CLI flags | `--join` `--peer` `--no-discovery` `--name` `--port` `--no-watch` `--lang` `--data` | CLI | Local | `cmd/lanvia/main.go` |
| K2 | Terminal banner | Prints the pairing code, local and LAN addresses, and a QR code that scans on dark terminals too | CLI | — | `core/banner.go` |
| K3 | Log language | Terminal output and logs follow `--lang` or the system language | CLI Desk | — | `core/lang.go` |
| K4 | Data directory | `Lanvia/` under the system config directory by default, or `--data` | CLI | — | `core/config.go` |
| K5 | Storage migration | Migrates old history formats to per-file storage on startup and removes leftovers | Desk CLI | — | `store.go` `load` |
| K6 | Backward compatibility | Interoperates with v0.7 and earlier (v1 signatures, plaintext fallback) | Desk CLI | — | `auth.go`, `peers.go` |
| K7 | Forward compatibility | Unknown fields and sync operations are kept and forwarded, so future features survive passing through this version | Desk CLI | Synced | `core/compat.go` |

## 3. Platform comparison

| Capability | Desktop app | Command line | Phone / tablet browser |
| --- | :---: | :---: | :---: |
| Watch and send the clipboard automatically | ✓ | ✓ | ✗ (browser restriction; paste manually) |
| Write received content to the clipboard | ✓ | ✓ | ✗ (tap "Copy") |
| Clipboard images | ✓ (Mac / Win) | ✓ (Mac / Win) | ✗ |
| Send / receive text, files, folders | ✓ | ✓ | ✓ |
| Search, tags, favorites, checklists, editing, history, comments | ✓ | ✓ (local browser) | ✓ |
| Bulk delete, drawer, sync capsule | ✓ | ✓ (local browser) | ✓ (no sync switches) |
| Local settings, pairing code, manual devices | ✓ | ✓ (local browser) | ✗ |
| Tray, quick panel, preview window, notifications | ✓ | ✗ | ✗ |
| Automatic updates | ✓ | ✗ | Not needed (the page comes from the computer) |
| Background file backup, storage limit | ✓ | ✓ | ✗ |
| Stream video while downloading | ✓ | ✓ | ✗ (downloaded and decrypted first) |

## 4. Synced vs. local

| Synced to every device | This computer only |
| --- | --- |
| New items (text, file entries, checklists) | Auto copy, auto paste, pause |
| Deleting one or many items, clearing history | History retention, item limit |
| Edits and version history | Auto-download limit, storage limit, clear copies |
| Per-item retention | App blocklist, notifications, automatic updates |
| Favorites and snippet names | Encrypted connections only, token on/off and reset |
| Tags | Device name, language, manually added devices |
| Checklist items (ticks, add / edit / delete, title) | Pairing code (changing it changes the space) |
| Comments | Theme, text size (saved per browser) |
| | Delivery receipts (only in the sender's memory) |

> File contents are not pushed to every device: the file *entry* syncs, and the content is fetched on demand or backed up in the background within the auto-download limit.

## 5. Keyboard shortcuts

| Where | Shortcut | Action |
| --- | --- | --- |
| Input box | Enter / ⌘Enter | Send (Shift+Enter for a new line) |
| Main window | ⌘K / Ctrl+K | Focus the Omni-Bar |
| | Esc (in the Omni-Bar) | Clear the text and the filter |
| | ↑ ↓ (on a focused card) | Move between cards |
| | Enter / Space (on a focused card) | Copy / open the drawer |
| Quick panel | ⌘K / Ctrl+K | Focus search |
| | ↑ ↓ | Select |
| | Enter | Copy the selected item (files: save and show in folder) |
| | Space (empty search box) / ⌘Y | Preview |
| | ⌘E | Edit |
| | Esc | Close the quick panel |
| Preview | Enter | Copy everything |
| | ⌘C | Copy the selection, or everything when nothing is selected |
| | ⌘E | Edit |
| | Space / Esc | Close the preview |
| Editing | ⌘S / ⌘Enter | Save a new version |
| | Esc | Cancel |
| Version history | ↑ ↓ | Switch versions |
| | ⌘C | Copy the selected version |
| | Esc | Back to the preview |
| Anywhere (desktop) | ⌘ / Ctrl + `+` `−` `0` | Larger, smaller, reset text size |

> On Windows / Linux, ⌘ means Ctrl.

## 6. Known limitations and plans

| Category | Today | Plan |
| --- | --- | --- |
| A Connection | No connection with router AP isolation | — |
| B Clipboard | No images on Linux; phone browsers can't read the clipboard in the background | Linux images; native app keyboard / share extensions |
| C Sending | Always sent to the whole space | Send to a specific device |
| F Bulk delete | If a peer fails one delete in the sequence, the rest of that batch isn't sent to it until it reconnects | Retry the remaining deletions |
| I Security | The page is served over http; token API is plain text | Optional HTTPS (self-signed, also enabling PWA offline mode and the share target) |
| G Desktop | Not notarized, no Windows code signing | Notarization and signing |
| — Mobile | Web only | Native iOS / Android apps |
