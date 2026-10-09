#!/usr/bin/env bash
set -euo pipefail
repo=ZevaierZhan/guarded-assist
version=${1:-}
use_gh=false
if command -v gh >/dev/null && gh auth status >/dev/null 2>&1; then use_gh=true; fi
if [[ -z "$version" ]]; then
  if "$use_gh"; then
    version=$(gh release view --repo "$repo" --json tagName --jq .tagName)
  else
    resolved=$(curl -fsSL --retry 2 -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
    version=${resolved##*/}
  fi
fi
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid stable Release version' >&2; exit 1; }
case "$(uname -s)" in Darwin) platform=darwin ;; Linux) platform=linux ;; *) echo 'Unsupported OS' >&2; exit 1 ;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64) arch=amd64 ;; *) echo 'Unsupported architecture' >&2; exit 1 ;; esac
destination=${2:-"${XDG_CACHE_HOME:-$HOME/.cache}/guarded-assist/releases/$version/$platform-$arch"}
asset="guarded-assist-server-$platform-$arch.tar.gz"
base="https://github.com/$repo/releases/download/$version"
task_temp=$(mktemp -d)
trap 'rm -f "$task_temp/SHA256SUMS" "$task_temp/$asset"; rmdir "$task_temp"' EXIT
if "$use_gh"; then
  gh release download "$version" --repo "$repo" --pattern SHA256SUMS --pattern "$asset" --dir "$task_temp"
else
  curl -fsSL --retry 2 "$base/SHA256SUMS" -o "$task_temp/SHA256SUMS"
  curl -fsSL --retry 2 "$base/$asset" -o "$task_temp/$asset"
fi
expected=$(awk -v name="$asset" '$2==name {print $1}' "$task_temp/SHA256SUMS")
[[ "$expected" =~ ^[a-f0-9]{64}$ ]] || { echo 'Checksum entry missing or ambiguous' >&2; exit 1; }
if command -v sha256sum >/dev/null; then actual=$(sha256sum "$task_temp/$asset"); else actual=$(shasum -a 256 "$task_temp/$asset"); fi
[[ "${actual%% *}" == "$expected" ]] || { echo 'Release checksum mismatch' >&2; exit 1; }
if [[ -d "$destination" ]]; then
  [[ -x "$destination/assistctl" ]] || { echo 'Destination exists but is incomplete; choose another destination' >&2; exit 1; }
else
  mkdir -p "$destination"
  tar -xzf "$task_temp/$asset" -C "$destination"
fi
destination=$(cd "$destination" && pwd)
# JSON-escape path characters without requiring Python, Node or jq.
escaped=${destination//\\/\\\\}; escaped=${escaped//\"/\\\"}
printf '{"version":"%s","cli":"%s/assistctl","directory":"%s"}\n' "$version" "$escaped" "$escaped"
