#!/bin/bash
# 把 dist/ 里的安装包发布到 GitHub Release：先建草稿，逐个上传（失败重试 3 次），全部传完再正式发布。
# 先运行 ./scripts/build.sh 打包。
#
#   ./scripts/release.sh v0.14.0 "Lanvia v0.14.0 · 标题" notes.md
#
# 注意：桌面版的自动更新按 Lanvia-macos.zip、Lanvia-windows-<架构>.exe 和 SHA256SUMS.txt 找安装包，发布里要有这些文件。
set -u
TAG=$1; TITLE=$2; NOTES=$3
REPO=calabli/lanvia
cd "$(dirname "$0")/.."
[ -f dist/SHA256SUMS.txt ] || { echo "dist/ 里没有安装包，请先运行 ./scripts/build.sh" >&2; exit 1; }
(cd dist && shasum -a 256 -c --quiet SHA256SUMS.txt) || { echo "dist/ 里的文件和校验值不一致" >&2; exit 1; }
if ! gh release view "$TAG" --repo $REPO >/dev/null 2>&1; then
  gh release create "$TAG" --repo $REPO --draft --target "$(git rev-parse HEAD)" --title "$TITLE" --notes-file "$NOTES" >/dev/null || { echo "创建草稿失败" >&2; exit 1; }
  echo "draft created"
fi
# 校验文件先传，免得自动更新找到了安装包却没有对应的校验值
for f in dist/SHA256SUMS.txt dist/Lanvia-macos.zip dist/Lanvia-macos-*.dmg dist/Lanvia-windows-*.exe dist/lanvia-cli-*.tar.gz dist/lanvia-cli-*.exe; do
  [ -f "$f" ] || continue
  for try in 1 2 3; do
    start=$(date +%s)
    if gh release upload "$TAG" "$f" --repo $REPO --clobber >/dev/null 2>&1; then
      echo "uploaded $(basename "$f") ($(( $(date +%s) - start ))s)"; break
    fi
    echo "retry $try: $(basename "$f")"; sleep 5
    [ $try = 3 ] && { echo "FAILED $(basename "$f")" >&2; exit 1; }
  done
done
gh release edit "$TAG" --repo $REPO --draft=false --latest >/dev/null && echo "published"
