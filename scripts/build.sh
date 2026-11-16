#!/usr/bin/env bash
# Build every release artifact into dist/. Must run on macOS (the Mac desktop app needs cgo and the Xcode toolchain).
#
#   scripts/build.sh            # everything
#   scripts/build.sh desktop    # desktop client only
#   scripts/build.sh cli        # command line version only
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION=$(sed -n 's/^const Version = "\(.*\)"/\1/p' core/node.go)
DIST=dist
WHAT=${1:-all}
LDFLAGS="-s -w"
mkdir -p "$DIST"

build_cli() {
  for t in windows/amd64 windows/arm64 darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
    os=${t%/*}; arch=${t#*/}; name=lanvia-cli-${os/darwin/macos}-$arch
    if [ "$os" = windows ]; then
      GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/$name.exe" ./cmd/lanvia
    elif [ "$os" = darwin ]; then
      # Build the Mac version with cgo so the clipboard uses the native NSPasteboard (no pbpaste process per poll)
      tmp=$(mktemp -d)
      clang_arch=$([ $arch = amd64 ] && echo x86_64 || echo arm64)
      MACOSX_DEPLOYMENT_TARGET=13.0 GOOS=$os GOARCH=$arch CGO_ENABLED=1 CC="clang -arch $clang_arch" \
        CGO_CFLAGS="-mmacosx-version-min=13.0" CGO_LDFLAGS="-mmacosx-version-min=13.0" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$tmp/lanvia" ./cmd/lanvia
      codesign --force -s - "$tmp/lanvia" >/dev/null
      tar -czf "$DIST/$name.tar.gz" -C "$tmp" lanvia && rm -rf "$tmp"
    else
      tmp=$(mktemp -d)
      GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$tmp/lanvia" ./cmd/lanvia
      tar -czf "$DIST/$name.tar.gz" -C "$tmp" lanvia && rm -rf "$tmp"
    fi
    echo "  ✓ $name"
  done
}

# Sign ad hoc and verify strictly. Strip the extended attributes first (Finder info, resource
# forks), otherwise signing fails or the result is judged invalid. Without a Developer ID
# certificate, Apple silicon still requires a complete signature.
sign_app() {
  xattr -cr "$1"
  codesign --force --deep -s - --identifier app.lanvia.desktop "$1" >/dev/null
  codesign --verify --deep --strict "$1" || { echo "签名校验失败：$1" >&2; exit 1; }
}

build_mac_app() {
  local tmp app
  tmp=$(mktemp -d)
  app="$tmp/Lanvia.app"
  # Matches the Go 1.26 minimum (macOS 13); without it the binary links against the newest system version of the local SDK
  export MACOSX_DEPLOYMENT_TARGET=13.0
  for arch in arm64 amd64; do
    clang_arch=$([ $arch = amd64 ] && echo x86_64 || echo arm64)
    GOOS=darwin GOARCH=$arch CGO_ENABLED=1 CC="clang -arch $clang_arch" \
      CGO_CFLAGS="-mmacosx-version-min=13.0" CGO_LDFLAGS="-mmacosx-version-min=13.0" \
      go build -trimpath -ldflags "$LDFLAGS" -o "$tmp/lanvia-$arch" ./cmd/lanvia-desktop
  done
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  lipo -create -output "$app/Contents/MacOS/Lanvia" "$tmp/lanvia-arm64" "$tmp/lanvia-amd64"

  # Icon: build the .icns from the 1024 px PNG
  local iconset="$tmp/AppIcon.iconset"
  mkdir -p "$iconset"
  for s in 16 32 128 256 512; do
    sips -z $s $s cmd/lanvia-desktop/build/appicon.png --out "$iconset/icon_${s}x${s}.png" >/dev/null
    sips -z $((s*2)) $((s*2)) cmd/lanvia-desktop/build/appicon.png --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
  done
  iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"
  sed "s/{{VERSION}}/$VERSION/g" cmd/lanvia-desktop/build/Info.plist > "$app/Contents/Info.plist"

  # Universal build (works on Apple silicon and Intel): zipped for the auto updater (older versions look for the Mac installer under this name)
  sign_app "$app"
  rm -f "$DIST/Lanvia-macos.zip" "$DIST/Lanvia-macos.dmg" "$DIST"/Lanvia-macos-*.dmg
  ditto -c -k --keepParent "$app" "$DIST/Lanvia-macos.zip"
  # Do not leave an unpacked Lanvia.app in dist/: Spotlight and Launchpad would index it as an installed app
  rm -rf "$DIST/Lanvia.app"

  # What users download and install: one package per chip, carrying only that chip's binary, half the size
  for arch in arm64 amd64; do
    local thin="$tmp/$arch/Lanvia.app"
    mkdir -p "$tmp/$arch" && cp -R "$app" "$thin"
    cp "$tmp/lanvia-$arch" "$thin/Contents/MacOS/Lanvia"
    sign_app "$thin"
    build_mac_dmg "$thin" "$arch"
  done
  rm -rf "$tmp"
  echo "  ✓ Lanvia.app → Lanvia-macos.zip（通用，自动更新用）、Lanvia-macos-apple-silicon.dmg、Lanvia-macos-intel.dmg"
}

