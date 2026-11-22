# Ride analytics

The activity map uses the primary GPS stream for its geometry. Speed uses recorded speed where it agrees with the distance and timestamps, with distance-derived speed as a fallback. Short median windows reduce GPS and altitude noise. Gradient uses changes in altitude over a continuous distance window. Grey sections indicate missing measurements. The Power layer shows measured power.

Replay requests the original sample resolution. The marker waits at the last recorded position during a recording gap, and the route leaves long gaps and implausible GPS jumps unconnected. On supported devices, MapLibre drapes the route over Mapzen's public terrain model at its original vertical scale. The model represents bare ground and may differ from the recorded altitude, particularly at bridges and in areas with limited terrain data. The browser requests elevation tiles for the map area it displays. Deployments can set `VITE_TERRAIN_TILES_URL` at web build time to use a different 256-pixel Terrarium tile source with `{z}/{x}/{y}` placeholders and zoom levels up to 15. If the elevation model fails, replay uses the flat map. When the basemap or WebGL is also unavailable, replay shows the route and marker in a 2D preview. [Mapzen Terrain Tiles attribution](https://github.com/tilezen/joerd/blob/master/docs/attribution.md)

## Estimated Power

Eligible ride owners can run Estimated Power from the activity page. Peloton prefills the rider weight as of the ride when it is available in their health metrics or athlete profile. The owner can override it and enter the weight of the bike and equipment. Equipment weight, rolling resistance and drag area are remembered for that account in the current browser. Wind is an optional constant headwind assumption for the current calculation. A negative value represents a tailwind.

The model multiplies ground speed by the sum of gravitational, rolling, aerodynamic and acceleration forces, then divides by an assumed drivetrain efficiency of 97%. Defaults are a rolling resistance coefficient of 0.005 for paved roads, a drag area of 0.35 m² for a road-bike position, and an air density of 1.225 kg/m³. A blank wind input makes air speed equal to ground speed. The calculation uses a smoothed speed change for acceleration when it falls within a plausible range. It reports zero watts when the model predicts coasting.

The estimate requires at least one minute of usable samples covering 60% of elapsed time. The average is weighted by the duration of those samples, with the coverage shown next to the result. Braking, wind changes, riding position, and errors in GPS speed or altitude can substantially affect the estimate. The calculation stays in the activity view. Measured power, best efforts, leaderboards, and training load continue to use their existing data sources.

## Segment comparisons

The Local KOM table ranks each member by their fastest eligible effort during the selected period. The average speed uses total matched distance divided by total elapsed time for eligible efforts in that period. The chart uses the recorded attempt times and can compare up to four members, with up to 200 recent efforts per member.
