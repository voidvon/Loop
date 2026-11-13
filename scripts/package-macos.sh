#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/.."
project_dir="$PWD"
core_path="${MINIPROXY_CORE:-$(command -v sing-box || true)}"
if [[ -z "$core_path" || ! -x "$core_path" ]]; then
  echo '找不到内核；开发机请安装 sing-box 或设置 MINIPROXY_CORE。' >&2
  exit 1
fi
check_dir="$(mktemp -d -t miniproxy-package)"
trap 'rm -rf "$check_dir"' EXIT
go run ./cmd/miniproxy -profiles "$project_dir/profiles" -render-config "$check_dir/config.json"
"$core_path" check -c "$check_dir/config.json"

app_dir="$project_dir/dist/MiniProxy.app"
mkdir -p "$app_dir/Contents/MacOS" "$app_dir/Contents/Resources/licenses"
go build -trimpath -ldflags='-s -w' -o "$app_dir/Contents/MacOS/MiniProxy" ./cmd/miniproxy
go build -trimpath -ldflags='-s -w' -o "$app_dir/Contents/MacOS/miniproxy-tun" ./cmd/miniproxy-tun
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
  <key>CFBundleExecutable</key><string>MiniProxy</string>
  <key>CFBundleIdentifier</key><string>local.miniproxy.desktop</string>
  <key>CFBundleName</key><string>MiniProxy</string>
  <key>CFBundleDisplayName</key><string>MiniProxy</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.15.9</string>
  <key>CFBundleVersion</key><string>27</string>
  <key>LSMinimumSystemVersion</key><string>15.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

core_version="$("$core_path" version | head -n 1 | awk '{print $3}')"
if [[ ! "$core_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo '请使用正式版内核，以便记录对应源码版本。' >&2
  exit 1
fi
curl -fsSL "https://raw.githubusercontent.com/SagerNet/sing-box/v${core_version}/LICENSE" \
  -o "$app_dir/Contents/Resources/licenses/sing-box-LICENSE"
printf 'sing-box %s\nUpstream: https://github.com/SagerNet/sing-box\nSource: https://github.com/SagerNet/sing-box/tree/v%s\nThis package contains the unmodified core executable.\nBefore public distribution, provide corresponding source and required third-party notices.\n' \
  "$core_version" "$core_version" > "$app_dir/Contents/Resources/licenses/sing-box-SOURCE.txt"
editor_module="$(go list -m -f '{{.Dir}}' github.com/ideaconnect/go-fyne-pretty-view/v2)"
tooltip_module="$(go list -m -f '{{.Dir}}' github.com/dweymouth/fyne-tooltip)"
install -m 644 "$editor_module/LICENSE" "$app_dir/Contents/Resources/licenses/pretty-view-LICENSE"
install -m 644 "$tooltip_module/LICENSE" "$app_dir/Contents/Resources/licenses/fyne-tooltip-LICENSE"

# Ad-hoc signatures support local use. Public distribution needs Developer ID
# signing and notarization, including the bundled core.
codesign --force --sign - "$app_dir/Contents/MacOS/sing-box"
codesign --force --sign - "$app_dir/Contents/MacOS/miniproxy-tun"
codesign --force --sign - "$app_dir"
codesign --verify --deep --strict "$app_dir"
archive="$project_dir/dist/MiniProxy-macos-$(uname -m).zip"
ditto -c -k --sequesterRsrc --keepParent "$app_dir" "$archive"
printf 'App: %s\nArchive: %s\n' "$app_dir" "$archive"
