import { describe, expect, it } from 'vitest';
import { editTrack } from './recorded.js';

const point = (lon: number, distance_m: number) => ({ lat: 51, lon, distance_m });

describe('recorded planner geometry', () => {
	it('reverses actual positions and retains gaps without adding distance', () => {
		const original = [
			[point(1, 0), point(2, 30)],
			[point(3, 80), point(4, 100)]
		];
		const reversed = editTrack(original, 0, 100, 0, true);
		expect(reversed).toEqual([
			[point(4, 0), point(3, 20)],
			[point(2, 20), point(1, 50)]
		]);
		expect(original[0]?.[0]?.lon).toBe(1);
	});

	it('can choose a new start on a loop while preserving its length', () => {
		const original = [[point(1, 0), point(2, 30), point(3, 50), point(1, 100)]];
		expect(editTrack(original, 0, 100, 50, false)).toEqual([
			[point(3, 0), point(1, 50), point(2, 80), point(3, 100)]
		]);
		expect(editTrack(original, 0, 100, 50, true)).toEqual([
			[point(3, 0), point(2, 20), point(1, 50), point(3, 100)]
		]);
	});

	it('trims by recorded distance and does not add a connector', () => {
		const original = [[point(1, 0), point(2, 30), point(3, 60), point(4, 100)]];
		expect(editTrack(original, 20, 80, 0, false)).toEqual([[point(2, 0), point(3, 30)]]);
	});
});
