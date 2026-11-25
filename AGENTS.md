# Peloton project instructions

Peloton is an invite-only, self-hosted cycling platform for a small group of friends. The responsive SvelteKit SPA is served by the Go application. See [PROJECT_PLAN.md](PROJECT_PLAN.md) for the current scope and validation priorities.

## Product and data

- Keep **Social → Riders** and **Social → Head-to-Head**. Signed-in members can browse one another's available rides, profiles, and derived cycling data. Keep ownership checks for edits, original sources, and credentials.
- Preserve geographic privacy zones, moderation, and provider restrictions. A sign-in is required for activity media and share links. [Member visibility](docs/visibility-model.md)
- Preserve Garmin Connect Personal API import, optional Strava worker sync, and independent FIT, GPX, and TCX uploads. Matching and duplicate detection are scoped to the account that owns the recordings. [Garmin](docs/garmin-connect.md) · [Strava](docs/strava-connect.md)
- Preserve segment timing, Local KOM, records, training analytics, and heatmaps. The Waasland planner offers public route planning and GPX export. [Planner](docs/waasland-planner.md)

## Development

- Use English in the UI, code, and documentation. Use Paraglide for new user-facing strings and keep locale-sensitive dates and measurements consistent.
- Follow the existing domain, port, use case, and adapter boundaries. Wire infrastructure in `cmd/server`. [Architecture ADR](docs/adr/0002-hexagonal-architecture.md)
- Keep existing migration files and historical data intact. Use forward migrations for schema changes and test against a disposable PostgreSQL instance with TimescaleDB and PostGIS.
- Use the Makefile and existing targeted tests. Keep generated Go protobuf stubs in sync. Follow Cairn's DCO sign-off convention when committing. [Contributing](CONTRIBUTING.md)
- Keep credentials and local environment files out of Git. Preserve `LICENSE`, `NOTICE`, `AUTHORS`, the Apache-2.0 API license, and applicable upstream attribution and source obligations.
