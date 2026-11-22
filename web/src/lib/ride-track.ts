import type { FeatureCollection, LineString } from 'geojson';

export interface TrackPoint {
	t: number;
	lon: number;
	lat: number;
	distM: number | null;
	eleM: number | null;
	speedKmh: number | null;
	hr: number | null;
	power: number | null;
	cadence: number | null;
}

export interface RidePoint extends TrackPoint {
	gradient: number | null;
	// False across recording gaps and implausible GPS jumps.
	connected: boolean;
}

export const rideMetrics = ['speedKmh', 'eleM', 'gradient', 'hr', 'power'] as const;
export type RideMetric = (typeof rideMetrics)[number];

const MAX_GAP_S = 30;
const MAX_SPEED_MPS = 35; // 126 km/h
const RAD = Math.PI / 180;

function gpsDistance(a: TrackPoint, b: TrackPoint): number {
	const h =
		Math.sin(((b.lat - a.lat) * RAD) / 2) ** 2 +
		Math.cos(a.lat * RAD) * Math.cos(b.lat * RAD) * Math.sin(((b.lon - a.lon) * RAD) / 2) ** 2;
	return 12_742_000 * Math.asin(Math.sqrt(Math.min(1, h)));
}

function median(values: number[]): number | null {
	values.sort((a, b) => a - b);
	const mid = values.length >> 1;
	return values.length === 0 ? null : (values[mid]! + values[(values.length - 1) >> 1]!) / 2;
}

export function validTrackPoint(p: Pick<TrackPoint, 't' | 'lon' | 'lat'>): boolean {
	return (
		Number.isFinite(p.t) &&
		Number.isFinite(p.lon) &&
		Number.isFinite(p.lat) &&
		Math.abs(p.lat) <= 90 &&
		Math.abs(p.lon) <= 180
	);
}

// Work on recorded samples in timestamp order. Smooth speeds and altitude over
// short windows and derive grade across at least 30 meters of continuous track.
export function analyzeTrack(track: readonly TrackPoint[]): RidePoint[] {
	const points: TrackPoint[] = [];
	for (const p of track) {
		if (validTrackPoint(p) && p.t > (points.at(-1)?.t ?? -Infinity)) points.push(p);
	}
	const gaps: boolean[] = [];
	const gpsSpeeds: (number | null)[] = [];
	const blocks: number[] = [];
	const distances: number[] = [];
	let distance = 0;
	for (const [i, p] of points.entries()) {
		const prev = points[i - 1];
		const dt = prev ? p.t - prev.t : 0;
		const gps = prev ? gpsDistance(prev, p) : 0;
		const connected = !!prev && dt <= MAX_GAP_S && gps / dt <= MAX_SPEED_MPS;
		gaps.push(!connected);
		blocks.push(connected ? blocks[i - 1]! : i);
		const recorded = prev && p.distM != null && prev.distM != null ? p.distM - prev.distM : -1;
		const step = recorded >= 0 && recorded / dt <= MAX_SPEED_MPS ? recorded : connected ? gps : 0;
		distance += step;
		distances.push(distance);
		gpsSpeeds.push(connected ? (step / dt) * 3.6 : null);
	}

	const altitude: (number | null)[] = [];
	const speeds: (number | null)[] = [];
	for (const [i, p] of points.entries()) {
		const ele = p.eleM;
		const nearbyEle = [];
		const nearbySpeed = [];
		const nearbyGPS = [];
		for (let j = Math.max(0, i - 12); j <= Math.min(i + 12, points.length - 1); j++) {
			const q = points[j]!;
			if (blocks[i] !== blocks[j] || Math.abs(p.t - q.t) > 12) continue;
			if (q.eleM != null && Number.isFinite(q.eleM)) nearbyEle.push(q.eleM);
			if (q.speedKmh != null && q.speedKmh >= 0 && q.speedKmh <= MAX_SPEED_MPS * 3.6) {
				nearbySpeed.push(q.speedKmh);
			}
			const gps = gpsSpeeds[j];
			if (gps != null) nearbyGPS.push(gps);
		}
		altitude.push(ele != null && Number.isFinite(ele) ? median(nearbyEle) : null);
		const gps = median(nearbyGPS);
		const recorded = median(nearbySpeed);
		speeds.push(
			recorded != null && (gps == null || Math.abs(recorded - gps) <= Math.max(20, gps * 0.7))
				? recorded
				: gps
		);
	}

	const legs: number[] = [];
	for (const [i] of points.entries()) {
		// Begin a new grade window after disconnected GPS or missing altitude.
		legs.push(gaps[i] || altitude[i] == null || altitude[i - 1] == null ? i : legs[i - 1]!);
	}
	let left = 0;
	let right = 0;
	return points.map((p, i) => {
		const d = distances[i]!;
		left = Math.max(left, legs[i]!);
		while (left < i && distances[left + 1]! <= d - 30) left++;
		right = Math.max(i, right);
		while (right + 1 < points.length && legs[right + 1] === legs[i] && distances[right]! < d + 30)
			right++;
		const span = distances[right]! - distances[left]!;
		const from = altitude[left];
		const to = altitude[right];
		const grade =
			span >= 30 && span <= 150 && from != null && to != null ? (100 * (to - from)) / span : null;
		return {
			...p,
			distM: d,
			eleM: altitude[i] ?? null,
			speedKmh: speeds[i] ?? null,
			gradient: grade != null && Math.abs(grade) <= 35 ? grade : null,
			connected: !gaps[i]
		};
	});
}

const sequential = ['#453781', '#2c728e', '#1f9e89', '#8dcc54', '#f9dc42'];
const diverging = ['#2b83ba', '#78bfdf', '#e8e8e8', '#f4a357', '#cb3c33'];
export const missingMetricColor = '#808891';

export function metricLegend(track: RidePoint[], metric: RideMetric) {
	const values: number[] = [];
	for (let i = 1; i < track.length; i++) {
		const value = track[i]![metric];
		if (track[i]!.connected && value != null) values.push(value);
	}
	if (!values.length) return [];
	values.sort((a, b) => a - b);
	const min = values[0]!;
	const max = values.at(-1)!;
	const colors = metric === 'gradient' && min < 0 && max > 0 ? diverging : sequential;
	if (min === max) return [{ color: colors[2]!, value: min }];
	return colors.map((color, i) => ({
		color,
		value:
			colors === diverging
				? i < 2
					? min * (1 - i / 2)
					: max * ((i - 2) / 2)
				: min + ((max - min) * i) / (colors.length - 1)
	}));
}

export function metricRoute(
	track: RidePoint[],
	metric: RideMetric | null
): FeatureCollection<LineString> {
	const features: FeatureCollection<LineString>['features'] = [];
	for (let i = 1; i < track.length; i++) {
		const p = track[i]!;
		const prev = track[i - 1]!;
		if (!p.connected) continue;
		features.push({
			type: 'Feature',
			properties: { value: metric ? p[metric] : null, index: i },
			geometry: {
				type: 'LineString',
				coordinates: [
					[prev.lon, prev.lat],
					[p.lon, p.lat]
				]
			}
		});
	}
	return { type: 'FeatureCollection', features };
}

// Index of the last recorded sample at or before t. Replay holds that sample
// until the next timestamp, including across gaps in the recording.
export function pointAtTime(track: readonly TrackPoint[], t: number): number {
	let lo = 0;
	let hi = track.length;
	while (lo < hi) {
		const mid = (lo + hi) >> 1;
		if (track[mid]!.t <= t) lo = mid + 1;
		else hi = mid;
	}
	return Math.max(0, lo - 1);
}
