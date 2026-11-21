# Running Cairn locally

Local development runs the **supporting services in Docker Compose** and the
**Cairn server and web app on the host**. See [Phase 1](../PROJECT_PLAN.md#1-start-the-unchanged-application-locally)
for the toolchain and startup checks. File uploads work without a provider worker.

## 1. Supporting services (compose)

`docker-compose.dev.yml` is dependencies-only — Postgres/TimescaleDB/PostGIS,
NATS (JetStream), Silo (the community MinIO fork, service name `minio`), and
Mailpit. Images are pinned and ports bind to `127.0.0.1`. The `peloton-dev`
project name scopes the containers, network and volumes.

```sh
make dev-up        # start dependencies under peloton-dev
make dev-logs      # tail
make dev-down      # stop, keep data   (make dev-down ARGS=-v to wipe volumes)
```

| Service | Host port | Notes |
| --- | --- | --- |
| Postgres | `localhost:5432` | `cairn` / `cairn` / db `cairn` |
| NATS | `localhost:4222` (mon `:8222`) | JetStream; `/varz`, `/jsz` |
| Silo (`minio`) | `localhost:9000` (console `:9001`) | `cairn` / `cairn-secret` |
| Mailpit | SMTP `localhost:1025` (UI `:8025`) | read notification emails |

## 2. Env

```sh
cp dev.env.example dev.env     # once; matches the compose services + dev secrets
```

`make dev-server` / `make dev-web` source `dev.env` automatically. It sets
`CAIRN_DATABASE_URL`, `CAIRN_NATS_URL`, the `CAIRN_STORAGE_*` (MinIO), the
`CAIRN_EMAIL_SMTP_*` (Mailpit), throwaway auth secrets, and the worker identity.
`CAIRN_DATABASE_AUTO_MIGRATE=true` applies migrations on boot.

## 3. The binaries (host, live-reload)

```sh
make web-deps      # once: install locked web dependencies and generate stubs
make dev-server    # one terminal: rebuilds + restarts cmd/server on save (Air)
make dev-web       # another terminal: Vite with hot reload
```

- **API**: http://localhost:8080 (`/healthz`, `/readyz`, `/metrics`, `/api/*`,
  `/cairn.v1.*`).
- **Web**: http://localhost:5173 (change `CAIRN_WEB_PORT` and
  `CAIRN_HTTP_PUBLIC_BASE_URL` together in `dev.env` if the port is in use). The
  vite dev server proxies `/api`, `/auth`, `/webhooks`, `/cairn.v1.*`,
  `/cairn.worker.v1.*` to the Go server on :8080.
- Sign in on `/login`. Bootstrap an admin via env on first boot
  (`CAIRN_INSTANCE_BOOTSTRAP_ADMIN_*`) or create one with the admin CLI.
  The bootstrap password is temporary. Choose a new password on first sign-in.

`make dev-server` downloads the Air version pinned in the Makefile. Its build
output lives in the ignored `tmp/` directory. For a server without live reload,
run `( set -a; . ./dev.env; set +a; make run )` from the repository root.

## The worker / provider plugin

Provider workers live in separate repositories. For local development, use
FIT, GPX or TCX uploads to exercise ingest, merge, best efforts, segment
matching and training load. Peloton’s local stack leaves Strava activity sync
disabled.

### Production worker auth

The dev path has no per-worker permission scoping. For a real deployment,
configure NATS auth-callout, mint an enrollment token (`POST
/api/admin/worker-enrollments`), and set `CAIRN_WORKER_ENROLLMENT_TOKEN` on the
worker. See [the architecture guide](architecture.md#45-worker-enrollment--dynamic-nats-credentials).

## Dev vs prod topology

The frontend is a **client-rendered SPA** (`ssr=false`), so dev and prod render
identically — only the packaging differs:

| | Dev (host binaries) | Prod (`docker/Dockerfile.core`) |
| --- | --- | --- |
| Frontend | vite dev server (HMR), proxies to the API | static SPA **embedded in the Go binary** |
| Routing | vite proxies API paths to :8080 | the one binary serves `/` + `/api` + `/cairn.v1.*` |
| Processes | server and Vite on the host | **one** distroless container and its dependencies, plus any optional workers |

## Production image (single distroless binary)

```sh
docker build -f docker/Dockerfile.core -t cairn .
```

Three stages: build the SvelteKit static SPA (`npm run build:static`,
adapter-static) → compile the Go server with `-tags embedweb`, embedding the SPA
from `internal/adapter/primary/web/dist` → **`gcr.io/distroless/static-debian12:nonroot`**
runtime (nonroot uid 65532, no shell, ca-certs + tzdata, ~56 MB). The resulting
`cairn serve` answers both `/cairn.v1.*` and `/` (the embedded UI with
client-side-routing fallback). Point it at Postgres/NATS/MinIO via the same
`CAIRN_*` env; no Node, no Caddy, no separate web deployment.

The [build-core workflow](../.github/workflows/build-core.yml) uses the same
Dockerfile. It generates Protobuf stubs in the build image with local Buf
plugins. Provider workers publish their own images.

## Secrets

`dev.env` holds throwaway dev secrets so the stack runs with zero setup. **Never
ship them.** For production generate real values (`cairn gen-secrets`), inject as
env, and switch the worker to the enrollment-token path — see
`docs/production-readiness.md`.
