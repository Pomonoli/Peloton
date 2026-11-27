import { describe, expect, it } from 'vitest';
import {
	linearTrend,
	scatterQuery,
	scatterScale,
	type SpeedDistancePoint
} from './speed-distance.js';

const point = (distance_km: number, speed_kmh: number): SpeedDistancePoint => ({
	id: String(distance_km),
	title: '',
	start: '',
	distance_km,
	speed_kmh
});

describe('speed versus distance', () => {
	it('fits a trend only when distances vary', () => {
		expect(linearTrend([point(20, 20), point(20, 25)])).toBeNull();
		expect(linearTrend([point(10, 20), point(20, 30), point(30, 40)])).toEqual({
			low: 10,
			high: 30,
			lowSpeed: 20,
			highSpeed: 40
		});
	});

	it('pads the chart for a single value and keeps narrow scales legible', () => {
		const one = scatterScale(20, 20, 5);
		expect(one.min).toBeLessThan(20);
		expect(one.max).toBeGreaterThan(20);
		const small = scatterScale(0.02, 0.15, 4);
		expect(small.min).toBe(0);
		expect(small.max).toBeGreaterThan(0.15);
		expect(small.ticks.length).toBeGreaterThan(1);
	});

	it('intersects the Stats year with supplied activity filters', () => {
		const query = new URLSearchParams(
			scatterQuery('?member=mira&from=2025-05-01&gear_id=bike', '2025')
		);
		expect(query.has('member')).toBe(false);
		expect(query.get('from')).toBe('2025-05-01');
		expect(query.get('year')).toBe('2025');
		expect(query.get('gear_id')).toBe('bike');
	});
});
