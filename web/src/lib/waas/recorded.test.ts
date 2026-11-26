import { describe, expect, it } from 'vitest';
import { cutTrack, moveTrackStart, rebaseTrack, reverseTrack } from './recorded.js';
import { axisTicks, elevationProfile, profilePoint, tickStep } from './profile.js';

const point = (lon: number, distance_m: number, elevation_m?: number) => ({
	lat: 51,
	lon,
	distance_m,
	...(elevation_m == null ? {} : { elevation_m })
});

describe('recorded planner geometry', () => {
	it('reverses recorded positions and retains gaps without adding distance', () => {
		const original = [
			[point(1, 0), point(2, 30)],
			[point(3, 80), point(4, 100)]
		];
		expect(reverseTrack(original)).toEqual([
			[point(4, 0), point(3, 20)],
			[point(2, 20), point(1, 50)]
		]);
		expect(original[0]?.[0]?.lon).toBe(1);
	});

	it('moves the start of a loop and preserves its length and any closing gap', () => {
		const original = [[point(1, 0), point(2, 30), point(3, 50), point(1, 100)]];
		const rotated = moveTrackStart(original, 50);
		expect(rotated).toEqual([[point(3, 0), point(1, 50), point(2, 80), point(3, 100)]]);
		expect(reverseTrack(rotated)).toEqual([
			[point(3, 0), point(2, 20), point(1, 50), point(3, 100)]
		]);
		original[0]![3] = point(1.2, 100);
		expect(moveTrackStart(original, 50)).toEqual([
			[point(3, 0), point(1.2, 50)],
			[point(1, 50), point(2, 80), point(3, 100)]
		]);
	});

	it('interpolates trim boundaries inside legs without bridging gaps or missing elevation', () => {
		const original = rebaseTrack([
			[point(1, 0, 10), point(4, 30, 40)],
			[point(5, 80, 180), point(7, 100)]
		]);
		expect(rebaseTrack(cutTrack(original, 10, 40))).toEqual([
			[point(2, 0, 20), point(4, 20, 40)],
			[point(5, 20, 180), point(6, 30)]
		]);
		expect(cutTrack(original, 10, 20)).toEqual([[point(2, 10, 20), point(3, 20, 30)]]);
		expect(cutTrack(original, 30, 50)).toEqual([[point(5, 30, 180), point(7, 50)]]);
	});

	it('interpolates over the date line using the short arc', () => {
		expect(cutTrack([[point(179, 0), point(-179, 100)]], 25, 75)).toEqual([
			[point(179.5, 25), point(-179.5, 75)]
		]);
	});
});

describe('route elevation profile', () => {
	it('shows absolute elevation and only calculates grade across continuous height samples', () => {
		const track = [
			[point(1, 0, -10), point(2, 40, -6), point(3, 70), point(4, 90, 12)],
			[point(5, 90, 342), point(6, 150, 348)]
		];
		const runs = elevationProfile(track);
		expect(runs.map((run) => run.length)).toEqual([2, 1, 2]);
		expect(profilePoint(track, runs, 20)).toMatchObject({
			point: { elevation_m: -8 },
			gradient: 10
		});
		expect(profilePoint(track, runs, 55)).toMatchObject({ point: { lon: 2.5 }, gradient: null });
		expect(profilePoint(track, runs, 90)).toMatchObject({
			point: { elevation_m: 12 },
			gradient: null
		});
		expect(profilePoint(track, runs, 120)).toMatchObject({
			point: { elevation_m: 345 },
			gradient: 10
		});
	});

	it('uses clean intervals at different metric scales', () => {
		expect(tickStep(31)).toBe(50);
		expect(axisTicks(-20, 20, 10)).toEqual([-20, -10, 0, 10, 20]);
		expect(axisTicks(0, 0.125, tickStep(0.02))).toEqual([0, 0.02, 0.04, 0.06, 0.08, 0.1, 0.12]);
	});
});
