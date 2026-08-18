#!/usr/bin/env bash
# Concurrent local stack: Go (Air) + Vite HMR.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

export PATH="$(go env GOPATH)/bin:${PATH}"

: "${KC_DATABASE_URL:=postgres://kc:kc@localhost:5433/knowledge_core?sslmode=disable}"
: "${KC_AUTH_MODE:=bootstrap}"
: "${KC_BOOTSTRAP_PASSWORD_FILE:=$ROOT/.tmp/admin.password}"
export KC_DATABASE_URL KC_AUTH_MODE KC_BOOTSTRAP_PASSWORD_FILE

mkdir -p .tmp web/ui/dist
cat > web/ui/dist/index.html <<'HTML'
<!doctype html>
<title>Knowledge Core</title>
<p>Embedded UI is a stub. Use Vite: <a href="http://127.0.0.1:5173/ui/">http://127.0.0.1:5173/ui/</a></p>
HTML

if [[ ! -d web/ui/node_modules ]]; then
  (cd web/ui && npm install)
fi

go install github.com/air-verse/air@v1.63.9

port_in_use() {
  ss -tln 2>/dev/null | rg -q ":${1} " || \
    netstat -tln 2>/dev/null | rg -q ":${1} "
}

port_owner() {
  ss -tlnp 2>/dev/null | rg ":${1} " || \
    netstat -tlnp 2>/dev/null | rg ":${1} " || true
}

if port_in_use 5173; then
  echo "Port 5173 is already in use (Vite dev server)." >&2
  port_owner 5173 >&2
  echo >&2
  echo "Stop the old process, e.g.:" >&2
  echo "  fuser -k 5173/tcp" >&2
  echo "Or find it: ss -tlnp | rg 5173" >&2
  exit 1
fi

if port_in_use 8080; then
  echo "Port 8080 is already in use (Go backend)." >&2
  port_owner 8080 >&2
  echo >&2
  echo "Stop the old process, e.g.:" >&2
  echo "  fuser -k 8080/tcp" >&2
  echo "Or: podman stop knowledge-core_app_1" >&2
  exit 1
fi

print_banner() {
  echo
  echo "  Frontend (Vite HMR):  http://127.0.0.1:5173/ui/"
  echo "  Backend API:          http://127.0.0.1:8080"
  echo "  Embedded UI (stub):   http://127.0.0.1:8080/ui/"
  if [[ -n "${1:-}" ]]; then
    echo
    echo "  Bootstrap password:   ${1}"
    echo "  (soubor: ${KC_BOOTSTRAP_PASSWORD_FILE})"
  fi
  echo
  echo "  Ctrl+C zastaví oba procesy."
  echo
}

wait_for_password() {
  local i
  for i in $(seq 1 50); do
    if [[ -s "$KC_BOOTSTRAP_PASSWORD_FILE" ]]; then
      tr -d '\n' < "$KC_BOOTSTRAP_PASSWORD_FILE"
      return 0
    fi
    sleep 0.2
  done
  return 1
}

existing_pw=""
if [[ -s "$KC_BOOTSTRAP_PASSWORD_FILE" ]]; then
  existing_pw="$(tr -d '\n' < "$KC_BOOTSTRAP_PASSWORD_FILE")"
fi
print_banner "$existing_pw"

pids=()
cleanup() {
  trap - INT TERM EXIT
  for pid in "${pids[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

air -c .air.toml &
pids+=($!)

(cd web/ui && npm run dev) &
pids+=($!)

if [[ "$KC_AUTH_MODE" == "bootstrap" ]]; then
  if pw="$(wait_for_password)"; then
    if [[ -z "$existing_pw" ]]; then
      echo
      echo "  Bootstrap password:   ${pw}"
      echo "  (soubor: ${KC_BOOTSTRAP_PASSWORD_FILE})"
      echo
    fi
  else
    echo "  Varování: bootstrap heslo se zatím neobjevilo v ${KC_BOOTSTRAP_PASSWORD_FILE}" >&2
  fi
fi

# Stop the stack if either process exits.
wait -n "${pids[@]}" || true
cleanup
