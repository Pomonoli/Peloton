import type { RecordedTrack, RoutePoint } from '#lib/recorded-routes.js';

export function canMoveStart(track: RecordedTrack) {
	return track.length === 1 && track[0]!.length >= 4;
}

export function trackDistance(track: RecordedTrack) {
	return track.at(-1)?.at(-1)?.distance_m ?? 0;
}

// The API can retain distance offsets across privacy zones. Measure only the
// connected legs visible in the editor, including when they are reversed.
export function rebaseTrack(lines: RecordedTrack): RecordedTrack {
	let distance = 0;
	return lines
		.filter((line) => line.length > 1)
		.map((line) =>
			line.map((point, index) => {
				if (index) distance += Math.abs(point.distance_m - line[index - 1]!.distance_m);
				return { ...point, distance_m: distance };
			})
		);
}

export function reverseTrack(track: RecordedTrack): RecordedTrack {
	return rebaseTrack(track.toReversed().map((line) => line.toReversed()));
}

export function moveTrackStart(track: RecordedTrack, meters: number): RecordedTrack {
	if (!canMoveStart(track) || meters <= 0) return track;
	const line = track[0]!;
	const first = line[0]!;
	const last = line.at(-1)!;
	if (meters >= last.distance_m) return track;

	const before = cutTrack(track, 0, meters)[0]!;
	const after = cutTrack(track, meters, last.distance_m)[0]!;
	if (first.lat === last.lat && first.lon === last.lon && first.elevation_m === last.elevation_m) {
		const length = last.distance_m;
		// Keep the closing point and unwrap distances across the original start.
		const tail = before.slice(1).map((point) => ({
			...point,
			distance_m: point.distance_m + length
		}));
		return rebaseTrack([[...after, ...tail]]);
	}
	return rebaseTrack([after, before]);
}

// Both boundaries fall on existing legs. Never synthesize a connector across
// recording gaps or interpolate heights through a missing sample.
export function cutTrack(track: RecordedTrack, from: number, to: number): RecordedTrack {
	if (to <= from) return [];
	const lines: RecordedTrack = [];
	for (const line of track) {
		const start = Math.max(from, line[0]!.distance_m);
		const end = Math.min(to, line.at(-1)!.distance_m);
		if (end <= start) continue;
		const first = indexAtDistance(line, start);
		const last = indexAtDistance(line, end);
		const a = line[first]!;
		const b = line[last]!;
		lines.push([
			a.distance_m === start ? a : interpolatePoint(line[first - 1]!, a, start),
			...line.slice(first + Number(a.distance_m === start), last),
			b.distance_m === end ? b : interpolatePoint(line[last - 1]!, b, end)
		]);
	}
	return lines;
}

export function indexAtDistance(points: readonly RoutePoint[], meters: number): number {
	let lo = 0;
	let hi = points.length - 1;
	while (lo < hi) {
		const mid = (lo + hi) >> 1;
		if (points[mid]!.distance_m < meters) lo = mid + 1;
		else hi = mid;
	}
	return lo;
}

export function interpolatePoint(a: RoutePoint, b: RoutePoint, meters: number): RoutePoint {
	const fraction =
		b.distance_m > a.distance_m ? (meters - a.distance_m) / (b.distance_m - a.distance_m) : 0;
	const lon = unwrapLongitude(b.lon, a.lon);
	const height = finiteElevation(a);
	const nextHeight = finiteElevation(b);
	return {
		lat: a.lat + fraction * (b.lat - a.lat),
		lon: unwrapLongitude(a.lon + fraction * (lon - a.lon), 0),
		distance_m: meters,
		...(height != null && nextHeight != null
			? { elevation_m: height + fraction * (nextHeight - height) }
			: {})
	};
}

export function unwrapLongitude(lon: number, origin: number) {
	return origin + ((((lon - origin + 180) % 360) + 360) % 360) - 180;
}

export function finiteElevation(point: RoutePoint): number | null {
	const height = point.elevation_m;
	return height != null && Number.isFinite(height) ? height : null;
}
