# Geographic Strava segment discovery

On 21 November 2026 an anonymous public map returned existing Strava IDs and encoded polylines when given a geographic viewport. No segment names or IDs were supplied to the map. The original [Flandrien Challenge experiment](README.md) still runs from its three hand-selected entries. The new experiment replays the map response through the same local matcher. Full production discovery remains open.

## Sint-Niklaas coverage

Two ordinary signed-out browser sessions at zoom 12 returned the same 1,000 unique IDs with identical polylines. That counts the response, which includes a wider area than the visible screen. The site's documentation describes a cap of 1,000. Zooming in changed the set of segments inside a fixed central rectangle. It recovered four central IDs missed at zoom 12 and omitted two seen at zoom 12. A zoomed-out result is an incomplete sample, even for a rectangle well inside it. [Provider documentation](https://tailwindkompass.com/docs)

| Public viewport | Map response rows | Starts in central rectangle | Both endpoints within 100 m of the public route | Every vertex within 100 m |
| --- | ---: | ---: | ---: | ---: |
| [Sint-Niklaas at zoom 12](https://tailwindkompass.com/explore?lat=51.1645&lon=4.1397&zoom=12) | 1,000 | 51 | 30 | 28 |
| [Same centre at zoom 14](https://tailwindkompass.com/explore?lat=51.1645&lon=4.1397&zoom=14) | 78 | 53 | 8 | 7 |
| [Public route centre at zoom 12](https://tailwindkompass.com/explore?lat=51.20&lon=4.07&zoom=12) | 707 | 57 | 37 | 33 |

The rectangle is 51.145–51.185° N, 4.105–4.175° E. The route is the published Creative Minds cycling loop. The third viewport uses the rounded midpoint of the route bounds. Distances were measured from segment vertices to the nearest route edge. These proximity counts show candidate coverage. The planned GPX had no original timestamps, so it was used only for geography. Strava confirmed four IDs from the central results. One of them, NaastSpoorTotUilenstraat, appeared in the tighter viewport. [Public route](https://www.routen.be/creative-minds-fietsroute) · [Noordlaan · 15246798](https://www.strava.com/segments/15246798/embed) · [spoorweg St-Niklaas - Nieuwkerke · 1484238](https://www.strava.com/segments/1484238/embed) · [NaastSpoorTotUilenstraat · 19141960](https://www.strava.com/segments/19141960/embed)

The provider's anonymous `find_tailwind_segments` MCP tool returned 2–5 IDs per query across five locations around Sint-Niklaas, five each for Oudenaarde and Utrecht, and zero for a North Sea control. The Sint-Niklaas centre query included results from Temse and the Scheldt. The tool takes a point or place. Its lookup and detail responses lacked polylines in the tests. The public REST API documents wind and bearing calculations and supplies no segment feed. [Developer documentation](https://tailwindkompass.com/developers)

## Recorded ride verification

One opt-in Bike Data Project GPX covering the Brussels area contained 1,041 original timestamped samples. It was imported into a loopback Peloton development instance, downloaded again, and checked byte for byte against the source. A broad public viewport over that region supplied 1,000 endpoint-checked geometries. Peloton's prototype filtered those down to 248 bounding-box candidates and found three forward traversals. Every elapsed time agreed with the difference between the corresponding original GPX timestamps. The public Strava pages classified all three as cycling segments. [Bike Data Project open data](https://www.bikedataproject.org/)

| Strava confirmation | Geometry vertices | Elapsed seconds |
| --- | ---: | ---: |
| [Buizingen-Lot - Kanaal · 40628454](https://www.strava.com/segments/40628454/embed) | 27 | 271 |
| [Canal Ruisbroek · 35700572](https://www.strava.com/segments/35700572/embed) | 20 | 201 |
| [Lot Dorent · 29325646](https://www.strava.com/segments/29325646/embed) | 50 | 367 |

The downloaded fixture's exact points, absolute timestamps and account details are excluded from Git. A recorded ride through Sint-Niklaas remains unverified. The public Ride with GPS ride page identified a local recorded ride, but the unauthenticated GPX download returned HTTP 401. The province and Creative Minds GPXs were planned routes without point timestamps.

## Source and maintenance assessment

| Source | Access and coverage | Maintenance implications |
| --- | --- | --- |
| Tailwind KOMpass public map | The signed-out page issues an internal `get_segments_in_bbox` request and gets real Strava IDs, encoded polylines and declared endpoints. The provider says its catalog is populated from rides of its own connected athletes. Peloton did not connect an account or sync an activity. Coverage varies by area and has no known denominator. | High for unattended use. The request and response are an internal browser interface with no public contract. The external REST interface explicitly excludes segment data. A supported data arrangement, viewport subdivision, ID deduplication and fresh geographic checks would be needed before depending on it. [Public map](https://tailwindkompass.com/explore) · [API limits and data scope](https://tailwindkompass.com/developers) |
| Tailwind KOMpass anonymous MCP | Keyless point/place lookup from the same provider. It prioritizes a small set of popular segments and returned no geometry. | Lower protocol maintenance. A second provider would be needed for geometry, and repeated point probes cannot establish complete coverage. [Public MCP tools](https://tailwindkompass.com/developers) |
| Strava official API v3 | The anonymous Explore request for the central Sint-Niklaas rectangle returned HTTP 401. Strava documents a ten-result cap and Extended Access Tier permission. | The strongest documented contract if access is available. OAuth, tiling and request limits remain ongoing operational costs. [Strava API reference](https://developers.strava.com/docs/reference/#api-Segments-exploreSegments) |
| Strava's own web map, segment pages and embeds | A signed-out Chromium session opening `/maps/segments` reached the map with a login prompt for segments. It downloaded basemap tiles. The earlier anonymous segment detail request returned 403. Public embeds resolve known IDs and show truncated display distance. | A signed-in session and segment-layer visibility remain untested. A browser-based dependency would need regression checks for session expiry, registration prompts and map changes. [Strava map help](https://support.strava.com/en-us/articles/15402007-how-do-i-find-segments-on-the-strava-website) |
| VeloViewer public pages | Returned distance and geometry for known IDs in the first experiment. No geographic enumeration interface was verified. | Useful for spot checks and potential geometry enrichment. Page scraping or another undocumented contract would need maintenance. [Example public segment page](https://veloviewer.com/segments/19452499) |
| Doogal Segment Explorer | Publisher describes a geographic map and export. Anonymous HTTP requests here returned 403, and the ordinary browser showed a verification page. No Belgian coverage was measured. | Its availability and data contract remain unverified. [Doogal Segment Explorer](https://www.doogal.co.uk/SegmentExplorer) |
| Strava Metro and academic snapshots | Metro's export uses OSM road edge IDs. The 2018 cycling study covers several Belgian cities, but its raw Strava segments are only available on request. | Neither supplies a verified current feed of Strava segment IDs and directed geometry near arbitrary routes. [Strava Metro export](https://stravametro.zendesk.com/hc/en-us/articles/360051202734-Edge-Counts-Data-Export-and-Download) · [Academic data availability](https://doi.org/10.1140/epjds/s13688-023-00385-7) |

A production route lookup would need overlapping windows along the route, tighter queries at saturation, deduplication by Strava ID, and explicit handling of clipped, expired, deleted and missing definitions. Publishing or caching the segment data at scale raises data rights and retention questions. Sending precise route-derived search windows to a remote service can also disclose where someone rode. For this experiment the map saw general area viewports. Activity files and timing remained local.

## Reproduce from the public map

1. In a signed-out desktop browser, open a [Sint-Niklaas viewport](https://tailwindkompass.com/explore?lat=51.1645&lon=4.1397&zoom=12) or the [Brussels-area viewport](https://tailwindkompass.com/explore?lat=50.82&lon=4.30&zoom=11.5). Use the ordinary map to pan and zoom to another public area.
2. Open the browser's Network tab, reload the page, and inspect the successful `get_segments_in_bbox` response. Save the response JSON array under `tmp/strava-segments/`. Keep only `id`, `name`, `distance`, `polyline`, `start_latitude`, `start_longitude`, `end_latitude` and `end_longitude`. The local reader ignores other fields. Save the body only. Request headers and cookies are unnecessary.
3. Download the archived FIT or GPX from a local Peloton activity that crosses the area. Importing an opt-in original from the [Bike Data Project](https://www.bikedataproject.org/) also works. The fixture in this test came from the project's openly shared GPX download. Use a viewport that covers that file's general area.
4. Run the local replay and inspect the candidates with efforts. The format distinguishes a valid empty response from a broken capture. A usable geometry must decode at precision 5 and have both declared endpoints within 10 m of the respective decoded endpoints. The existing matcher checks direction and complete geometry before timing.

```bash
go test -race ./experiments/strava-segments
go run ./experiments/strava-segments \
  -activity tmp/strava-segments/archived.gpx \
  -viewport tmp/strava-segments/viewport.json
```

For the geographic-only benchmark, download the [Creative Minds GPX](https://www.routen.be/route/3906/download/gpx). Decode each returned precision-5 polyline, count starts inside the rectangle above, and count a segment as close to the route if both endpoints are within 100 m. Repeat for each vertex to reproduce the stricter count. The command's existing catalog path uses `-refresh` and runs independently. [First experiment](README.md) · [Sanitized measurements](dynamic_evidence.json)
