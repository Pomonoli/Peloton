# Garmin Connect

Peloton uses Garmin’s official read-only Personal API. Each rider creates and connects a key for their own Garmin account. The connector runs in the Peloton server and shares the existing activity pipeline. [Garmin Personal API](https://developer.garmin.com/gc-developer-program/personal/personal-api/) · [Garmin’s reference client](https://github.com/garmin/connect-personal-api-python)

## Connect locally

1. Start the local dependencies with `make dev-up`. Use your existing `dev.env` and keep its master encryption key. In a terminal at the repository root, run:

   ```sh
   set -a
   . ./dev.env
   set +a
   make migrate-up
   make dev-server
   ```

   In a second terminal run `make dev-web`. The web app is at `http://localhost:5173`. Start or restart the server with `CAIRN_SEGMENT_DISCOVERY_ENABLED=true` in your local configuration and NATS running if you want automatic Strava segment discovery.
2. Sign in to your own [Garmin Connect account](https://connect.garmin.com/app/settings/personalApiToken), choose **Create Key**, and pick an expiration period. Garmin displays the key once.
3. Sign in to Peloton. Open **Settings → Connections → Garmin Connect**, paste the key, and choose **Connect Garmin**. The cycling history import starts automatically. The connection shows the number of historical rides processed, rides without recorded samples, and the last completed sync.
4. Open a historical ride and compare its map and sensor charts with Garmin. Then sync a new ride to Garmin and choose **Sync Now**, or leave the server running until the next scheduled check. The default polling interval is one hour. Repeat **Sync Now** to verify that the activity list still contains one entry for the ride. A Garmin ride that matches an existing manual upload is grouped with it by Peloton’s existing activity matcher.
5. Use **Replace key** before a key expires or **Reconnect** afterward. Save a key from the same Garmin account to continue the existing import. **Disconnect** removes the stored Garmin session and stops further imports. Imported rides stay in Peloton. Revoke the key in Garmin Connect if you want to invalidate it there as well.

## Import behavior

- The history scan pages through the cycling activities Garmin makes available. It checkpoints after each ride and resumes after a restart or temporary outage. Subsequent polls overlap the previous successful scan by 14 days. Activities uploaded to Garmin with an older start date can be picked up by disconnecting and starting a fresh history scan, or by uploading an original file manually.
- Peloton prefers the original FIT and also handles original TCX or GPX files. When Garmin supplies no usable file, Peloton imports the available ride summary. Summary-only rides lack the recorded track and sensor samples needed for route matching and stream-based records. Object storage must be enabled to retain original files for download.
- The Garmin connector uses the instance scheduler cadence, `CAIRN_SCHEDULER_POLLING_INTERVAL`. Setting `CAIRN_SCHEDULER_ENABLED=false` turns off recurring polls. The history import, retries, and **Sync Now** still run.
- Garmin keys and bearer sessions are encrypted at rest with the instance master key. Keep each key private. Protect access to the Peloton instance and use HTTPS when connecting from another device. Existing Cairn Garmin worker connections can still be managed through the external worker controls.
- Strava segment discovery uses the existing catalog, cache, and visibility rules. Discovery requires NATS and the feature flag. A visible ride appears on a Local KOM leaderboard only when its recorded route matches a segment and its visibility allows the viewer to see the effort.

## Automated verification

The Garmin HTTP client and sync tests use fixtures. The server integration test runs its own database inside a disposable TimescaleDB/PostGIS instance, uses an embedded NATS server, and substitutes fixture Garmin activities and segment definitions. It exercises real FIT parsing, source storage, account isolation, manual upload grouping, and the segment discovery consumer.

```sh
make test-db-up
CAIRN_TEST_DATABASE_URL='postgres://cairn:cairn@127.0.0.1:55432/cairn?sslmode=disable' \
  go test -tags integration ./cmd/server -run '^TestAPI_GarminConnectHistoryAndSegmentDiscovery$'
make test-db-down
```

A live Garmin import requires your own Personal API key and remains a separate check.
