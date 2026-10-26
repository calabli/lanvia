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

# 临时签名并严格校验。没有 Developer ID 证书时，Apple 芯片也要求二进制至少有一个完整的签名；
# 签名不完整时 macOS 会直接报“已损坏”，签名完整时只提示“无法验证开发者”，可以放行。
# 先清掉附加属性（Finder 信息、资源分支），否则签名会失败或被判为无效。
sign_app() {
  xattr -cr "$1"
  codesign --force --deep -s - --identifier app.lanvia.desktop "$1" >/dev/null
  codesign --verify --deep --strict "$1" || { echo "签名校验失败：$1" >&2; exit 1; }
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

  # 通用版（Apple 芯片和 Intel 都能用）：打成 zip 给自动更新用（旧版本按这个文件名找 Mac 安装包）
  sign_app "$app"
  rm -f "$DIST/Lanvia-macos.zip" "$DIST/Lanvia-macos.dmg" "$DIST"/Lanvia-macos-*.dmg
  ditto -c -k --keepParent "$app" "$DIST/Lanvia-macos.zip"
  # 不在 dist 里留未打包的 Lanvia.app：Spotlight 和启动台会把它当成已安装的应用收录
  rm -rf "$DIST/Lanvia.app"

  # 给用户下载安装的：每种芯片一个安装包，只带对应芯片的程序，体积小一半
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

# DMG：里面是 macOS 安装器（“安装 Lanvia.pkg”）和安装说明。用安装器而不是拖拽：
# 有欢迎页和进度条，固定装到“应用程序”（装好后出现在启动台和 Spotlight），装完自动打开；
# 安装器装上的程序不带隔离标记，所以放行一次安装器之后，打开 Lanvia 不会再被拦。
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
  # 不允许“搬家”：否则电脑上别处已有 Lanvia.app 时，安装器会把新版本装到那里，而不是“应用程序”
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
  mac) build_mac_app ;;
  all) build_cli; build_mac_app; build_windows_desktop ;;
  *) echo "用法: $0 [all|cli|desktop|mac]"; exit 1 ;;
esac
(cd "$DIST" && rm -f SHA256SUMS.txt && shasum -a 256 *.dmg *.zip *.exe *.tar.gz 2>/dev/null > SHA256SUMS.txt)
echo "完成 → $DIST/"
