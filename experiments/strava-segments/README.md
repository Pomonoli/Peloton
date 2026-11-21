# Strava segment discovery proof of concept

The command looks up real Strava cycling segments in a three-entry public catalog sample and matches them against an archived Peloton FIT or GPX. The ride file stays local. The command runs independently of the Peloton server and writes only a local catalog cache and the requested JSON report. It uses public cycling metadata and segment geometry as input. Effort times come from the activity's sample timestamps.

The [geographic discovery follow-up](DYNAMIC_DISCOVERY.md) uses a separate viewport response and a recorded ride.

## Reproduce

From the repository root, use the Go toolchain specified in `go.mod`. Run `go test -race ./experiments/strava-segments` to verify parsing, cache round-tripping and expiry, direction, geometry coverage, GPS gaps, and timestamp handling.

For an already imported ride, open the activity's source management page and download the archived FIT or GPX. For a reproducible positive example, download the public [Tour of Flanders Blue Loop GPX](https://www.cyclinginflanders.cc/routes/ronde-van-vlaanderen-blue-loop) and create a timed fixture from it:

```bash
mkdir -p tmp/strava-segments
curl -fsSL 'https://www.cyclinginflanders.cc/sites/cyclinginflanders/files/public/media/files/2026-09/RVV%20Blue%20%282%29.gpx' \
  -o tmp/strava-segments/blue-loop.gpx
python3 experiments/strava-segments/demo_gpx.py \
  tmp/strava-segments/blue-loop.gpx tmp/strava-segments/forward.gpx
python3 experiments/strava-segments/demo_gpx.py --reverse \
  tmp/strava-segments/blue-loop.gpx tmp/strava-segments/reverse.gpx
```

The planned route has no timestamps. The fixture uses the planned route's coordinates interpolated at intervals of at most 8 m and timestamps corresponding to 5 m/s. You can import the forward fixture into a local Peloton instance, then download its archived file and use that file in the following commands.

```bash
go run ./experiments/strava-segments -activity tmp/strava-segments/forward.gpx -refresh
go run ./experiments/strava-segments -activity tmp/strava-segments/forward.gpx
go run ./experiments/strava-segments -activity tmp/strava-segments/reverse.gpx
```

The first invocation makes up to six sequential HTTPS requests. The next two read the local cache and make zero requests. The default cache is `tmp/strava-segments/catalog.json`. Use `-cache` to choose a different file. The cache contains only the Strava ID, displayed name and distance, start-to-finish geometry, source URL and retrieval date. The command rejects expired entries after seven days. A failed refresh reports the status and any `Retry-After` value and leaves the previous file available for offline use within its lifetime.

## Verified on 21 November 2026

The local Peloton instance imported the 10,958-point GPX and returned a byte-identical archived file. The live catalog and offline archive runs returned identical efforts. Both sample timestamps and their differences were independently checked against the archived GPX. The segments below were verified against Strava. The elapsed times belong to the simulated activity. See [full results and controls](evidence.json).

