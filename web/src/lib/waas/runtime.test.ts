import { describe, expect, it } from 'vitest';
import { WaasRouteMetrics } from '../../../static/waas-engine/route-metrics.js';
import { recordedElevation } from './metrics.js';
import { finiteElevation } from './recorded.js';
import { runtime } from './runtime.js';
import type { Plan, Point } from './types.js';

const points: Point[] = WaasRouteMetrics.measure([
	[51, 4, 10],
	[51.001, 4, 30],
	[51.002, 4, 10],
	[51.001, 4, 30],
	[51, 4, 10]
]).points;

function plan(passes = 1): Plan {
	const chunk = {
		id: 'prepared-loop',
		from: 'sn',
		to: 'sn',
		points,
		meters: points.at(-1)![3],
		gain: 40,
		metricsVersion: 1,
		climbs: [],
		bridges: [],
		sections: []
	};
	return {
		chunks: Array.from({ length: passes }, () => chunk),
		meters: chunk.meters * passes,
		gain: chunk.gain * passes,
		profile: 'climb',
		distanceOff: 0,
		gainOff: 0,
		reverse: 0,
		repeat: 0,
		selectedZones: []
	};
}

describe('the planner runtime shipped with the interface', () => {
	it('supplies all summary heights and a profile from the exported GPS points', async () => {
		const { createPlanner } = await runtime();
		const planner = createPlanner({});
		const ride = planner.assemble(plan(3));
		expect(ride.elevation).toEqual({ ascent: 120, descent: 120, highest: 30, lowest: 10 });
		expect(ride.gain).toBe(ride.elevation.ascent);
		expect(ride.meters).toBeCloseTo(points.at(-1)![3] * 3);
		const xml = planner.gpx(ride, 'sn');
		const exportedHeights = [...xml.matchAll(/<ele>([^<]+)<\/ele>/g)].map((entry) =>
			Number(entry[1])
		);
		expect(exportedHeights).toEqual(ride.points.map((point) => point[2]));
		expect(exportedHeights).toHaveLength(13);
	});

	it('also supplies the recorded-route editor with the matching height calculator', async () => {
		const measure = await recordedElevation();
		const stats = measure(
			[
				[
					{ lat: 51, lon: 4, distance_m: 0, elevation_m: 0 },
					{ lat: 51.001, lon: 4, distance_m: 100, elevation_m: 10 }
				],
				[
					{ lat: 51.01, lon: 4, distance_m: 100, elevation_m: 100 },
					{ lat: 51.011, lon: 4, distance_m: 200, elevation_m: 90 }
				]
			],
			finiteElevation
		);
		expect(stats).toEqual({ ascent: 10, descent: 10, highest: 100, lowest: 0 });
	});

	it('retains zero elevation for a flat route and unknown values for missing heights', async () => {
		const measure = await recordedElevation();
		const flat = points.map(([lat, lon, , distance_m]) => ({
			lat,
			lon,
			distance_m,
			elevation_m: 0
		}));
		expect(measure([flat], finiteElevation)).toEqual({
			ascent: 0,
			descent: 0,
			highest: 0,
			lowest: 0
		});
		const missing = points.map(([lat, lon, , distance_m]) => ({ lat, lon, distance_m }));
		expect(measure([missing], finiteElevation)).toEqual({
			ascent: null,
			descent: null,
			highest: null,
			lowest: null
		});
	});
});
