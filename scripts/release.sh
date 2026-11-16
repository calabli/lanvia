#!/bin/bash
# Publish the installers in dist/ to a GitHub Release: create a draft first, upload them one by one
# (retry 3 times on failure), then publish once everything is up. Run ./scripts/build.sh first.
#
#   ./scripts/release.sh v0.14.0 "Lanvia v0.14.0 · title" notes.md
#
# Note: the desktop auto updater looks for the installers by Lanvia-macos.zip, Lanvia-windows-<arch>.exe and SHA256SUMS.txt, so the release has to contain those files.
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
# Upload the checksum file first, so the auto updater never finds an installer without matching checksums
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
