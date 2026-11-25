# Peloton

Peloton is an invite-only, self-hosted cycling platform for a small group of friends. Open **Social → Riders** to browse member profiles, rides, and cycling data, or **Social → Head-to-Head** to compare records and segment efforts. [Member visibility](docs/visibility-model.md)

## Features

- **Waasland route planner.** Public route planning for climbs, cobbles, gravel and uninterrupted riding, up to 200 km, with wind, named places, direction preferences and GPX export. [Planner documentation](docs/waasland-planner.md)

- **Garmin Connect sync.** Connect a Garmin Personal API key in **Settings → Connections → Garmin Connect** to import available cycling history and check for new rides. Use **Sync Now** to start a check yourself. Garmin rides without a usable recording are imported as summaries. [Garmin Connect setup](docs/garmin-connect.md)
- **Optional Strava sync.** Connect an authorized Strava account, import cycling history, and check for new rides. Peloton combines matching Garmin, Strava, and file recordings into one ride. [Strava setup and access requirements](docs/strava-connect.md)
- **File imports.** Upload FIT, GPX or TCX files from **Activities**. File imports work independently of Garmin.
- **Shared segments.** Peloton discovers Strava segment definitions along imported GPS routes and caches them locally. **All Segments** shows the group catalog. **My Segments** filters it to segments you’ve completed. Each Local KOM leaderboard ranks members by their fastest valid effort, calculated from their Peloton rides. The catalog, leaderboards and activity details show average speed in km/h.
- **Cycling analysis.** Review ride statistics, elevation and sensor charts, personal records, training load and time in zone. Use the personal heatmap to see where you ride most often.

Segment discovery uses the public Tailwind KOMpass map feed. Coverage varies by area, and provider changes, outages or request limits can prevent discovery. Peloton queries geographic bounds and matches the GPS tracks locally. Cached definitions remain available for local matching. Segment discovery needs a recorded GPS track. [Provider research](experiments/strava-segments/DYNAMIC_DISCOVERY.md)

## Run with Docker

Install Docker Engine or Docker Desktop with the Docker Compose plugin. The repository’s Compose file supplies PostgreSQL with TimescaleDB and PostGIS, NATS with JetStream, and S3-compatible object storage. Build the Peloton image from the repository. It serves the web app and API from one container.

```sh
git clone https://github.com/Pomonoli/Peloton.git
cd Peloton
docker build -f docker/Dockerfile.core -t peloton:local .
docker compose -f docker-compose.dev.yml up -d --wait postgres nats minio
```

On first setup, generate secrets into a file outside the checkout. Keep this file for future updates.

```sh
(umask 077; set -C; docker run --rm peloton:local gen-secrets > ../peloton.env)
```

Add these settings to `../peloton.env` as plain `KEY=value` lines. Replace the placeholders. Use the database and object storage credentials from [the Compose file](docker-compose.dev.yml) for this local setup.

```dotenv
CAIRN_DATABASE_URL=postgres://<postgres-user>:<postgres-password>@postgres:5432/cairn?sslmode=disable
CAIRN_DATABASE_AUTO_MIGRATE=true
CAIRN_NATS_URL=nats://nats:4222
CAIRN_STORAGE_ENDPOINT=http://minio:9000
CAIRN_STORAGE_ACCESS_KEY_ID=<object-storage-user>
CAIRN_STORAGE_SECRET_ACCESS_KEY=<object-storage-password>
CAIRN_STORAGE_BUCKET=cairn
CAIRN_HTTP_PUBLIC_BASE_URL=http://localhost:8080
CAIRN_INSTANCE_BOOTSTRAP_ADMIN_EMAIL=<admin-email>
CAIRN_INSTANCE_BOOTSTRAP_ADMIN_PASSWORD=<temporary-admin-password>
CAIRN_SEGMENT_DISCOVERY_ENABLED=true
```

Start Peloton on the network created by the Compose file.

