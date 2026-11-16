#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/.."
project_dir="$PWD"
go run ./scripts/prepare-fyne.go
core_path="${LOOP_CORE:-$(command -v sing-box || true)}"
if [[ -z "$core_path" || ! -x "$core_path" ]]; then
  echo '找不到内核；开发机请安装 sing-box 或设置 LOOP_CORE。' >&2
  exit 1
fi
check_dir="$(mktemp -d -t loop-package)"
trap 'rm -rf "$check_dir"' EXIT
go run ./cmd/loop -profiles "$project_dir/profiles" -render-config "$check_dir/config.json"
"$core_path" check -c "$check_dir/config.json"

app_dir="$project_dir/dist/Loop.app"
mkdir -p "$app_dir/Contents/MacOS" "$app_dir/Contents/Resources/licenses"
icon_source="$project_dir/internal/desktop/assets/app-icon.png"
iconset_dir="$check_dir/Loop.iconset"
mkdir -p "$iconset_dir"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$icon_source" --out "$iconset_dir/icon_${size}x${size}.png" >/dev/null
  retina_size=$((size * 2))
  sips -z "$retina_size" "$retina_size" "$icon_source" --out "$iconset_dir/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset_dir" -o "$app_dir/Contents/Resources/Loop.icns"
background_app="$app_dir/Contents/Helpers/loop-background.app"
mkdir -p "$background_app/Contents/MacOS"
go build -trimpath -ldflags='-s -w' -o "$app_dir/Contents/MacOS/loop" ./cmd/loop
go build -trimpath -ldflags='-s -w' -o "$background_app/Contents/MacOS/loopd" ./cmd/loopd
rm -f "$app_dir/Contents/MacOS/loopd"
go build -trimpath -ldflags='-s -w' -o "$app_dir/Contents/MacOS/loopctl" ./cmd/loopctl
go build -trimpath -ldflags='-s -w' -o "$app_dir/Contents/MacOS/loop-tun" ./cmd/loop-tun
cp "$core_path" "$app_dir/Contents/MacOS/sing-box"
mkdir -p "$app_dir/Contents/Resources/profiles"
cp -R profiles/. "$app_dir/Contents/Resources/profiles/"
# Main config supersedes the two migration backups; do not ship stale sources.
rm -f "$app_dir/Contents/Resources/profiles/base.json" "$app_dir/Contents/Resources/profiles/routing.json"
find "$app_dir/Contents/Resources/profiles" -name '*.bak' -type f -delete
find "$app_dir/Contents/Resources/profiles" -type f -exec chmod 600 {} +
# Remove the obsolete generated config from earlier development bundles.
rm -f "$app_dir/Contents/Resources/config.json"
chmod 755 "$app_dir/Contents/MacOS/"*

cat > "$app_dir/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>loop</string>
  <key>CFBundleIdentifier</key><string>local.loop.desktop</string>
  <key>CFBundleName</key><string>Loop</string>
  <key>CFBundleDisplayName</key><string>Loop</string>
  <key>CFBundleIconFile</key><string>Loop.icns</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.16.18</string>
  <key>CFBundleVersion</key><string>46</string>
  <key>LSMinimumSystemVersion</key><string>15.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

cat > "$background_app/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>loopd</string>
  <key>CFBundleIdentifier</key><string>local.loop.background</string>
  <key>CFBundleName</key><string>Loop Background</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.16.18</string>
  <key>CFBundleVersion</key><string>46</string>
  <key>LSMinimumSystemVersion</key><string>15.0</string>
  <key>LSUIElement</key><true/>
</dict></plist>
PLIST

core_version="$("$core_path" version | head -n 1 | awk '{print $3}')"
if [[ ! "$core_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo '请使用正式版内核，以便记录对应源码版本。' >&2
  exit 1
fi
if [[ -n "${LOOP_CORE_LICENSE:-}" ]]; then
  cp "$LOOP_CORE_LICENSE" "$app_dir/Contents/Resources/licenses/sing-box-LICENSE"
else
  curl --connect-timeout 10 --max-time 30 -fsSL "https://raw.githubusercontent.com/SagerNet/sing-box/v${core_version}/LICENSE" \
    -o "$app_dir/Contents/Resources/licenses/sing-box-LICENSE"
fi
printf 'sing-box %s\nUpstream: https://github.com/SagerNet/sing-box\nSource: https://github.com/SagerNet/sing-box/tree/v%s\nThis package contains the unmodified core executable.\nBefore public distribution, provide corresponding source and required third-party notices.\n' \
  "$core_version" "$core_version" > "$app_dir/Contents/Resources/licenses/sing-box-SOURCE.txt"
editor_module="$(go list -m -f '{{.Dir}}' github.com/ideaconnect/go-fyne-pretty-view/v2)"
tooltip_module="$(go list -m -f '{{.Dir}}' github.com/dweymouth/fyne-tooltip)"
install -m 644 "$editor_module/LICENSE" "$app_dir/Contents/Resources/licenses/pretty-view-LICENSE"
install -m 644 "$tooltip_module/LICENSE" "$app_dir/Contents/Resources/licenses/fyne-tooltip-LICENSE"
install -m 644 third_party/fyne/LICENSE "$app_dir/Contents/Resources/licenses/fyne-LICENSE"
install -m 644 third_party/fyne/AUTHORS "$app_dir/Contents/Resources/licenses/fyne-AUTHORS"
install -m 644 third_party/fyne/LOOP_PATCHES.md "$app_dir/Contents/Resources/licenses/fyne-PATCHES.md"

# Ad-hoc signatures support local use. Public distribution needs Developer ID
# signing and notarization, including the bundled core.
codesign --force --sign - "$app_dir/Contents/MacOS/sing-box"
codesign --force --sign - "$app_dir/Contents/MacOS/loop-tun"
codesign --force --sign - "$background_app"
codesign --force --sign - "$app_dir/Contents/MacOS/loopctl"
codesign --force --sign - "$app_dir"
codesign --verify --deep --strict "$app_dir"
archive="$project_dir/dist/Loop-macos-$(uname -m).zip"
ditto -c -k --sequesterRsrc --keepParent "$app_dir" "$archive"
printf 'App: %s\nArchive: %s\n' "$app_dir" "$archive"
