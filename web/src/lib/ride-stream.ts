import { type ActivityStream, StreamChannel } from '#lib/proto/cairn/v1/activity_pb.js';
import { validTrackPoint, type TrackPoint } from '#lib/ride-track.js';

export interface ActivityStreamView {
	sourceID: string;
	resolutionHz: number;
	sampleCount: number;
	coordinates: [number, number][];
	offsets: number[];
	hr: (number | null)[] | null;
	power: (number | null)[] | null;
	cadence: (number | null)[] | null;
	speed: (number | null)[] | null;
	altitude: (number | null)[] | null;
	// One entry per GPS-bearing sample, index-aligned with `coordinates`. Carries
	// the time offset + per-point values so the map can snap to the route, show a
	// value overlay, and sync a hover position with the streams chart.
	track: TrackPoint[];
}

export function streamToView(s: ActivityStream | undefined): ActivityStreamView | null {
	if (!s) return null;
	const channels = new Set<StreamChannel>(s.channels);
	const hasGPS = channels.has(StreamChannel.LATITUDE) && channels.has(StreamChannel.LONGITUDE);
	const hasDist = channels.has(StreamChannel.DISTANCE);
	const hasEle = channels.has(StreamChannel.ALTITUDE);
	const hasSpeed = channels.has(StreamChannel.SPEED);
	const hasHR = channels.has(StreamChannel.HEART_RATE);
	const hasPower = channels.has(StreamChannel.POWER);
	const hasCad = channels.has(StreamChannel.CADENCE);

	const coordinates: [number, number][] = [];
	const track: TrackPoint[] = [];
	// floats use NaN for missing (but 0 is valid, e.g. distance at start);
	// ints use 0 for missing.
	// Out-of-range samples count as missing too.
	const nf = (v: number | undefined) => (v === undefined || Number.isNaN(v) ? null : v);
	const nz = (v: number | undefined) => (v === undefined || v === 0 ? null : v);
	if (hasGPS) {
		for (let i = 0; i < s.sampleCount; i++) {
			const lat = s.latitude[i];
			const lon = s.longitude[i];
			const t = s.timeS[i];
			if (lat === undefined || lon === undefined || t === undefined) continue;
			if (!validTrackPoint({ lat, lon, t })) continue;
			coordinates.push([lon, lat]);
			const speedMps = hasSpeed ? nf(s.speedMps[i]) : null;
			track.push({
				t,
				lon,
				lat,
				distM: hasDist ? nf(s.distanceM[i]) : null,
				eleM: hasEle ? nf(s.altitudeM[i]) : null,
				speedKmh: speedMps == null ? null : Math.round(speedMps * 3.6 * 10) / 10,
				hr: hasHR ? nz(s.heartRateBpm[i]) : null,
				power: hasPower ? nz(s.powerW[i]) : null,
				cadence: hasCad ? nz(s.cadence[i]) : null
			});
		}
	}
	return {
		sourceID: s.activitySourceId,
		resolutionHz: s.resolutionHz,
		sampleCount: s.sampleCount,
		coordinates,
		track,
		offsets: s.timeS,
		hr: channels.has(StreamChannel.HEART_RATE) ? intColToNullable(s.heartRateBpm) : null,
		power: channels.has(StreamChannel.POWER) ? intColToNullable(s.powerW) : null,
		cadence: channels.has(StreamChannel.CADENCE) ? intColToNullable(s.cadence) : null,
		speed: channels.has(StreamChannel.SPEED) ? floatColToNullable(s.speedMps) : null,
		altitude: channels.has(StreamChannel.ALTITUDE) ? floatColToNullable(s.altitudeM) : null
	};
}

// In the proto column-oriented stream format int columns use 0 for
// "missing" (repeated int32 has no in-band null). For chart rendering we
// want real nulls so uPlot draws gaps; map 0 → null.
function intColToNullable(col: number[]): (number | null)[] {
	return col.map((v) => (v === 0 ? null : v));
}

// Float columns use NaN for missing.
function floatColToNullable(col: number[]): (number | null)[] {
	return col.map((v) => (Number.isNaN(v) ? null : v));
}

// Align sensor-only sources to the primary GPS clock. Never move GPS vertices
// or fill a measurement across a missing merged-grid sample.
export function alignTrackSensors(
	track: TrackPoint[],
	grid: number[],
	primaryStart: number,
	hr: (number | null)[] | null,
	power: (number | null)[] | null
): TrackPoint[] {
	if (!grid.length || (!hr && !power)) return track;
	let i = 0;
	return track.map((p) => {
		const target = primaryStart + p.t;
		while (i + 1 < grid.length && Math.abs(grid[i + 1]! - target) < Math.abs(grid[i]! - target))
			i++;
		if (Math.abs(grid[i]! - target) > 2.5) return p;
		return { ...p, hr: p.hr ?? hr?.[i] ?? null, power: p.power ?? power?.[i] ?? null };
	});
}
