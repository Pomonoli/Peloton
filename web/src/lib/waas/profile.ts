import type { RecordedTrack } from '#lib/recorded-routes.js';
import { finiteElevation, indexAtDistance, interpolatePoint, trackDistance } from './recorded.js';

export function elevationProfile(track: RecordedTrack) {
	return track.flatMap((line) => {
		const runs: (typeof line)[] = [];
		let run: typeof line = [];
		for (const point of line) {
			if (finiteElevation(point) == null) {
				run = [];
				continue;
			}
			if (!run.length) runs.push(run);
			run.push(point);
		}
		return runs;
	});
}

export function profilePoint(track: RecordedTrack, runs: RecordedTrack, meters: number) {
	const line = lineAtDistance(track, meters);
	if (!line) return null;
	const index = indexAtDistance(line, meters);
	const next = line[index]!;
	const point =
		index === 0 || next.distance_m === meters
			? next
			: interpolatePoint(line[index - 1]!, next, meters);
	const run = lineAtDistance(runs, meters, line[0]!);
	let gradient: number | null = null;
	if (run && finiteElevation(point) != null) {
		const start = Math.max(run[0]!.distance_m, meters - 30);
		const end = Math.min(trackDistance([run]), meters + 30);
		const first = Math.max(0, indexAtDistance(run, start) - 1);
		const last = indexAtDistance(run, end);
		const a = run[first]!;
		const b = run[last]!;
		const span = b.distance_m - a.distance_m;
		if (span >= 30 && span <= 150) {
			const grade = (100 * (b.elevation_m! - a.elevation_m!)) / span;
			if (Math.abs(grade) <= 35) gradient = grade;
		}
	}
	return { point, gradient };
}

// At a gap the preceding endpoint owns the shared distance. Restrict the
// elevation run to the same GPS segment in case it starts at that distance.
function lineAtDistance(track: RecordedTrack, meters: number, start?: { distance_m: number }) {
	let low = 0;
	let high = track.length;
	while (low < high) {
		const mid = (low + high) >> 1;
		if (track[mid]!.at(-1)!.distance_m < meters) low = mid + 1;
		else high = mid;
	}
	const line = track[low];
	return line &&
		line[0]!.distance_m <= meters &&
		(!start || line[0]!.distance_m >= start.distance_m)
		? line
		: null;
}

export function tickStep(raw: number) {
	const base = 10 ** Math.floor(Math.log10(raw));
	return ([1, 2, 2.5, 5, 10].find((step) => step * base >= raw) ?? 10) * base;
}

export function axisTicks(start: number, end: number, step: number) {
	return Array.from({ length: Math.max(0, Math.floor((end - start) / step + 1e-8) + 1) }, (_, i) =>
		Number((start + i * step).toPrecision(10))
	);
}
