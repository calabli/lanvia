# Lanvia Product Guide

> Applies to v0.14.x. The categorized feature table (IDs, platforms, sync scope, code locations) is in [FEATURES.md](FEATURES.md); the implementation is described in [ARCHITECTURE.md](ARCHITECTURE.md); building and releasing are covered in [DEVELOPMENT.md](DEVELOPMENT.md).

## 1. What Lanvia is

Lanvia **shares clipboard content and files instantly between devices on the same local network**.

- **No server, no internet, no account.** Devices on the same Wi-Fi form a "space" with a 10-character pairing code and sync directly with each other.
- **Computers** (macOS / Windows / Linux) run a single-file app that watches the system clipboard; whatever other devices send lands in the clipboard, ready for ⌘V / Ctrl+V.
- **Phones and tablets** (iPhone / iPad / Android) install nothing: scan a QR code with the camera and use it in the browser (it can be added to the home screen).

### Typical scenarios

| Scenario | How |
| --- | --- |
| Copy on a Mac, paste on Windows | Same pairing code on both, auto copy and auto paste on — it just works |
| Get a verification code or link from the phone onto the computer | Paste it in the phone's browser page; it lands in the computer's clipboard |
| Send a screenshot or file from the computer to the phone | Drag it into the window or copy the screenshot; download or preview it on the phone |
| A shared shopping list at home | Create a checklist; several devices tick and add items at the same time without overwriting each other |
| Frequently used snippets (Wi-Fi password, address) | Favorite and name them; copy with one click from the tray menu |

### Design principles

1. **LAN first, zero configuration.** Automatic discovery and scan-to-join; an IP only has to be entered when the router blocks broadcast.
2. **Data only flows between your own devices.** The pairing code never goes over the network, and content is encrypted between devices.
3. **Concurrent edits converge.** Edits, checklists, tags, comments and favorites all have deterministic merge rules, so nothing overwrites or loses anything else.
4. **Stay out of the way.** No global shortcuts; no notifications while a Lanvia window is in front; automatic updates install only when you are not using a Lanvia window.

## 2. Editions

| Edition | Description | Download |
| --- | --- | --- |
| Desktop app (recommended) | Lives in the menu bar / tray: quick panel, preview window, main window, notifications, automatic updates | Mac: `Lanvia-macos-apple-silicon.dmg` / `Lanvia-macos-intel.dmg` (with an installer); Windows: `Lanvia-windows-{amd64,arm64}.exe` |
| Command-line version | Runs a node in the terminal, prints the pairing code and a QR code; manage it at `http://localhost:8686` | `lanvia-cli-*`, six platforms |
| Web | Phones, tablets and other computers use it in the browser after scanning; the same UI as the desktop app | Nothing to install |

> Don't run the command-line version and the desktop app on the same computer at the same time: they share the data directory and ports (HTTP 8686, UDP 8687).

Requirements: macOS 13+; Windows 10/11 (WebView2 is built in); on Linux the clipboard needs one of `wl-clipboard`, `xclip` or `xsel`.

## 3. Features

### 3.1 Pairing and devices

- **Pairing code**: 10 characters (confusable characters removed, about 50 bits), shown as `XXXXX-XXXXX`, generated on first launch.
- **Scan to join**: the main window / terminal shows a QR code. The pairing code sits after the `#` in the URL, so it is never sent to the server, and the page removes it from the address bar right after reading it.
- **Join another computer**: enter its pairing code in Settings (or use `--join`) to switch to its space. "Regenerate pairing code" disconnects every paired device.
- **Automatic discovery**: a broadcast every 3 seconds; computers in the same space connect automatically. Computers nearby with a different pairing code are listed as "nearby devices" so you can join them in one click.
- **Add a device manually**: when the router blocks broadcast or the devices are on different subnets, enter the other computer's IP (adding it on one side is enough — the other connects back). Routers with AP/client isolation block all traffic between devices, which this can't fix.
- **Online devices**: every online computer and browser, its system type, and whether its connection to this computer is encrypted.

### 3.2 Clipboard sync

- **Auto copy** (on by default): copying on the computer sends it. When off, nothing is sent on copy; use "Send clipboard" in the tray menu or on the page.
- **Auto paste** (on by default): text from other devices is written to this computer's clipboard. When off, it only appears in the list.
- **Images**: images copied or captured on Mac / Windows (image only, no text) are sent as image files; with auto paste on, received images (up to 20 MB) go into the clipboard too. Not supported on Linux yet.
- **Never sent automatically**: files copied in Finder / Explorer, content marked sensitive by password managers, and anything copied in a blocked app. Drag files into the window to send them.
- **Pause syncing**: 15 minutes / 1 hour / until resumed. Nothing copied here is sent and nothing received is written to the clipboard; on resume, what was copied during the pause is not sent.
- **App blocklist**: add apps in Settings (or pick them from "recent copy sources"); content copied in those apps is not sent.

