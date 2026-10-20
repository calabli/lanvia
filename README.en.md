# Lanvia

[简体中文](README.md) | **English**

Instantly share clipboard text and files between devices on the same local network — Mac, Windows, Linux, iPhone, iPad and Android.

- **Computers**: run a single-file app. It watches the system clipboard and writes whatever other devices send straight into it, so you just press ⌘V / Ctrl+V.
- **Phones / tablets**: nothing to install. Scan a QR code with the camera and it opens in the browser (you can "Add to Home Screen").
- **Several computers**: give them the same pairing code and they find each other via UDP broadcast and stay in sync. No server, no internet required.

> The interface is available in English and Chinese and follows your system language by default (change it under Settings → Language). Some names below also give the Chinese label in brackets.

## Download and run

**Desktop app (recommended)**: on a Mac, unzip `Lanvia-macos.zip` and drag `Lanvia.app` into Applications; on Windows, run `Lanvia-windows-amd64.exe`. It lives in the menu bar / system tray — no browser needed:

| Action | What it does |
| --- | --- |
| Click the tray icon | Opens the quick panel: type to search, ↑↓ to choose, Enter copies to the clipboard and closes, Esc closes |
| Right-click the tray icon | Copy any of the 6 latest items (files are saved and shown in Finder / Explorer), favorites, active / passive mode, send current clipboard, pause syncing, launch at login, check for updates, quit |
| **Space** or **⌘Y** in the panel | Floating preview window: read long content in full, Enter copies everything, select part and ⌘C copies just that |
| **⌘E** in the panel, or Edit (编辑) in the preview | Edit the content; ⌘S saves it as a new version and syncs it to every device |
| Clock button in the preview | Version history: who changed what and when, line-by-line diff, copy or restore any version |
| Timer button on an item | Retention for this item: delete after 1 hour / 1 day / 7 days / 30 days, or keep forever — synced to every device |
| Star on an item | Save it as a favorite snippet, optionally named (e.g. "Home Wi-Fi"); it never expires, and the tray's Favorites menu copies it in one click |
| "New list" next to the input box | Shared checklists (shopping, things to take): check, add and edit items right on the card; several lines in the input box become a list in one click |
| Comment button on an item | Add comments (e.g. "Moved to 4 pm"); they sync to every device and appear live in an open preview |
| Search box | Space-separated keywords must all match; with no exact match it falls back to fuzzy matching (`wfmm` finds "WiFi 密码"); filter by type, device and time |
| Drop a folder / Add folder | Packs it into a zip on this device and sends it |
| Tray menu → Open main window (打开主窗口) | Pairing QR code, settings (language, send mode, history retention, auto-download files, storage limit, encryption, add device manually) and the full history |

**Can't see the other computer?** If both use the same pairing code but never show up (the router blocks broadcast, they are on different subnets, etc.), enter the other computer's IP under **Settings → Add device manually (手动添加设备)**. Adding it on one computer is enough. If the router has AP / client isolation enabled, devices cannot reach each other at all and this won't help.

The preview window detects and highlights JSON, JavaScript, Go, Python, Shell, SQL, HTML, CSS and YAML, and formats JSON in one click.

**Send mode**: in active mode, anything you copy is sent automatically; in passive mode nothing is sent until you choose **Send current clipboard (发送当前剪贴板)** in the tray menu or on the page. Files copied in Finder and passwords copied from a password manager are never sent automatically — drag files into the window instead.

On first launch the main window opens so you can pair your phone. Closing a window doesn't quit; Lanvia keeps syncing in the background.

- **Notifications**: when another device sends something you get a system notification — Copy right from it for text, or click it to open the preview. Not shown while a Lanvia window is in front.
- **Pause syncing**: pause for 15 minutes / 1 hour / until you resume from the tray. While paused, copies aren't sent and incoming items aren't written to the clipboard.
- **App blocklist**: add apps in settings (or pick them from "Recently copied from"); content copied in those apps is never sent.
- **Auto-update**: checks GitHub for a new version at startup and every 6 hours, and updates from the tray in one click (verified with SHA256, then replaced and relaunched). The repository must be public for this to work.

**Command-line version**: for servers, Linux, or when you don't want a UI.

```bash
go run ./cmd/lanvia
```

