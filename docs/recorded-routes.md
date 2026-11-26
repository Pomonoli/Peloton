# Recorded routes

Open **Explore → Routes** to discover routes from the group's recorded cycling activities. Peloton creates one catalog entry after the same rider completes two sufficiently similar rides. Matching recordings from any rider count, including rides imported before the route was discovered. The featured rider has the most completions. Ties go to the rider with the earliest matching ride, then to the lowest rider ID if the rides started at the same time.

The catalog compares validated GPS coverage in both directions. It checks that at least 85% of each recording follows the same corridor against the route's representative activity. Reversing direction or starting at a different point on a loop can still match. The route name uses the representative ride's existing reverse-geocoded start place when available and visible. Otherwise it uses the route type and distance.

Hidden, provider-restricted, and deleted activities are excluded from the shared catalog. Members see the representative geometry with its owner's privacy zones masked, including in GPX and the Route Planner. GPS gaps and masked sections are kept as separate segments. The Route Planner can reverse a recording, shorten it, or change the start of a continuous loop before exporting.

## Upgrade and maintenance

Migration **90** adds the recorded-track index, route memberships, and processing queue. Rebuild and restart the Peloton Docker application using the existing database and settings. With `CAIRN_DATABASE_AUTO_MIGRATE=true`, startup applies the migration. Otherwise run `cairn migrate up` with the new image and the normal instance environment before restarting. There is no need to reimport rides.

The server fills the catalog in the background after deployment and resumes queued work after a restart. New or corrected activities are processed ahead of the historical backlog. A route drops out of the catalog if no rider still has two qualifying rides. As long as a representative can be retained or replaced from existing matches, its ID can be reused when the route qualifies again. The existing activity merge recompute operation also queues route repair.

The detail map uses Peloton's configured interactive basemap. The small route previews use OpenStreetMap tiles with a track-only fallback.
