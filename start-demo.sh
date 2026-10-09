#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
exec ./assist-server-linux-amd64 "$@"