### 3.3 Content types

| Type | Description |
| --- | --- |
| Text | Up to 2 MB. Verification codes and links are recognized, with "Copy code" and "Open link" buttons |
| File | Drag, paste or pick, with a progress bar; up to 4 GB each. Images show thumbnails in the list; videos can be scrubbed |
| Folder | Packed into a zip (no compression) in the browser and sent |
| Checklist | Shared to-do / shopping lists: tick, add and edit items right on the card; multi-line text in the input box can be turned into a list in one click |

### 3.4 Managing content

| Feature | Description |
| --- | --- |
| Omni-Bar | One field for searching and sending: typing filters the list, Enter sends (several `-` / `*` lines become a checklist), ⌘V anywhere sends, files can be dropped anywhere. Space-separated keywords must all match; with no exact match it falls back to fuzzy matching (`lnva` finds "lanvia"); `#tag` filters by tag; long text is searched in full on the computer |
| Cards | A single click copies a text or checklist card; hovering shows only ★ and “Details”; checklist items can be ticked right on the card |
| Device beam | The top bar shows the online devices joined by a flowing line; click one to show only what it sent, click again to clear |
| Sync capsule | The `SYNC · E2E` pill opens one panel for auto copy / auto paste, pause, appearance, text size, tags and settings (phones only see appearance and their own settings) |
| Preview | Read long content in full; JSON, JavaScript, Go, Python, Shell, SQL, HTML, CSS and YAML are detected and highlighted, JSON can be formatted; select part to copy just that |
| Drawer | “Details” or a double-click slides in a drawer with Content / History / Comments tabs; edit, copy, tags, retention and delete live in its bottom bar |
| Editing and versions | Saving an edit creates a new version that syncs to every device; see who changed what and when, compare line by line, copy or restore any version (up to 50 versions per item) |
| Favorites | Save as a named snippet; never expires and doesn't count against the item limit; copy it from the tray's Favorites menu |
| Tags | Tag items (e.g. `#work`), synced to every device; the tag bar above the list filters by tag |
| Comments | Comment on items, synced to every device, appearing live in an open preview; up to 200 per item |
| Per-item retention | Delete after 1 hour / 1 day / 7 days / 30 days, or keep forever; synced to every device |
| Delivery receipts | After sending, shows states such as "Waiting for confirmation · 1/2" or "Received · 2 devices"; unconfirmed sends can be retried. A receipt only means the other side received the item — not that it went into their clipboard or that the file was downloaded |
| Bulk delete | "Select" enters multi-select mode; tick cards, then use the floating bar: select all (also loads the pages not yet shown), delete up to 5,000 items at once, or clear history — on every device |
| Delete / clear | Deleting syncs to every device. "Clear" keeps favorites and checklists by default (the rest are deleted item by item); untick the option to clear everything |

### 3.5 Storage and retention

- **History retention** (30 days by default, 0 = forever): only affects this computer. Expired items are deleted and won't sync back from other devices.
- **Item limit**: at most 300 items; the oldest go first. Favorites, keep-forever items and checklists are exempt.
- **Auto-download files** (below 100 MB by default): each computer backs up files from other devices (from the last 3 days) in the background, so they remain downloadable when the sender is off.
- **Storage limit** (2 GB by default): when exceeded, only copies are removed (oldest first, keep-forever last); originals sent from this computer are never removed automatically. Settings can clear all copies at once.

### 3.6 Desktop app

| Action | What it does |
| --- | --- |
| Click the tray icon | Opens the quick panel: type to search, ↑↓ to choose, Enter copies and closes, Esc closes |
| Right-click the tray icon | Copy any of the 6 latest items (files are saved and shown in Finder / Explorer), favorites, auto copy / auto paste, send clipboard, pause syncing, launch at login, check for updates, quit |
| **Space** or **⌘Y** in the panel | Floating preview window (pin / unpin on top) |
| **⌘E** in the panel | Edit; ⌘S saves a new version |
| Sync capsule / ⌘ + − 0 | Text size 80%–160%, saved per device |
| Tray menu → Open main window | Single centered stream with the device beam, sync capsule, Omni-Bar and cards; details in a drawer; pairing QR code (the + next to the beam), settings (grouped into General, Sync, Storage, Devices & pairing, Security, Automation, About & updates), full history |

- On first launch (no content yet) the main window opens so you can pair by scanning. Closing a window doesn't quit; syncing continues in the background.
- **Notifications**: shown when another device sends something; text has a "Copy" button, files have "Show in folder", clicking opens the preview. Several arriving together are merged; none are shown while a Lanvia window is in front.
- **Saving files**: files "downloaded" on the desktop go to `Downloads/Lanvia/`.

### 3.7 Update checks and automatic updates (desktop)

