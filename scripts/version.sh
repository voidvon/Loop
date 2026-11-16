#!/bin/bash
set -euo pipefail

project_dir="$(cd "$(dirname "$0")/.." && pwd)"
version="${2:-$(cat "$project_dir/VERSION")}"
if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "无效版本号：$version" >&2
  exit 1
fi
IFS=. read -r major minor patch <<< "$version"
if (( minor > 20 || patch > 20 )); then
  echo '次版本和修订号必须在 0–20 之间。' >&2
  exit 1
fi
case "${1:-current}" in
  current) printf '%s\n' "$version" ;;
  next)
    if (( patch < 20 )); then
      patch=$((patch + 1))
    elif (( minor < 20 )); then
      minor=$((minor + 1))
      patch=0
    else
      major=$((major + 1))
      minor=0
      patch=0
    fi
    printf '%s.%s.%s\n' "$major" "$minor" "$patch"
    ;;
  *) echo '用法：bash scripts/version.sh [current|next] [版本号]' >&2; exit 1 ;;
esac
