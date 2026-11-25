# Peloton project plan

Peloton serves a small, invite-only cycling group through a responsive web app and a self-hosted Go server. Docker Compose supplies PostgreSQL with TimescaleDB and PostGIS, NATS JetStream, and S3-compatible storage. [Run and update Peloton](README.md)

## Supported features

- **Rides and imports.** Independent FIT, GPX and TCX uploads, Garmin Connect Personal API history and recurring sync, and optional Strava cycling sync through the enrolled worker. Import feeds converge on the same activity matcher. [Garmin Connect](docs/garmin-connect.md) · [Strava Connect](docs/strava-connect.md)
- **Cycling analysis.** Activity streams, timing and speed, training analytics, personal and shared records, heatmaps, segment detection, and Local KOM rankings. [Ride analytics](docs/ride-analytics.md)
- **Member navigation.** Social contains Riders and Head-to-Head. Profiles link to a member's overview, activities, stats, records, analysis, heatmap, and segments. Authentication, geographic privacy zones, and restrictions apply to the available data. [Visibility model](docs/visibility-model.md)
- **Route planning.** The Waasland planner provides regional routing and GPX export. [Planner](docs/waasland-planner.md)

## Validation priorities

- Run Go unit and PostgreSQL integration tests after changes to permissions, import, and matching. Run the Riders, Head-to-Head, heatmap, and segment-detail browser suites after changes to member navigation or the views it links to. Use a disposable environment and fixture provider accounts. [Local development](docs/run-local.md)
- Keep upgrades reproducible. Preserve existing migrations, account and activity identities, source archives, and encrypted integration credentials. Verify backup and restore on a test deployment. [Operations](docs/production-readiness.md)
- Keep provider-specific assumptions documented. Garmin and Strava apply their own access and availability rules. Segment discovery depends on geographic coverage and the availability of Tailwind KOMpass. [Segment discovery evidence](experiments/strava-segments/DYNAMIC_DISCOVERY.md)

Peloton's supported client is the responsive web app. A native APK, ride recording, and live GPS tracking are outside the current plan.