The terminal prints the pairing code, LAN address and a QR code; `http://localhost:8686` in a browser shows the same management page. Don't run the command-line and desktop versions at the same time — they share configuration and port.

Common flags:

| Flag | Description |
| --- | --- |
| `--join XXXXX-XXXXX` | Use another computer's pairing code and join its space (also available on the management page) |
| `--peer 192.168.1.20:8686` | Connect to a peer manually (this run only), for when broadcast is blocked or you're on different subnets |
| `--no-discovery` | Don't use LAN broadcast; only connect to manually added devices |
| `--name "Study PC"` | Device name |
| `--port 8686` | HTTP port (discovery always uses UDP 8687) |
| `--no-watch` | Don't watch this computer's clipboard |
| `--lang en` | Language of terminal output and logs (`zh` / `en`), defaults to the system language |
| `--data DIR` | Data directory, defaults to `Lanvia/` under the system config directory |

## Building

Run on a Mac; output goes to `dist/`:

```bash
scripts/build.sh
```

- `Lanvia-macos.zip`: `Lanvia.app` as a universal binary (Apple silicon + Intel), macOS 13+, ad-hoc signed
- `Lanvia-windows-{amd64,arm64}.exe`: with icon, high-DPI manifest and no console window (Windows 10/11 ship WebView2)
- `lanvia-cli-*`: the command-line version for six platforms
- `SHA256SUMS.txt`

`scripts/build.sh desktop` or `scripts/build.sh cli` builds only one part. Icons are generated with `go run ./tools/genicons`.

## Layout

```
core/                 The node: clipboard, discovery and sync, encryption, HTTP API
core/web/             Built-in web UI, split by feature into base / list / preview / features / settings / main scripts
cmd/lanvia/           Command-line version
cmd/lanvia-desktop/   Desktop app (Wails v3: tray, quick panel, preview window, main window, notifications, auto-update)
internal/update/      Auto-update: checking, downloading with verification, replacing the app
tools/genicons/       Generates the app and tray icons
scripts/build.sh      Build script
```

## How it works

```
 iPhone ─┐  SSE + HTTP                signed HTTP relay
 iPad  ──┼──────────▶  Mac node  ◀──────────────────▶  Windows node ◀── Android
         │                ▲                                   ▲
         └ QR pairing     └───── UDP broadcast discovery (8687) ┘
```