- Checks GitHub 20 seconds after launch and every 6 hours after that; can also be triggered under Settings → About & updates or from the tray menu.
- **Automatic updates on (default)**: the new version downloads in the background, is verified with SHA-256, and is installed and relaunched when you are not using a Lanvia window, followed by an "Updated to vX" notification. If an automatic install fails (for example the password dialog was cancelled), that version is not retried automatically; "Update now" still works.
- **Automatic updates off**: one notification, then "Update to vX" in the tray menu and settings until you choose to install.
- Apps installed with the Mac installer belong to the current user, so updates need no password; older installs owned by root ask for the system password once and are then handed back to the current user.
- No update is offered when the release has no package for this platform.

### 3.8 Automation (optional)

Settings → Automation shows an API token (derived from the pairing code and impossible to reverse; it can be regenerated or disabled). The token and everything sent with it travel **in plain text**.

- **iOS Shortcuts** (can be bound to Back Tap or the share sheet)
  - Send clipboard: `Get Clipboard` → `Get Contents of URL` (POST `http://<computer-ip>:8686/api/clip`, header `Authorization: Bearer <token>`, request body "File" set to the clipboard)
  - Fetch the latest: `Get Contents of URL` (GET `/api/latest`, same header) → `Copy to Clipboard`
- **Android**: call the same endpoints with HTTP Shortcuts or Tasker.
- **curl**: `curl -H "Authorization: Bearer <token>" --data-binary "hello" http://<computer-ip>:8686/api/clip`

### 3.9 Interface language

Chinese and English, following the system by default and switchable in Settings. The local web windows, tray menu and logs share this setting; the command-line version uses `--lang`.

## 4. Settings

| Setting | Default | Scope | Notes |
| --- | --- | --- | --- |
| Device name | System computer name | This computer | Shown on other devices |
| Auto copy | On | This computer | Send on copy |
| Auto paste | On | This computer | Write received text / images to the clipboard |
| History retention | 30 days | This computer | 0 = forever |
| Auto-download files | 100 MB | This computer | 0 = off, -1 = no limit |
| Storage limit | 2048 MB | This computer | 0 = no limit; only copies are removed |
| Encrypted connections only | Off | This computer | Rejects old versions without encryption and prevents stripping encryption |
| Notifications | On | This computer (desktop) | |
| Automatic updates | On | This computer (desktop) | |
| App blocklist | Empty | This computer | bundle id / process name / app name, case-insensitive |
| Disable token access | Off | This computer | Shortcuts / curl can't connect when disabled |
| Interface language | Follow system | This computer | `zh` / `en` |
| Manually added devices | Empty | This computer | host:port |

Per-item retention, favorites, tags, comments, edits, checklists, deletions and clears **sync to every device**; the settings above only affect this computer.

## 5. Security (for users)

- The pairing code never travels over the network; only devices that know it can join.
- Content and files between computers, and between phone browsers and computers, are encrypted with AES-256-GCM (key derived from the pairing code).
- Captured requests can't be replayed; the local management page is protected against DNS rebinding and cross-site requests.
- **Known limitations**
  - The web page itself (HTML and scripts) is served over http. Someone who can only sniff the same Wi-Fi sees nothing, but someone who can tamper with traffic (e.g. ARP spoofing) could change the page's scripts and obtain the pairing code.
  - On phones, files over 256 MB are transferred unencrypted (to avoid using the same amount of memory); not allowed with "Encrypted connections only".
  - The automation token and what is sent with it are plain text. Disable it when unused and regenerate it if you suspect a leak.

## 6. Web limitations

- An http page on the LAN can't read the clipboard in the background, so on phones you paste manually (long-press the input box) and tap "Copy" on received items. The local dashboard on the computer (localhost) is a secure context and can paste in one click.
- On phones, images and videos are downloaded and decrypted before display, so videos can't stream while downloading.

## 7. FAQ

| Problem | Fix |
| --- | --- |
| Same pairing code but the other device never shows up | The router blocks broadcast or the devices are on different subnets: add its IP under Settings → Add device manually |
| Manual add doesn't connect either | Make sure Lanvia is running there, the IP and port are right, and the firewall allows TCP 8686 and UDP 8687; AP isolation can't be worked around |
| Desktop app says "port already in use" | The command-line version is running; quit it first |
| macOS blocks the installer | Not notarized yet: System Settings → Privacy & Security → Open Anyway (see the install guide in the DMG) |
| Clipboard doesn't sync on Linux | Install `wl-clipboard` (Wayland) or `xclip` / `xsel` (X11) |
| A password copied from a password manager wasn't synced | By design: content marked sensitive is never sent |

## 8. Roadmap (not implemented yet)

- Optional HTTPS (self-signed certificate; once installed the browser can read the clipboard and use PWA offline mode and the share target)
- Send to a specific device
- Native iOS / Android apps: share extension, keyboard extension, running in the background
- Clipboard images on Linux
- Apple notarization / Windows code signing
