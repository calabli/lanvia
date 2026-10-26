#!/usr/bin/env bash
# 打包所有发布产物到 dist/。需要在 macOS 上运行（Mac 桌面版依赖 cgo 和 Xcode 工具链）。
#
#   scripts/build.sh            # 全部
#   scripts/build.sh desktop    # 只打桌面客户端
#   scripts/build.sh cli        # 只打命令行版
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
      # Mac 用 cgo 编译，剪贴板走原生 NSPasteboard（不必每次轮询都启动 pbpaste）
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

build_mac_app() {
  local tmp app
  tmp=$(mktemp -d)
  app="$tmp/Lanvia.app"
  # 与 Go 1.26 的最低要求（macOS 13）一致；不设置时会按本机 SDK 的最新系统版本链接
  export MACOSX_DEPLOYMENT_TARGET=13.0
  for arch in arm64 amd64; do
    clang_arch=$([ $arch = amd64 ] && echo x86_64 || echo arm64)
    GOOS=darwin GOARCH=$arch CGO_ENABLED=1 CC="clang -arch $clang_arch" \
      CGO_CFLAGS="-mmacosx-version-min=13.0" CGO_LDFLAGS="-mmacosx-version-min=13.0" \
      go build -trimpath -ldflags "$LDFLAGS" -o "$tmp/lanvia-$arch" ./cmd/lanvia-desktop
  done
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  lipo -create -output "$app/Contents/MacOS/Lanvia" "$tmp/lanvia-arm64" "$tmp/lanvia-amd64"

  # 图标：从 1024 的 PNG 生成 .icns
  local iconset="$tmp/AppIcon.iconset"
  mkdir -p "$iconset"
  for s in 16 32 128 256 512; do
    sips -z $s $s cmd/lanvia-desktop/build/appicon.png --out "$iconset/icon_${s}x${s}.png" >/dev/null
    sips -z $((s*2)) $((s*2)) cmd/lanvia-desktop/build/appicon.png --out "$iconset/icon_${s}x${s}@2x.png" >/dev/null
  done
  iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

  sed "s/{{VERSION}}/$VERSION/g" cmd/lanvia-desktop/build/Info.plist > "$app/Contents/Info.plist"
  # 临时签名：没有 Developer ID 证书时，Apple 芯片也要求二进制至少有一个完整的签名。
  # 签名不完整时，从网上下载后 macOS 会直接报“已损坏”；签名完整时只提示“无法验证开发者”，可以放行。
  # 先清掉附加属性（Finder 信息、资源分支），否则签名会失败或被判为无效。
  xattr -cr "$app"
  codesign --force --deep -s - --identifier app.lanvia.desktop "$app" >/dev/null
  codesign --verify --deep --strict "$app" || { echo "签名校验失败" >&2; exit 1; }
  rm -f "$DIST/Lanvia-macos.zip"
  # zip 给自动更新用（旧版本按这个文件名找 Mac 安装包）
  ditto -c -k --keepParent "$app" "$DIST/Lanvia-macos.zip"
  rm -rf "$DIST/Lanvia.app" && cp -R "$app" "$DIST/Lanvia.app"
  build_mac_dmg "$app"
  rm -rf "$tmp"
  echo "  ✓ Lanvia.app (universal) → Lanvia-macos.zip, Lanvia-macos.dmg"
}

# DMG：给用户手动安装用。里面是 App、“应用程序”的快捷方式和安装说明（包括第一次打开被 macOS 拦下时怎么放行）。
build_mac_dmg() {
  local app=$1 stage
  stage=$(mktemp -d)
  cp -R "$app" "$stage/Lanvia.app"
  ln -s /Applications "$stage/应用程序 Applications"
  cp cmd/lanvia-desktop/build/dmg-readme.txt "$stage/安装说明 Install Guide.txt"
  rm -f "$DIST/Lanvia-macos.dmg"
  hdiutil create -quiet -volname "Lanvia $VERSION" -srcfolder "$stage" -fs HFS+ -format UDZO -imagekey zlib-level=9 -ov "$DIST/Lanvia-macos.dmg"
  rm -rf "$stage"
}

build_windows_desktop() {
  # 生成带图标和高 DPI 清单的资源文件（.syso 会被 go build 自动链接）
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
  all) build_cli; build_mac_app; build_windows_desktop ;;
  *) echo "用法: $0 [all|cli|desktop]"; exit 1 ;;
esac
(cd "$DIST" && rm -f SHA256SUMS.txt && shasum -a 256 *.dmg *.zip *.exe *.tar.gz 2>/dev/null > SHA256SUMS.txt)
echo "完成 → $DIST/"