```sh
docker run -d --name peloton --restart unless-stopped \
  --network peloton-dev_cairn --env-file ../peloton.env \
  -p 127.0.0.1:8080:8080 peloton:local
```

Open `http://localhost:8080`, sign in with the bootstrap admin account and change its temporary password. The bundled dependency stack uses development credentials and unauthenticated NATS. For a shared instance, secure the dependencies, keep their ports private, put Peloton behind an HTTPS reverse proxy, and set `CAIRN_HTTP_PUBLIC_BASE_URL` to the address members use. Use `docker run --rm peloton:local config help` to list settings. Existing configuration keys retain the `CAIRN_` prefix.

### Update

Back up the database and object storage, and keep the environment file and master encryption key. From the same checkout, pull and rebuild before replacing the application container.

```sh
git pull --ff-only origin main
docker build -f docker/Dockerfile.core -t peloton:local .
docker compose -f docker-compose.dev.yml up -d --wait postgres nats minio
docker stop peloton
docker rm peloton
```

Repeat the `docker run` command above with the same environment file. Peloton applies pending migrations on startup with `CAIRN_DATABASE_AUTO_MIGRATE=true`. The Compose volumes hold the database and activity files. The removal of the legacy social and notification features uses the existing schema and preserves historical rows. Account verification and password-reset email still use the configured mail adapter.

### Rebuild cycling records

After deploying the updated server and applying migration 84 (downhill variants) and migration 85 (catalog index), run `docker exec peloton /usr/local/bin/cairn records recompute` with the existing deployment configuration. Existing records must be recomputed to pick up the updated downhill selection and the cumulative-ascent calculation. The command rebuilds historical records for every rider, validates cycling distance and speed, and rematches cycling segments with stored GPS. It can be rerun and preserves original files, activities, and existing raw streams. When stored samples are missing, it can recover them from archived FIT, GPX, or TCX files that are still accessible to the instance. Activities without the necessary measurements may have fewer record categories.

### Backfill activity moving time

After deploying the updated server, run the timing backfill with the deployment's existing database and archive configuration. This command can be rerun.

```sh
docker exec peloton /usr/local/bin/cairn activities backfill-moving-time
```

It uses stored GPS tracks and available FIT or GPX archives to fill in moving time for earlier imports. It refreshes activity average speed, time-based training stress estimates, and gear totals. It preserves elapsed time, source data, imported training stress, manual timing edits, personal records, and segment efforts.

Garmin FIT imports use complete timer start and stop events or the device's timer total. GPS can exclude stops while the timer is running when the track has adequate coverage. GPX imports estimate motion from coherent GPS displacement over windows of about eight seconds. Separate thresholds for starting and continuing movement help distinguish stops from slow climbing. The estimator also detects sustained near-stationary periods, rejects implausible jumps, and omits gaps above the recording's cadence-based limit of 5–15 seconds. At least 70% of the reference duration must be observed. With less coverage, Peloton retains the available source or previously stored time. Very slow movement, short stops, and sustained GPS drift can still be ambiguous.

**Exclude downhills** reads a separate set of verified cycling speed and distance efforts. Peloton smooths altitude over 50 metres and requires continuous GPS and elevation through each eligible effort. An effort is excluded if it drops at least 3 metres at an average grade of at least 1.5%, or if 20% or more of the effort is on a sustained descent of at least 3%. The standard records remain available. Route comparisons use versioned GPS streams and refresh when an import or recovery changes the underlying recording.

Climbing records report the most elevation gained within each continuous time window. Descents contribute zero gain, small altitude fluctuations are ignored, and recording gaps break the window.

## Attribution and license

Peloton is a fork of [Cairn](https://github.com/opencairn-org/cairn-core).

Copyright 2025-2026 The Cairn Authors. See [NOTICE](NOTICE) and [AUTHORS](AUTHORS) for attribution and third-party notices.

Peloton is distributed under the [GNU Affero General Public License v3.0](LICENSE). The API contract is distributed under the [Apache License 2.0](api/proto/LICENSE).
