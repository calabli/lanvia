# Lanvia

[简体中文](README.md) | **English**

Instantly share clipboard text and files between devices on the same local network — Mac, Windows, Linux, iPhone, iPad and Android. No server, no internet, no account.

- **Computers**: run a single-file app. It watches the system clipboard and writes whatever other devices send straight into it, so you just press ⌘V / Ctrl+V.
- **Phones / tablets**: nothing to install. Scan a QR code with the camera and it opens in the browser (you can "Add to Home Screen").
- **Several computers**: give them the same pairing code and they find each other via UDP broadcast and stay in sync. Content travels between devices encrypted with AES-256-GCM.

The interface is available in English and Chinese and follows your system language by default.

## Features

- Send text, images, files and folders instantly; files are backed up on every computer in the background, so they stay downloadable when the sender is off
- Independent auto copy / auto paste toggles, pause syncing, app blocklist; content marked sensitive by password managers is never sent
- Search (multiple keywords, fuzzy matching, tags, filters), highlighted code preview, editing with version history, favorite snippets, tags, comments, shared checklists
- Per-item retention, global history retention, bulk delete, storage quota for file copies
- Desktop app: tray quick panel, floating preview, system notifications, delivery receipts, automatic updates
- HTTP endpoints for iOS Shortcuts / Android automation

See the **[product guide](docs/PRODUCT.md)** for everything Lanvia can do.

## Download and run

**Desktop app (recommended)**, from Releases:

- Mac: download `Lanvia-macos-apple-silicon.dmg` or `Lanvia-macos-intel.dmg` for your chip and open "安装 Lanvia.pkg" inside. The app is not notarized yet, so the first time you need to allow it under System Settings → Privacy & Security (see the install guide in the DMG).
- Windows: run `Lanvia-windows-amd64.exe` (`arm64` on ARM devices).

The desktop app lives in the menu bar / system tray: click the icon for the quick panel (type to search, ↑↓ to choose, Enter copies), right-click for the menu. On first launch the main window opens so you can scan the pairing QR code with your phone.

**Command-line version**, for servers, Linux, or when you don't want a UI:

```bash
go run ./cmd/lanvia
```

The terminal prints the pairing code, LAN addresses and a QR code; `http://localhost:8686` in a browser is the same management page. Common flags:

| Flag | Description |
| --- | --- |
| `--join XXXXX-XXXXX` | Join another computer's space with its pairing code |
| `--peer 192.168.1.20:8686` | Add a peer manually (this run only), when the router blocks broadcast or devices are on different subnets; repeatable |
| `--no-discovery` | Don't use LAN broadcast; only connect to manually added devices |
| `--name Study-PC` | Device name |
| `--port 8686` | HTTP port (broadcast always uses UDP 8687) |
| `--no-watch` | Don't watch this computer's clipboard |
| `--lang en` | Language for terminal output and logs (`zh` / `en`), defaults to the system language |
| `--data DIR` | Data directory, defaults to `Lanvia/` under the system config directory |

> Don't run the command-line version and the desktop app at the same time — they share the configuration and ports.

**Can't see the other device?** If the pairing codes match but the other device never shows up (the router blocks broadcast, different subnets, …), add its IP under Settings → Add device manually. Adding it on one computer is enough. Routers with AP/client isolation block all traffic between devices, which this can't fix.

## Documentation

| Document | Contents |
| --- | --- |
| [docs/PRODUCT.md](docs/PRODUCT.md) | Positioning, features, settings, security notes, FAQ, roadmap |
| [docs/FEATURES.md](docs/FEATURES.md) | Categorized feature table: IDs, platforms, sync scope, code locations; platform comparison; shortcuts |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Architecture, data model and storage, discovery and sync protocol, merge rules, HTTP API, security design, front end and desktop app |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | Environment, running locally, tests, conventions, packaging and releasing |

## Layout

```
core/                 Node: clipboard, discovery and sync, encryption, storage, HTTP API
core/web/             Built-in web UI (plain JS, no build step)
cmd/lanvia/           Command-line version
cmd/lanvia-desktop/   Desktop app (Wails v3)
internal/update/      Automatic updates
tools/genicons/       Icon generator
scripts/              Packaging (build.sh) and release (release.sh)
docs/                 Product and technical documentation
```
