# Lanvia Development, Build and Release

> Product features are in [PRODUCT.md](PRODUCT.md), the categorized feature table in [FEATURES.md](FEATURES.md), and the architecture and protocols in [ARCHITECTURE.md](ARCHITECTURE.md).

## 1. Environment

| Tool | Used for |
| --- | --- |
| Go 1.26+ | building (`go.mod` requires 1.26) |
| Xcode command line tools (macOS) | the Mac desktop and Mac CLI builds need cgo (NSPasteboard, Wails) |
| node | `core/webcrypto_test.go` uses it to check that the web crypto interoperates with Go (skipped without node) |
| gh | `scripts/release.sh` publishes to GitHub |

Building every platform has to happen on macOS (Windows builds are pure Go cross-compiles with `CGO_ENABLED=0`).

## 2. Running locally

```bash
go run ./cmd/lanvia                                   # CLI, then open http://localhost:8686
go run ./cmd/lanvia --data /tmp/lanvia-b --port 8688  # a second node on the same computer, for debugging sync
go run ./cmd/lanvia-desktop                           # desktop app (needs cgo on macOS)
```

- Two nodes need the same pairing code to find each other: start the second with `--join <first node's code>`. Nodes on one computer share UDP 8687 (port reuse is enabled).
- The front end has no build step: edit `core/web/` and `go run` again (assets are embedded with `go:embed`).
- Use `--no-watch` while debugging sync to avoid reading your clipboard. Received content is still written to the clipboard unless you turn off auto paste in settings.

## 3. Testing

```bash
go vet ./...
go test ./...
go test -race ./core ./internal/...
GOOS=windows go build ./...          # Windows cross-compile check
GOOS=linux CGO_ENABLED=0 go vet ./core ./cmd/lanvia ./internal/...
for f in core/web/*.js; do node --check "$f"; done   # front-end syntax check
```

Coverage includes: signatures and anti-replay (`auth_test`), framed encryption (`crypto_test`, and `webcrypto_test` against the JS implementation), storage and migration (`store_test`), multi-node sync and merging (`sync_test`, `tags_test`, `todo_test`, `comments_test`), bulk delete (`batch_delete_test`), forward compatibility (`compat_test`), delivery receipts, quota, thumbnails, clipboard image conversion, and update lookup / download verification / replacement (`internal/update`).

> `go vet` for the Windows target reports `possible misuse of unsafe.Pointer` in `clipboard_windows.go`. That is the standard pattern for the address returned by Win32 `GlobalLock` and a known false positive.

## 4. Conventions

- Code comments, commit messages and documentation are in English.
- User-facing strings: Chinese is the source text. Terminal and log text uses `core.L(chinese, english)`; web text gets its English in the dictionary in `core/web/i18n.js`; tray menu text in `cmd/lanvia-desktop/i18n.go`.
- Every content change goes through `App`'s entry points (`ingest` / `remove` / `removeMany` / `afterXxx`), so storing, pushing to pages and relaying to nodes always happen together.
- New synced state goes into a new `Item` field or a new `Envelope.Op` carrying an `EID`, with a deterministic merge rule (ARCHITECTURE.md section 8), plus a multi-node test in the style of `sync_test`.
- Items in the Store are copy-on-write; never modify a pointer obtained from `List()` / `Get()` in place.
- Change the config with `App.updateCfg` and read it with `cfgSnapshot()`.

## 5. Packaging

```bash
scripts/build.sh            # everything, into dist/
scripts/build.sh cli        # CLI only
scripts/build.sh desktop    # Mac + Windows desktop
scripts/build.sh mac        # Mac desktop only
```

| Artifact | Description |
| --- | --- |
| `Lanvia-macos-apple-silicon.dmg` / `Lanvia-macos-intel.dmg` | for users: single-architecture app + macOS installer (installs into Applications and opens it) + install guide |
| `Lanvia-macos.zip` | universal `Lanvia.app` for automatic updates (older versions look for the Mac package under this name) |
| `Lanvia-windows-{amd64,arm64}.exe` | with icon, high-DPI manifest, no console window |
| `lanvia-cli-*` | CLI for six platforms (the Mac ones built with cgo) |
| `SHA256SUMS.txt` | checksums of every artifact, used by automatic updates |

- Minimum macOS 13 (matching Go 1.26), ad-hoc signed and strictly verified; no Apple notarization or Windows code signing yet.
- The version comes from `const Version` in `core/node.go`.
- Icons: `go run ./tools/genicons` generates `core/web/icon*.png`, `cmd/lanvia-desktop/build/appicon*.png`, `icon-small-32.png` and the tray icons from `tools/genicons/source.png`.
- Mac installer templates live in `cmd/lanvia-desktop/build/pkg/` (`distribution.xml`, welcome / conclusion pages, pre/post-install scripts).

## 6. Releasing

1. Bump `Version` in `core/node.go` and commit (e.g. "Version 0.14.3").
2. Run `scripts/build.sh`.
3. Write the release notes in `notes.md`.
4. Publish:

   ```bash
   scripts/release.sh v0.14.3 "Lanvia v0.14.3 · Title" notes.md
   ```

   The script checks that `dist/` matches `SHA256SUMS.txt`, creates a draft, uploads the checksum file first and then each package (retrying each up to 3 times), and only publishes once everything is uploaded — so automatic updates never see a release whose packages are still uploading.

> Automatic updates depend on the fixed names `Lanvia-macos.zip`, `Lanvia-windows-<arch>.exe` and `SHA256SUMS.txt`; renaming them breaks updates for older versions.

## 7. Debugging automatic updates

Set `LANVIA_UPDATE_URL` to an endpoint that returns GitHub Release JSON (e.g. a local fake server) and the desktop app uses it instead of GitHub. `internal/update/update_test.go` shows how to build such a fake server.