- **Nodes**: every computer is a peer node (Go, with the web UI embedded in the binary). The desktop app runs the node in the same process and its windows load the page the node serves on localhost, so it's the same UI you see in a browser.
- **Discovery**: every 3 seconds each network interface broadcasts a beacon carrying a space ID derived from the pairing code. When a device in the same space shows up, a signed request to its `/api/info` confirms both sides share the pairing code.
- **Sync**: each item has a random ID. A node only relays changes it sees for the first time, so the network doesn't need to be fully connected and loops can't form. A node coming online pulls the other side's history; deletes and clears spread as tombstones.
- **Push**: browsers receive updates over Server-Sent Events (reliable on iOS Safari, with a keep-alive every 20 seconds) and reconnect when brought back to the foreground.
- **Files**: stored on the node they were uploaded to; other computers each keep a backup copy in the background (100 MB and under by default, adjustable in settings; each computer's file storage is capped at 2 GB by default and only copies are evicted, never originals). Downloads go to the sender first and fall back to any computer that has a copy, so the sender being off doesn't matter. Range requests are supported (videos can seek).
- **Retention**: the global retention period applies to this computer only, and expired items won't sync back from other devices; per-item retention syncs to every device — concurrent changes are resolved by time, with a fixed tie-break when times are equal.
- **Editing and versions**: every edit creates a version with its author and time, and the original is kept as the first version. Nodes merge version sets by ID and show the newest version, so concurrent edits on several devices converge to the same result without losing any version.
- **Lists**: merged item by item — each item's text and checked state keep the latest change independently, and deletions are tombstones — so several devices checking, adding or editing different items at once never overwrite each other. Lists aren't affected by retention or the item limit; they're only removed by hand. Older devices don't know about lists and ignore them.
- **Comments**: each comment has a random ID and nodes merge comment sets by ID; deletions are kept as tombstones (the earliest deletion time wins), so deletions propagate and never sync back. Up to 200 comments per item. Older devices don't know about comments, so comments relayed only through them are lost, but new devices connected directly sync normally.
- **Encryption**: requests, push events, relayed content and files are encrypted with AES-256-GCM (key derived from the pairing code), both between computers and between phone browsers and computers, in 64 KB frames so tampering, truncation and reordering are all detected. The web client ships its own AES-GCM (http pages on a LAN can't use the browser's crypto APIs), using the same frame format as the Go side. On phones, images and videos are downloaded and decrypted before they're shown, so videos can't stream while downloading; files over 256 MB are sent unencrypted on phones to avoid using as much memory. Older nodes without encryption fall back to plaintext for compatibility; turn on "Encrypted connections only" in settings to refuse them.
- **Clipboard**: macOS calls NSPasteboard directly (polling only reads the change count, so it uses almost no power; without cgo it falls back to `pbpaste`/`pbcopy`), Windows calls the Win32 API directly, Linux uses `wl-clipboard`/`xclip`/`xsel`. Copied files and content marked sensitive by password managers are not sent. Content written by a remote device is remembered so it isn't broadcast again as a new copy.

## Security

- The pairing code (10 characters, about 50 bits) **never travels over the network**. Both sides derive an HMAC key from it, and every request is signed with timestamp + random nonce + method + path + query, valid for 5 minutes (clients first calibrate their clock with `/api/time`).
- **Replay protection**: a write request's nonce is accepted only once, so a captured request can't be sent again, and the query is covered by the signature so it can't be altered. Devices on v0.7 and earlier sign only timestamp, method and path; they're still accepted for compatibility, but their requests aren't protected against replay.
- In the QR code the pairing code sits after `#`, so it's never sent to the server, and it's removed from the address bar as soon as it's read.
- The LAN uses plain http, where browsers don't provide `crypto.subtle`, so the web client ships its own SHA-256/HMAC and AES-256-GCM (tests use node to encrypt and decrypt against Go in both directions).
- The local dashboard doesn't need signatures, but requests must come from the loopback address with `Host` set to localhost, and writes must have an `Origin` matching the host — protecting against DNS rebinding and cross-site requests.
- Uploaded files are served with `Content-Security-Policy: sandbox`, and SVG/HTML are always downloads, so they can't run scripts on this origin.
- Content, history and files between computers are encrypted with AES-256-GCM; with "Encrypted connections only" on, a man in the middle can't strip the encryption either.
- **Limitations**:
  - The page itself (HTML and scripts) is served over http. Someone who only sniffs the Wi-Fi can't read the content, but someone who can tamper with traffic (e.g. ARP spoofing) could change the scripts and obtain the pairing code.
  - The automation token and everything sent with it travel unencrypted. You can turn token access off in settings, or regenerate the token if it may have leaked.

## Phone automation (optional)

**Settings → Automation (自动化)** on the dashboard shows an API token (derived from the pairing code; the code can't be recovered from it; you can regenerate or turn it off). The token and everything sent with it travel unencrypted.

**iOS Shortcuts**: can be bound to Back Tap or the share sheet.

- Send clipboard: `Get Clipboard` → `Get Contents of URL` (POST `http://<computer-ip>:8686/api/clip`, header `Authorization: Bearer <token>`, request body "File" set to the clipboard)
- Fetch the latest item: `Get Contents of URL` (GET `/api/latest` with the same header) → `Copy to Clipboard`

**Android**: call the same endpoints with HTTP Shortcuts or Tasker.

## Limits of the web client

- An http page on the LAN can't read the clipboard in the background, so on phones you paste manually (long-press the input box) and tap Copy (复制) when something arrives. The dashboard on a computer (localhost) is a secure context and can paste with one click.
- Screenshots and files can be pasted, dragged in or picked, with a progress bar.
- Verification codes and links are detected, with "Copy code" and "Open link" buttons.

## Ideas for later

- System notifications
- Optional HTTPS (with a self-signed certificate installed, browsers can read the clipboard and use PWA offline mode and share targets)
- Writing images directly to the system clipboard (macOS NSPasteboard, Windows CF_DIB)
- Sending to a specific device
- Native iOS / Android apps: share extension, keyboard extension, running in the background
