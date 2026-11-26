import type { RecordedTrack } from '#lib/recorded-routes.js';

export function canMoveStart(track: RecordedTrack) {
	return track.length === 1 && track[0]!.length >= 4;
}

// Preserve real recorded points and segment breaks, including at the
// original start of a loop whose endpoints are slightly different.
export function editTrack(
	track: RecordedTrack,
	from: number,
	to: number,
	start: number,
	reverse: boolean
): RecordedTrack {
	const first = track[0]?.[0];
	const last = track.at(-1)?.at(-1);
	if (!first || !last) return [];
	const full = last.distance_m - first.distance_m;
	let lines = track;
	if ((from > 0 || to < 100) && full > 0) {
		const low = first.distance_m + (full * from) / 100;
		const high = first.distance_m + (full * to) / 100;
		lines = track
			.map((line) => line.filter((p) => p.distance_m >= low && p.distance_m <= high))
			.filter((line) => line.length > 1);
	} else if (start > 0 && canMoveStart(track)) {
		const line = track[0]!;
		const split = line.findIndex(
			(point) => point.distance_m >= first.distance_m + (full * start) / 100
		);
		if (split > 0 && split < line.length - 1) {
			const after = line.slice(split);
			const before = line.slice(0, split + 1);
			lines =
				first.lat === last.lat && first.lon === last.lon
					? [[...after, ...before.slice(1)]]
					: [after, before];
		}
	}
	if (reverse) lines = lines.toReversed().map((line) => line.toReversed());

	let distance = 0;
	return lines.map((line) =>
		line.map((point, index) => {
			if (index) {
				let delta = point.distance_m - line[index - 1]!.distance_m;
				if (reverse) delta = -delta;
				distance += delta < 0 ? delta + full : delta;
			}
			return { ...point, distance_m: distance };
		})
	);
}
