#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/.."
project_dir="$PWD"
version="$(bash scripts/version.sh current)"
go run ./scripts/prepare-fyne.go
core_path="${LOOP_CORE:-$(command -v sing-box || true)}"
if [[ -z "$core_path" || ! -x "$core_path" ]]; then
  echo '找不到内核；开发机请安装 sing-box 或设置 LOOP_CORE。' >&2
  exit 1
fi
check_dir="$(mktemp -d -t loop-package)"
trap 'rm -rf "$check_dir"' EXIT
# Public packages contain only the shared config and rule snapshots. Never copy
# the developer's nodes, subscriptions, settings, selections or backups.
seed_dir="$check_dir/profiles"
mkdir -p "$seed_dir/rule-sets"
cp profiles/config.json "$seed_dir/config.json"
for name in LICENSE README.md geoip-cn.srs geosite-cn.srs; do
  cp "profiles/rule-sets/$name" "$seed_dir/rule-sets/$name"
done
# An unstarted loopback placeholder lets sing-box check the proxy policies.
printf '%s\n' '{"nodes":[{"id":"package-check","name":"package-check","outbound":{"type":"socks","server":"127.0.0.1","server_port":9}}]}' > "$seed_dir/nodes.json"
printf '%s\n' '{"subscriptions":[]}' > "$seed_dir/subscriptions.json"
go run ./cmd/loopctl -state-dir "$check_dir/state" -profiles "$seed_dir" -render-config "$check_dir/config.json"
"$core_path" check -c "$check_dir/config.json"
printf '%s\n' '{"nodes":[]}' > "$seed_dir/nodes.json"

app_dir="$check_dir/Loop.app"
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
cp -R "$seed_dir/." "$app_dir/Contents/Resources/profiles/"
# Main config supersedes the two migration backups; do not ship stale sources.
rm -f "$app_dir/Contents/Resources/profiles/base.json" "$app_dir/Contents/Resources/profiles/routing.json"
find "$app_dir/Contents/Resources/profiles" -name '*.bak' -type f -delete
find "$app_dir/Contents/Resources/profiles" -type f -exec chmod 600 {} +
# Remove the obsolete generated config from earlier development bundles.
rm -f "$app_dir/Contents/Resources/config.json"
chmod 755 "$app_dir/Contents/MacOS/"*

cat > "$app_dir/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>loop</string>
  <key>CFBundleIdentifier</key><string>local.loop.desktop</string>
  <key>CFBundleName</key><string>Loop</string>
  <key>CFBundleDisplayName</key><string>Loop</string>
  <key>CFBundleIconFile</key><string>Loop.icns</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$version</string>
  <key>CFBundleVersion</key><string>$version</string>
  <key>LSMinimumSystemVersion</key><string>15.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

cat > "$background_app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>loopd</string>
  <key>CFBundleIdentifier</key><string>local.loop.background</string>
  <key>CFBundleName</key><string>Loop Background</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$version</string>
  <key>CFBundleVersion</key><string>$version</string>
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
printf 'sing-box %s\nUpstream: https://github.com/SagerNet/sing-box\nSource: https://github.com/SagerNet/sing-box/tree/v%s\nRelease and corresponding core source archive: https://github.com/voidvon/Loop/releases/tag/v%s\nThe bundled core is distributed under GPL-3.0-or-later; see sing-box-LICENSE.\n' \
  "$core_version" "$core_version" "$version" > "$app_dir/Contents/Resources/licenses/sing-box-SOURCE.txt"
editor_module="$(go list -m -f '{{.Dir}}' github.com/ideaconnect/go-fyne-pretty-view/v2)"
tooltip_module="$(go list -m -f '{{.Dir}}' github.com/dweymouth/fyne-tooltip)"
install -m 644 "$editor_module/LICENSE" "$app_dir/Contents/Resources/licenses/pretty-view-LICENSE"
install -m 644 "$tooltip_module/LICENSE" "$app_dir/Contents/Resources/licenses/fyne-tooltip-LICENSE"
install -m 644 third_party/fyne/LICENSE "$app_dir/Contents/Resources/licenses/fyne-LICENSE"
install -m 644 third_party/fyne/AUTHORS "$app_dir/Contents/Resources/licenses/fyne-AUTHORS"
install -m 644 third_party/fyne/LOOP_PATCHES.md "$app_dir/Contents/Resources/licenses/fyne-PATCHES.md"
# Include notices from every Go module linked into the four Loop executables.
go list -deps -f '{{if .Module}}{{.Module.Path}} {{.Module.Version}} {{.Module.Dir}}{{end}}' \
  ./cmd/loop ./cmd/loopd ./cmd/loopctl ./cmd/loop-tun | sort -u > "$check_dir/modules.txt"
while read -r module module_version module_dir; do
  [[ -n "$module_dir" && "$module" != local/loop ]] || continue
  notice_dir="$app_dir/Contents/Resources/licenses/modules/$module"
  mkdir -p "$notice_dir"
  printf '%s %s\n' "$module" "$module_version" > "$notice_dir/MODULE.txt"
  find "$module_dir" -maxdepth 1 -type f \
    \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' -o -iname 'authors*' \) \
    -exec cp {} "$notice_dir/" \;
done < "$check_dir/modules.txt"
go_license="$(go env GOROOT)/LICENSE"
if [[ ! -f "$go_license" ]]; then
  go_license="$(dirname "$(go env GOROOT)")/LICENSE"
fi
install -m 644 "$go_license" "$app_dir/Contents/Resources/licenses/Go-LICENSE"

# Ad-hoc signatures support local use. Public distribution needs Developer ID
# signing and notarization, including the bundled core.
codesign --force --sign - "$app_dir/Contents/MacOS/sing-box"
codesign --force --sign - "$app_dir/Contents/MacOS/loop-tun"
codesign --force --sign - "$background_app"
codesign --force --sign - "$app_dir/Contents/MacOS/loopctl"
codesign --force --sign - "$app_dir"
codesign --verify --deep --strict "$app_dir"
mkdir -p "$project_dir/dist"
archive="$project_dir/dist/Loop-v${version}-macos-$(uname -m).zip"
ditto -c -k --sequesterRsrc --keepParent "$app_dir" "$archive"
rm -rf "$project_dir/dist/Loop.app"
ditto "$app_dir" "$project_dir/dist/Loop.app"
app_dir="$project_dir/dist/Loop.app"
printf 'App: %s\nArchive: %s\n' "$app_dir" "$archive"
