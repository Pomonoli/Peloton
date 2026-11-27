# Ride analytics

The activity map uses the primary GPS stream for its geometry. Speed uses recorded speed where it agrees with the distance and timestamps, with distance-derived speed as a fallback. Short median windows reduce GPS and altitude noise. Gradient uses changes in altitude over a continuous distance window. Grey sections indicate missing measurements. The Power layer shows measured power.

Replay requests the original sample resolution. The marker waits at the last recorded position during a recording gap, and the route leaves long gaps and implausible GPS jumps unconnected. On supported devices, MapLibre drapes the route over Mapzen's public terrain model at its original vertical scale. The model represents bare ground and may differ from the recorded altitude, particularly at bridges and in areas with limited terrain data. The browser requests elevation tiles for the map area it displays. Deployments can set `VITE_TERRAIN_TILES_URL` at web build time to use a different 256-pixel Terrarium tile source with `{z}/{x}/{y}` placeholders and zoom levels up to 15. If the elevation model fails, replay uses the flat map. When the basemap or WebGL is also unavailable, replay shows the route and marker in a 2D preview. [Mapzen Terrain Tiles attribution](https://github.com/tilezen/joerd/blob/master/docs/attribution.md)

## Estimated Power

Eligible ride owners can run Estimated Power from the activity page. Peloton prefills the rider weight as of the ride when it is available in their health metrics or athlete profile. The owner can override it and enter the weight of the bike and equipment. Equipment weight, rolling resistance and drag area are remembered for that account in the current browser. Wind is an optional constant headwind assumption for the current calculation. A negative value represents a tailwind.

The model multiplies ground speed by the sum of gravitational, rolling, aerodynamic and acceleration forces, then divides by an assumed drivetrain efficiency of 97%. Defaults are a rolling resistance coefficient of 0.005 for paved roads, a drag area of 0.35 m² for a road-bike position, and an air density of 1.225 kg/m³. A blank wind input makes air speed equal to ground speed. The calculation uses a smoothed speed change for acceleration when it falls within a plausible range. It reports zero watts when the model predicts coasting.

The estimate requires at least one minute of usable samples covering 60% of elapsed time. The average is weighted by the duration of those samples, with the coverage shown next to the result. Braking, wind changes, riding position, and errors in GPS speed or altitude can substantially affect the estimate. The calculation stays in the activity view. Measured power, best efforts, leaderboards, and training load continue to use their existing data sources.

## Segment comparisons

The Local KOM table ranks each member by their fastest eligible effort during the selected period. The average speed uses total matched distance divided by total elapsed time for eligible efforts in that period. The chart uses the recorded attempt times and can compare up to four members, with up to 200 recent efforts per member.

## Territory heatmap

Open **Explore → Heatmap** and select **Territory** in the layer menu. Every resolution 8 H3 cell is awarded to the rider with the most validated GPS distance within its boundaries. Cells have an average area of about 0.74 km², and the legend sums their actual areas. An exact distance tie goes to the lower rider ID. Select a claimed territory to see the ranking and distance for the underlying cell.

Territory includes shared cycling activities that started in the last 90 days. Deleted, moderated, and provider-restricted activities are excluded. A rider's contributions are hidden in cells intersecting their geographic privacy zones. Each ride contributes through its primary recording, so duplicate sources attached to the same ride count once.

The layer prepares existing rides in batches the first time it is opened. New imports and revised streams are picked up automatically, and rides age out as the window advances. The open map refreshes every 30 seconds and when the browser tab becomes visible.

## Speed vs Distance

In Insights → Stats, each point is one cycling activity. The chart opens with the rider whose Stats page you are viewing. Use Compare riders to overlay other members' eligible activities. The Stats year selector and activity filters in the URL also apply to the chart. Distances and average moving speeds use the validated activity summaries and are displayed in km and km/h. Each rider's dashed line is a linear fit across their displayed activities when they have at least two distinct distances. Select a point to see its ride and open the activity.

## Shared activities

In Social → Head-to-Head → Shared, choose two riders to compare their outdoor GPS recordings. Shared Rides requires at least five minutes of actual moving proximity within about 50 m, with both riders heading within 60° of one another. Brief interruptions can connect matching sections. Shared Distance measures the average of the two distances during those sections, with breaks, stops, and time spent apart omitted. Rejoining during the same ride adds distance without creating a second shared ride.

Crossed Paths shows brief GPS-detected encounters within about 30 m between riders who were both moving on separate rides. It can include riders moving in opposite directions. An encounter is a geographical inference from recordings. Select any of the three totals for details, activity links, and encounter maps. The date filter uses the beginning of each shared ride and the time of each encounter, with both calendar dates included in UTC.

Shared applies member activity visibility and geographic privacy zones before publishing results. It compares overlapping ride windows and interpolates inside validated recording intervals. Existing rides are evaluated from their stored GPS on first use and their results, including empty matches, are cached. A recording replacement, a different primary source, or a privacy zone edit invalidates the corresponding cache entry. Deleted or newly restricted activities are filtered out when the comparison is read.
