# Operating Peloton

Peloton is an invite-only, self-hosted service. This guide covers deployment dependencies, backups, and operational checks. [Member visibility](visibility-model.md)

## Pre-flight checklist

- [ ] **Secrets** — `cairn gen-secrets > cairn.secrets.env`, store in a secrets
      manager, inject as env. Never commit. The dev compose's fixed
      `dev-only-*` secrets must NOT ship.
  - `CAIRN_AUTH_SESSION_SECRET`, `CAIRN_AUTH_MASTER_ENCRYPTION_KEY` (the master
    key encrypts all secrets-at-rest: OAuth tokens, webhook signing secrets,
    NATS account key — losing it means re-entering them all). OIDC client
    secrets are NOT stored: they come from `CAIRN_OIDC_*` env at runtime.
- [ ] **No dev admin surface** — the legacy token-gated `/admin/*` smoketest
      layer has been **removed from the binary**. Operator actions live on the
      admin-session-gated `/api/admin/*` surface (NATS key bootstrap/rotate,
      DLQ, geocode backfill, worker onboarding, invites). Nothing to disable.
- [ ] **TLS / proxy** — terminate TLS at a reverse proxy; set
      `CAIRN_HTTP_PUBLIC_BASE_URL` to the public https origin (drives OIDC
      redirect URIs, webhook callback URLs, password-reset + share links).
      Confirm `X-Forwarded-*` is set and `CAIRN_INSTANCE_TRUSTED_PROXIES` lists
      the proxy so per-IP rate limiting keys on the real client.
- [ ] **CSP** — a strict Content-Security-Policy belongs at the proxy (a wrong
      one silently breaks the SPA). The Go server + Caddy set the baseline
      security headers (`nosniff`, `DENY`, HSTS-when-TLS, `frame-ancestors`).
- [ ] **Migrations** — see below. Decide auto vs. explicit.
- [ ] **Backups** — see below. Verify a restore before you need one.
- [ ] **Email** — point `CAIRN_EMAIL_SMTP_*` at a real relay
      (`STARTTLS=true` + `SMTP_USERNAME/PASSWORD`), not the dev Mailpit catcher.
- [ ] **NATS auth-callout** — bootstrap the account key
      (`POST /api/admin/nats/bootstrap-account-key`), run NATS with
      `docker/nats-auth-callout.conf` (issuer = the returned public key), set
      `CAIRN_NATS_AUTH_CALLOUT=true` + `CAIRN_NATS_WORKER_ACCOUNT=CAIRN` on
      core, and onboard workers via enrollment tokens (not the dev anonymous
      path). See `docs/architecture.md` §4.5.

## Migrate-on-deploy

The schema is goose migrations embedded in the binary; the running version is
checked at startup (see `cairn serve` doc: current / older+AUTO_MIGRATE /
older / newer).

- **Single-node**: set `CAIRN_DATABASE_AUTO_MIGRATE=true` — startup applies
  pending migrations, then serves.
- **Multi-replica**: leave AUTO_MIGRATE **off**. Run `cairn migrate up` as a
  discrete pipeline step (a pre-deploy job) before rolling out the new binary,
  so two replicas never race the same migration. The binary refuses to start
  against an older schema, which gates the rollout safely.
- TimescaleDB note: migrations run on a dedicated **simple-protocol** pgx
  connection (continuous-aggregate DDL fails over the default prepared
  protocol) — already handled in `db.go`, but relevant if you run migrations
  out-of-band.

## Backup & restore

Three stateful stores; back up all three consistently.

1. **Postgres / TimescaleDB / PostGIS** (the source of truth — activities,
   streams hypertable + continuous aggregates, segments, metrics, users,
   encrypted secrets).
   - Use `pg_dump`/`pg_restore` or a Timescale-aware physical backup
     (`pg_basebackup` + WAL archiving / `pgBackRest`). The streams hypertable
     is large; a logical dump is slow at scale — prefer physical + PITR for
     anything sizeable.
   - **CAggs**: after a logical restore, the continuous aggregates exist but
     their refresh policies cover only a recent window. Historical buckets are
     materialised on demand by `StreamRepo.RefreshAggregates` after each
     ingest; a bulk backfill is a one-time `refresh_continuous_aggregate` over
     the historical range (see [the architecture notes](architecture.md)).
2. **Object storage (S3/MinIO)** — archived raw blobs (`raw_blob_id`). Back up
   the bucket. Losing it disables per-source download + quota-free re-parse,
   but activities + streams survive (re-fetch from the provider restores blobs).
3. **NATS JetStream** — job/result/event streams + KV (presence, rate limits,
   manifests). These are **transient/operational**, not source of
   truth — a fresh NATS loses in-flight jobs (re-driven on the next reconcile)
   but no durable data. The one thing to preserve is the **NATS account signing
   key**, which lives encrypted in Postgres (`instance_signing_keys`), so a
   Postgres backup already covers it.

**The master key is the crown jewel.** All at-rest secrets are AES-GCM-encrypted
with `CAIRN_AUTH_MASTER_ENCRYPTION_KEY` (AAD-bound to the row). Back it up
*separately* from the database. Rotate with `cairn rotate-key --new-key=<new>`.

## Monitoring

- **Liveness**: `GET /healthz` (cheap; the container healthcheck).
- **Readiness**: `GET /readyz` — checks Postgres + (when wired) NATS + S3;
  returns 503 + the failing dependency. Route traffic on this.
- **Metrics**: `GET /metrics` (Prometheus). Internal-only — not exposed on the
  public origin. `cairn_http_*` request counters/histograms (path-normalised),
  `go_*`/`process_*`, `cairn_build_info`.
- **DLQ**: jobs that exhaust redelivery are captured in `dead_lettered_jobs`;
  inspect/replay via `/api/admin/dlq`. Alert on a growing DLQ.

## Provider checks

The integration suite runs against disposable PostgreSQL and covers the HTTP API, authorization, and provider fixtures. Use a consenting test account for live Garmin or Strava checks. The optional Strava worker and live segment discovery have their own verification steps. [Garmin](garmin-connect.md) · [Strava](strava-connect.md) · [Segment discovery](../experiments/strava-segments/DYNAMIC_DISCOVERY.md)