# DMG: contains the macOS installer (the "安装 Lanvia.pkg" package) and the install guide. An
# installer rather than drag and drop: it has a welcome page and a progress bar, always installs
# into /Applications and opens itself when done. The apps it installs carry no quarantine flag.
build_mac_dmg() {
  local app=$1 arch=$2 file name name_en host
  case $arch in
    arm64) file=apple-silicon; name="Apple 芯片"; name_en="Apple silicon"; host=arm64 ;;
    amd64) file=intel; name="Intel 芯片"; name_en="Intel"; host=x86_64 ;;
  esac
  local tmp root res stage tpl=cmd/lanvia-desktop/build/pkg
  tmp=$(mktemp -d); root="$tmp/root"; res="$tmp/res"; stage="$tmp/stage"
  mkdir -p "$root/Applications" "$res" "$stage"
  cp -R "$app" "$root/Applications/Lanvia.app"
  # Do not allow relocating: otherwise an existing Lanvia.app elsewhere would take the new version instead of /Applications
  pkgbuild --analyze --root "$root" "$tmp/component.plist" >/dev/null
  plutil -replace 0.BundleIsRelocatable -bool NO "$tmp/component.plist"
  pkgbuild --quiet --root "$root" --component-plist "$tmp/component.plist" --identifier app.lanvia.desktop \
    --version "$VERSION" --install-location / --scripts "$tpl/scripts" "$tmp/component.pkg"
  local subst="s/{{VERSION}}/$VERSION/g; s/{{ARCH_NAME}}/$name/g; s/{{ARCH_NAME_EN}}/$name_en/g; s/{{HOST_ARCH}}/$host/g"
  sed "$subst" "$tpl/distribution.xml" > "$tmp/distribution.xml"
  for f in welcome.html conclusion.html; do sed "$subst" "$tpl/resources/$f" > "$res/$f"; done
  productbuild --quiet --distribution "$tmp/distribution.xml" --resources "$res" --package-path "$tmp" "$stage/安装 Lanvia.pkg"
  cp cmd/lanvia-desktop/build/dmg-readme.txt "$stage/安装说明 Install Guide.txt"
  hdiutil create -quiet -volname "Lanvia $VERSION（$name）" -srcfolder "$stage" -fs HFS+ -format UDZO \
    -imagekey zlib-level=9 -ov "$DIST/Lanvia-macos-$file.dmg"
  rm -rf "$tmp"
}

build_windows_desktop() {
  # Build the resource file with the icon and the high-DPI manifest (.syso is linked in by go build)
  (cd cmd/lanvia-desktop && go run github.com/tc-hib/go-winres@v0.3.3 simply \
    --arch amd64,arm64 --manifest gui --icon build/appicon-windows.png \
    --product-name Lanvia --file-description "Lanvia" --original-filename Lanvia.exe \
    --product-version "$VERSION" --file-version "$VERSION")
  for arch in amd64 arm64; do
    GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS -H windowsgui" \
      -o "$DIST/Lanvia-windows-$arch.exe" ./cmd/lanvia-desktop
    echo "  ✓ Lanvia-windows-$arch.exe"
  done
  rm -f cmd/lanvia-desktop/rsrc_windows_*.syso
}

echo "Lanvia $VERSION"
case $WHAT in
  cli) build_cli ;;
  desktop) build_mac_app; build_windows_desktop ;;
  mac) build_mac_app ;;
  all) build_cli; build_mac_app; build_windows_desktop ;;
  *) echo "用法: $0 [all|cli|desktop|mac]"; exit 1 ;;
esac
(cd "$DIST" && rm -f SHA256SUMS.txt && shasum -a 256 *.dmg *.zip *.exe *.tar.gz 2>/dev/null > SHA256SUMS.txt)
echo "完成 → $DIST/"
