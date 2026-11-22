# Optional Strava activity sync

Peloton uses Cairn’s Strava worker and Go provider SDK over the existing NATS control plane. The Compose image builds worker v0.7.2 from commit `aee3c7e6da8f8a702d1b10cf16fdc2b81f9bd729` with the checked-in cycling patch. The worker uses the Cairn 0.7 protocol and cycling parser version 1.2. The patch filters cycling at listing and fetch time, fetches streams on incremental imports, and skips separate photo, gear, and Strava segment imports. The core also filters worker results. [Upstream worker v0.7.2](https://github.com/opencairn-org/cairn-provider-strava/tree/v0.7.2)

## Strava app access

- Strava currently requires a subscription on the account that creates an API app. The app starts in Single Player Mode and can authorize only its creator. The API dashboard can raise capacity to 10 athletes. Strava review is required above 10. Check the actual capacity and quotas displayed in the dashboard before inviting other riders. [Getting started](https://developers.strava.com/docs/getting-started/) · [Athlete capacity and rate limits](https://developers.strava.com/docs/rate-limits/)
- Set the app’s **Authorization Callback Domain** to the hostname members use, for example `peloton.example.net`. Peloton constructs the callback `https://peloton.example.net/auth/oauth/strava/callback` from `CAIRN_HTTP_PUBLIC_BASE_URL`. For a local smoke test use `localhost` as the callback domain and sign in through the same local origin advertised by Peloton. See the dashboard for the client ID and client secret. The worker requests `activity:read_all` so it can see cycling activities marked **Only You**. [OAuth authentication](https://developers.strava.com/docs/authentication/)
- Each Peloton user authorizes their own Strava account. The connection form stores that user’s app credentials. If several users use the same API app, its owner should provision those credentials privately. Store them in Peloton through Settings and keep them out of Compose, shell history, Git, and logs.
- Shared API data is restricted to the athlete who provided it. Strava’s policy also limits caching, requires deleted data to stop being displayed, and restricts competing applications. This integration retains imported activities on local disconnect and has no automated Strava deletion or retention expiry workflow. Confirm that your intended use and retention are authorized by Strava before using the integration with live data. Strava can restrict or revoke access. [API Agreement](https://www.strava.com/legal/api) · [API Policy](https://www.strava.com/legal/api_policy)

## Production Compose configuration

1. Update the Peloton core and apply its database migrations. Keep the existing Postgres, NATS JetStream, and S3-compatible storage configuration. The worker has no database credentials, storage credentials, or public port. It needs outbound HTTPS to Strava, a NATS connection, and access to the internal S3 endpoint in the core’s signed URLs.
2. If NATS auth-callout is already set up for existing workers, reuse it. For initial setup, sign in as a Peloton admin and use **Admin → Worker onboarding** to bootstrap the NATS account signing key. The equivalent API is `POST /api/admin/nats/bootstrap-account-key` and returns `public_key`. Mount [docker/nats-auth-callout.conf](../docker/nats-auth-callout.conf) as the NATS server configuration. Supply its two environment variables from the deployment’s environment or secret store.

   ```dotenv
   CAIRN_NATS_AUTH_ISSUER=<public_key from Peloton>
   CAIRN_NATS_PASSWORD=<dedicated core-to-NATS password>
   ```

   Configure the Peloton core with the corresponding settings. Keep the established `CAIRN_AUTH_SESSION_SECRET` and `CAIRN_AUTH_MASTER_ENCRYPTION_KEY`.

   ```dotenv
   CAIRN_HTTP_PUBLIC_BASE_URL=https://peloton.example.net
   CAIRN_NATS_URL=nats://nats:4222
   CAIRN_NATS_USERNAME=cairn-core
   CAIRN_NATS_PASSWORD=<same core-to-NATS password>
   CAIRN_NATS_AUTH_CALLOUT=true
   CAIRN_NATS_WORKER_ACCOUNT=CAIRN
   CAIRN_STORAGE_DRIVER=s3
   CAIRN_STORAGE_ENDPOINT=http://minio:9000
   CAIRN_STORAGE_BUCKET=cairn
   CAIRN_STORAGE_ACCESS_KEY_ID=<existing bucket access key>
   CAIRN_STORAGE_SECRET_ACCESS_KEY=<existing bucket secret>
   ```

   The NATS and S3 URLs above use Compose DNS. Use broker TLS and a `tls://` NATS URL when crossing hosts or untrusted networks. The reverse proxy must route `/auth/oauth/strava/callback` to Peloton and preserve its public HTTPS origin. [Worker authentication and enrollment](architecture.md#45-worker-enrollment--dynamic-nats-credentials)
3. Under **Admin → Worker onboarding**, create an enrollment for provider `strava` and name `strava-fetcher`. The API equivalent is `POST /api/admin/worker-enrollments` with `{"name":"strava-fetcher","provider":"strava","expires_in_hours":168}`. Store the returned token privately. Set these values in a private Compose interpolation file such as `../strava-worker.env`.

   ```dotenv
   PELOTON_STRAVA_NATS_URL=nats://nats:4222
   PELOTON_STRAVA_WORKER_NAME=strava-fetcher
   PELOTON_STRAVA_ENROLLMENT_TOKEN=<token from Peloton>
   ```

   The name must match the enrollment. Prolong or replace the enrollment before expiration so the worker can reconnect. Use the enrollment token as the worker’s NATS credential. Keep the core’s NATS password in the core and broker configuration.
4. Add [docker-compose.strava.yml](../docker-compose.strava.yml) to the existing Peloton Compose project. The base Compose file must define the `cairn` network and make NATS and the internal S3 URL reachable from it. Replace `compose.yml` below with the existing production Compose filename. Start the worker after Peloton has initialized JetStream and the provider registry.

   ```sh
   docker compose --env-file ../strava-worker.env \
     -f compose.yml -f docker-compose.strava.yml \
     --profile strava up -d --build strava-worker
   ```

   The worker appears under **Admin → Worker onboarding** when its manifest and heartbeat arrive. Garmin and file uploads work with the Strava profile disabled. For a local developer stack, use the dev NATS URL and omit the enrollment token only with the unauthenticated loopback broker. If Peloton runs on the host, run the Strava worker on the host too so it can reach the same loopback S3 endpoint.

## Test with a real account

1. Open **Settings → Connections → Add connection → Strava**, enter the API app’s client ID and secret, and select **Connect**. Approve access to all activities. An athlete capacity or consent failure leaves the Peloton account unlinked. Use **Reconnect** after correcting the app settings or restoring authorization.
2. Choose **Full sync**, review the count, and start importing with **Skip existing**. The queue and history show progress. Backfills may span multiple quota windows. The worker uses Strava’s per-app read budget and backs off on rate limits.
3. Compare a ride present in both Garmin and Strava. The owner should see one activity, with both recordings listed under its sources and one contribution to statistics and the heatmap. A second member should see the independent ride and its independent measurements. A Strava-only ride should appear only to its owner.
4. Upload or record a new ride in Strava. Use **Sync now** or wait for the next automatic check. Automatic import and its interval are configurable on the connection. Use full sync again to pick up older activities uploaded outside the incremental lookback.
5. Disconnect from Connections. Existing Peloton activities remain and syncing stops. Reconnecting the same athlete through the same worker reuses the original import identity. To revoke the upstream OAuth grant as well, remove the app in Strava’s connected-app settings.

The shared activity and its statistics use the independent recording when one exists. The owner’s combined stream can fill an absent channel from Strava. Strava-only rides can supply the owner’s personal analytics and match already known segments. Their efforts do not enter shared rankings. Strava account sync does not use Strava’s segment API or send Strava GPS tracks to the public segment discovery provider. Existing activities that contain older Strava imports are initially restricted by migration and are rebuilt from independent sources the next time they are re-merged.

## Automated checks

With an integration database configured, run `go test -tags integration ./cmd/server -run '^TestAPI_Strava(Connection|CrossProvider|Historical)'`. These fixtures cover account isolation, reconnect, repeated imports, both provider orders, a manual FIT upload, distinct routes, and shared data access.

To exercise the real worker against Cairn’s mock Strava API, check out the pinned upstream commit, apply [cycling-only.patch](../deploy/strava/cycling-only.patch), and build `./cmd/worker-strava` and `./cmd/mock-strava` from that checkout. Set `PELOTON_TEST_STRAVA_WORKER` and `PELOTON_TEST_STRAVA_MOCK` to the respective binary paths and run `go test -tags integration ./cmd/server -run '^TestAPI_StravaWorkerRoundTrip$'`. The test starts a local mock API, NATS JetStream, and a loopback blob test double. OAuth with real Strava credentials is a separate check.
