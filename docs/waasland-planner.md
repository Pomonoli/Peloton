# Waasland route planner

Open `/routes/waasland` from the **Waasland planner** navigation link. Planning and GPX export are public; member profiles and activities retain their existing authentication. This feature creates planned tracks, not completed activities.

The planner supports climbs, cobbles, gravel and uninterrupted riding, distances from 15 to 200 km, four regional departure points, up to three climb repetitions, six climb-zone preferences, clockwise/counterclockwise routing, named places, alternatives and shareable settings. The 200 km flow preset uses the Scheldt–Ghent–Terneuzen canal circuit. Open-Meteo supplies current wind; wind failure does not block planning. Requested places and direction can require an external BRouter cycling connection. Ferries remain subject to actual operating times.

## Data and deployment

No additional package, secret, migration or account is required. Rebuild the normal Docker image after merging. The existing application serves the planner and `/api/route-planner/waasland/data/{file}`.

The route engine is maintained locally in `web/static/waas-engine` and is lazily bundled by Vite with content-hashed URLs, including its height calculator and GPX writer. The planner and recorded-route editor load the engine from the same application build; fixed `/waas-engine/*.js` URLs are not runtime dependencies. Large public regional datasets are loaded lazily from the existing Waasland host (`https://waasland-klimlussen.seppevds.chatgpt.site`) through a read-only server adapter. Only allowlisted filenames can be fetched. The adapter never forwards member cookies, authorizations or activity data, restricts redirects to the same origin and limits response size and time. JavaScript-form datasets are parsed as JSON assignments; remote scripts are never executed in Peloton.

For independent hosting, mirror the existing dataset files and change the fixed adapter origin in `cmd/server/wire.go`. Alternatively set `VITE_WAAS_DATA_URL` at frontend build time to a same-origin asset directory or a trusted CORS-enabled data host. Keep the original filenames. Catalog failure produces an actionable error or a partial-catalog notice. Map tiles, current weather and custom routing still require their respective external services.

## Metrics and limitations

Distance is recalculated along the complete exported coordinates. Repeated passages remain in the GPX track and count towards distance and ascent. Regional elevation uses the existing prepared height data, distance-based smoothing and a noise threshold; BRouter connections use router elevations. The UI and GPX share the same geometry. Ascent remains an estimate and can differ from Garmin or Komoot due to elevation source and filtering. A requested ascent is an optimization objective, never an invented result. Distance misses and incomplete data are reported explicitly. Surface and access classifications depend on map coverage; unknown surfaces remain visible in the summary.

Shared links reproduce planner settings. They do not publish private member data or promise an immutable alternative when datasets change.

## Integration validation

The frontend unit suite, Svelte checks, frontend lint and production static build pass. The Go unit suite passes with `go test -short ./...`, including allowlist, public proxy, redirect, size and response-body lifecycle tests. Browser QA exercised all four profiles, alternatives, current wind and GPX download using a local mirror of the public catalogs (the managed preview does not share the Go process's loopback network). Examples were 69.96 km/402 estimated m for climbs, 70.19 km/16.68 mapped km for cobbles, 70.35 km/14.96 mapped km for gravel, and 199.88 km for the Scheldt–canal circuit. These are examples, not fixed route guarantees.

The QA browser lacks WebGL2, so the existing canvas route fallback was exercised; interactive map layers still need checking on a WebGL2-capable device. The full `make check` cannot run in this workspace because the pinned Go protobuf generators are not installed. Database integration, coverage gates and release audits remain for the repository CI.
