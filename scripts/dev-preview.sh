#!/usr/bin/env bash
# Run the local Compose dependencies, Go watcher and Vite in the background.
set -euo pipefail
cd "$(dirname "$0")/.."
umask 077
state="$PWD/.dev-preview"
mkdir -p "$state"
exec 9>"$state/start.lock"
flock 9

# Ignore application settings inherited from a production shell or dev.env.
while IFS= read -r name; do unset "$name"; done < <(compgen -e | sort | sed -n '/^CAIRN_/p')
export NO_PROXY="127.0.0.1,localhost,${NO_PROXY:-}" no_proxy="127.0.0.1,localhost,${no_proxy:-}"
port="${PELOTON_PREVIEW_PORT:-3000}"
public_url="${PELOTON_PREVIEW_URL:-http://localhost:$port}"

if [[ -n "${DOCKER_HOST:-}${DOCKER_CONTEXT:-}" ||
      "$(docker context inspect default --format '{{.Endpoints.docker.Host}}')" != unix:///var/run/docker.sock ]]; then
    echo "Switch to the local default Docker context before starting the preview." >&2
    exit 1
fi

# BusyBox wget otherwise follows HTTP_PROXY in some Cloud containers.
cat > "$state/compose.yml" <<'YAML'
services:
  nats:
    healthcheck:
      test: ["CMD", "wget", "-Y", "off", "--quiet", "--tries=1", "--spider", "http://127.0.0.1:8222/healthz"]
YAML
compose=(docker compose -p peloton-cloud -f docker-compose.dev.yml -f "$state/compose.yml")
"${compose[@]}" up -d --wait
"${compose[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U cairn -d postgres <<'SQL'
SELECT 'CREATE DATABASE peloton_preview'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'peloton_preview') \gexec
SQL

if [[ ! -s "$state/accounts.env" ]]; then
    python3 - <<'PY' > "$state/accounts.env.new"
import secrets

for key in ("CAIRN_AUTH_SESSION_SECRET", "CAIRN_AUTH_MASTER_ENCRYPTION_KEY",
            "PREVIEW_BOOTSTRAP_PASSWORD", "PREVIEW_ADMIN_PASSWORD",
            "PREVIEW_RIDER_ONE_PASSWORD", "PREVIEW_RIDER_TWO_PASSWORD"):
    print(f"export {key}={secrets.token_urlsafe(32)}")
PY
    mv "$state/accounts.env.new" "$state/accounts.env"
fi
# Use the checked-in Compose service defaults only.
source ./dev.env.example
source "$state/accounts.env"

export CAIRN_DATABASE_URL="${CAIRN_DATABASE_URL/localhost:5432\/cairn/127.0.0.1:5432/peloton_preview}"
export CAIRN_DATABASE_AUTO_MIGRATE=true
export CAIRN_NATS_URL=nats://127.0.0.1:4222
export CAIRN_STORAGE_ENDPOINT=http://127.0.0.1:9000
export CAIRN_STORAGE_BUCKET=peloton-preview
export CAIRN_EMAIL_ADAPTER=smtp CAIRN_EMAIL_FROM_ADDRESS=preview@peloton.local
export CAIRN_EMAIL_SMTP_HOST=127.0.0.1 CAIRN_EMAIL_SMTP_PORT=1025 CAIRN_EMAIL_SMTP_STARTTLS=false
export CAIRN_INSTANCE_BOOTSTRAP_ADMIN_EMAIL=admin@peloton.local
export CAIRN_INSTANCE_BOOTSTRAP_ADMIN_PASSWORD="$PREVIEW_BOOTSTRAP_PASSWORD"
export CAIRN_HTTP_ADDR=127.0.0.1:8090 CAIRN_API_ORIGIN=http://127.0.0.1:8090
export CAIRN_WEB_PORT="$port" CAIRN_HTTP_PUBLIC_BASE_URL="$public_url"
unset __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS
if [[ -n "${PELOTON_PREVIEW_URL:-}" ]]; then
    export __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS
    __VITE_ADDITIONAL_SERVER_ALLOWED_HOSTS="$(python3 -c 'import sys, urllib.parse; print(urllib.parse.urlsplit(sys.argv[1]).hostname)' "$public_url")"
fi

make web-deps
settings="$port $public_url"
previous_settings="$(cat "$state/settings" 2>/dev/null || true)"
start() {
    local service="$1" command="$2" pid attempt path="$state/$1.pid"
    if [[ -s "$path" ]]; then
        read -r pid < "$path"
        if [[ "$(ps -p "$pid" -o args=)" == *"peloton-preview-$service"* ]]; then
            if [[ "$settings" == "$previous_settings" ]]; then return; fi
            kill -TERM -- "-$pid"
            for ((attempt = 0; attempt < 30; attempt++)); do
                if ! kill -0 -- "-$pid" 2>/dev/null; then break; fi
                sleep 0.2
            done
        fi
    fi
    setsid bash -c "$command"' & wait "$!"' "peloton-preview-$service" \
        9>&- </dev/null >"$state/$service.log" 2>&1 &
    echo "$!" > "$path"
}
start backend 'make dev-preview-backend'
start frontend 'cd web && npm run dev -- --host 0.0.0.0'

wait_for() {
    local url="$1"
    for ((attempt = 0; attempt < 180; attempt++)); do
        if curl -fs -m 2 "$url" >/dev/null; then return; fi
        sleep 1
    done
    echo "Timed out at $url. See $state/*.log." >&2
    return 1
}
wait_for "$CAIRN_API_ORIGIN/readyz"
wait_for "http://127.0.0.1:$port/login"
printf '%s\n' "$settings" > "$state/settings"
if [[ ! -f "$state/seeded" ]]; then
    PYTHONDONTWRITEBYTECODE=1 python3 scripts/dev-preview-seed.py "$port" "$state"
    touch "$state/seeded"
fi
printf 'Peloton is listening on port %s. Logs: %s/*.log\n' "$port" "$state"