| Verified Strava segment | Strava embed distance | Published line length | Vertices | Forward elapsed |
| --- | ---: | ---: | ---: | ---: |
| [Koppenberg · 19452499](https://www.strava.com/segments/19452499/embed) | 0.5 km | 557 m | 7 | 111.932 s |
| [Oude Kwaremont (kasseien) · 1092383](https://www.strava.com/segments/1092383/embed) | 1.4 km | 1,486 m | 36 | 297.326 s |

Both remained geographic candidates on the reversed route and produced zero efforts. Kemmelberg (Belvedère), ID `20920752`, was fetched and excluded by the route bounds. The existing 31-point development FIT was parsed successfully and returned zero candidates outside the sample region. A recorded ride covering these segments remains to be verified. [Cycling in Flanders catalog](https://www.cyclinginflanders.cc/flandrien-challenge)

## Discovery methods

| Method | Observation |
| --- | --- |
| Official Strava API v3 | Anonymous segment lookup and geographic Explore returned HTTP 401. Explore documents a ten-result ceiling and requires an Extended Access Tier app with granted permission. [Strava API reference](https://developers.strava.com/docs/reference/#api-Segments-exploreSegments) |
| Strava's website and undocumented web request | `/segments/explore` redirected to `/maps/segments`, whose anonymous HTML contained the map shell. Its public JavaScript referenced the undocumented `POST /api/next/data/segments/get-segments` details request. A single anonymous request with an empty leaderboard selection returned HTTP 403. `/segments/search` and the older `/segments/608879/streams?stream_types[]=latlng` returned HTTP 404. Signed-in map behavior is unverified. |
| Public Strava segment pages and embeds | The three catalog IDs were verified anonymously. Public segment pages provided ID, name and cycling type. Their public embeds also provided distance at 0.1 km display resolution. These surfaces need a known ID and supplied no usable line geometry in the anonymous responses tested. [Example embed](https://www.strava.com/segments/19452499/embed) |
| Cycling in Flanders public catalog | Detail pages publish a Strava link and a simplified start-to-finish line in the map settings. The challenge also offers a ZIP with 59 segment GPXs. Those exports can represent different extents than current linked pages, so the prototype uses geometry from the same page as its current Strava link. For example, the Oude Kwaremont tourism headline says 2.11 km, but the linked ID covers about 1.49 km. A separate Paterberg page probe returned HTTP 429. The provider's limit and sustained throughput are unknown. [Koppenberg](https://www.cyclinginflanders.cc/koppenberg) · [Oude Kwaremont](https://www.cyclinginflanders.cc/oude-kwaremont) |
| VeloViewer public segment pages | Known IDs exposed distance and encoded geometry without sign-in. For the two matches, distances were 557.9 m and 1,488.96 m. Their endpoints were within 1.4 m of the lines used above, with 10 and 268 vertices respectively. This cross-check showed denser geometry can be retrieved for known IDs. The prototype does not depend on VeloViewer. [Koppenberg](https://veloviewer.com/segments/19452499) · [Oude Kwaremont](https://veloviewer.com/segments/1092383) |

The tested catalog is regional and curated. Automatic route matching is limited to that catalog's coverage. Public page schemas and linked IDs can change. A production design needs a durable source with known coverage and semantics for renamed, merged, deleted or private segments.

## Data and account risks

Strava's published API rules restrict automated extraction, persistent indexes and retention, and describe suspension for violations. Public third-party access alone does not establish redistribution rights. Account-based approaches also expose sessions and can lead to account restrictions. Sending precise discovery windows to a remote provider could reveal private ride locations. This sample fetches the same public URLs for all activities and keeps ride data out of those requests. [Strava API policy](https://www.strava.com/legal/api_policy) · [Strava terms](https://www.strava.com/legal/terms)

## Integrating with Cairn

The prototype reuses `fileimport.Parse`, the raw stream converter, the domain bounding-box utilities and `domain.MatchSegment`. Cairn's default 15 m corridor missed both Blue Loop matches on the sparse catalog geometry. The experiment opens a 50 m corridor to locate candidate traversal windows, then checks both complete paths in order within 25 m. It rejects candidate gaps exceeding 90 m or 30 s. It chooses the closest original recorded sample within each endpoint encounter and subtracts the two timestamps. This means timing resolution depends on the file's sampling. Sparse or noisy GPS, pauses longer than the gap threshold, and nearby roads within tolerance can affect results. This experiment has no route-wide recall or timing accuracy benchmark.

For a later integration, put discovery behind a driven port and run it in an optional post-import job. Store provider ID, source, geometry precision and refresh state separately from activity efforts. PostGIS already builds and indexes segment geometry from encoded polylines, and the segment use case already loads raw GPS, queries visible spatial candidates and replaces efforts transactionally. Cairn's current external segments require a linked account and private scope, so shared catalog entries need an explicit provenance and visibility model. Any Local KOM path should accept only locally timed Peloton activities, apply activity and segment visibility, and deduplicate different sources for the same activity before ranking. The experiment changes none of the application wiring. [Segment domain](../../internal/domain/segment.go) · [PostGIS repository](../../internal/adapter/secondary/postgres/segment_repo.go) · [Existing match use case](../../internal/usecase/segment/match.go)
