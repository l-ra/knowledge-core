#!/usr/bin/env bash
# Stop local dev stack and remove temporary dev artifacts.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

kill_port() {
  local port="$1"
  local label="$2"
  if command -v fuser >/dev/null 2>&1; then
    if fuser "${port}/tcp" >/dev/null 2>&1; then
      echo "Stopping ${label} on port ${port}..."
      fuser -k "${port}/tcp" >/dev/null 2>&1 || true
      return 0
    fi
  fi
  local pids
  pids="$(ss -tlnp 2>/dev/null | rg ":${port} " | rg -o 'pid=[0-9]+' | cut -d= -f2 | sort -u || true)"
  if [[ -z "$pids" ]]; then
    echo "Port ${port} (${label}): nothing running."
    return 0
  fi
  echo "Stopping ${label} on port ${port} (pids: ${pids//$'\n'/ })..."
  while read -r pid; do
    [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
  done <<< "$pids"
}

kill_port 5173 "Vite"
kill_port 8080 "Go backend"

# Air parent processes for this repo (if still running).
while read -r pid; do
  [[ -n "$pid" ]] || continue
  if [[ "$pid" != "$$" ]]; then
    echo "Stopping Air (pid ${pid})..."
    kill "$pid" 2>/dev/null || true
  fi
done < <(pgrep -f "air -c ${ROOT}/.air.toml" 2>/dev/null || true)

sleep 0.3

removed=()
for path in tmp .tmp air.log; do
  if [[ -e "$path" ]]; then
    rm -rf "$path"
    removed+=("$path")
  fi
done

echo
if ((${#removed[@]})); then
  echo "Removed: ${removed[*]}"
else
  echo "No temp dev directories to remove."
fi
echo "Done. Run: make dev"
