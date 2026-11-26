# Recorded routes

Open **Explore → Routes** to discover routes from the group's recorded cycling activities. Peloton creates one catalog entry after the same rider completes two sufficiently similar rides. Matching recordings from any rider count, including rides imported before the route was discovered. The featured rider has the most completions. Ties go to the rider with the earliest matching ride, then to the lowest rider ID if the rides started at the same time.

The shorter GPS track must be at least 80% as long as the longer one, and at least 80% of each recording must follow the same corridor within 35 metres. Reversed rides, different starting points, minor detours, and shorter or longer variants can match. Rides can also match across route types.

Every ride in a group must match the same representative recording. Peloton chooses the ride with the highest average overlap among recordings that match every member. Ties go to the earliest ride, then the lowest activity ID. Peloton reevaluates the representative when the group changes and merges nearby groups when a recording from either group can represent every ride. The representative supplies the route type, distance, and elevation profile. The route name uses that ride's reverse-geocoded start place when available and visible. Otherwise it uses the route type and distance.

Hidden, provider-restricted, and deleted activities are excluded from the shared catalog. Members see the representative geometry with its owner's privacy zones masked, including in GPX and the Route Planner. GPS gaps and masked sections are kept as separate segments. The Route Planner can reverse a recording, shorten it, or change the start of a continuous loop before exporting.

## Upgrade and maintenance

Migration **91** schedules reclustering of historical rides through the existing processing queue. Rebuild and restart the Peloton Docker application using the existing database and settings. With `CAIRN_DATABASE_AUTO_MIGRATE=true`, startup applies the migration. Otherwise run `cairn migrate up` with the new image and the normal instance environment before restarting. The worker uses stored recordings and reads the existing activity streams when needed.

The server rebuilds memberships and refreshes catalog statistics in the background after deployment. It resumes queued work after a restart and prioritizes new, corrected, or deleted rides over the historical backlog. When groups merge, Peloton keeps the oldest route ID and removes the duplicate entries. A route drops out of the catalog if no rider still has two qualifying rides. A group with remaining matches can reuse its ID if it qualifies again. Empty groups are removed.

The detail map uses Peloton's configured interactive basemap. The small route previews use OpenStreetMap tiles with a track-only fallback.
