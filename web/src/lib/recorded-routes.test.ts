import { afterEach, describe, expect, it, vi } from 'vitest';
import {
	mapLines,
	previewPoints,
	recordedRoutePath,
	riderName,
	routeData,
	routeDistance,
	routeElevation,
	routeKind,
	type RecordedTrack
} from './recorded-routes.js';

vi.mock('#lib/paraglide/runtime.js', async (original) => ({
	...(await original<typeof import('#lib/paraglide/runtime.js')>()),
	getLocale: () => 'en'
}));

afterEach(() => vi.unstubAllGlobals());

describe('recorded routes', () => {
	it('builds a safe local path and fetches route data with cancellation', async () => {
		const url = recordedRoutePath('a/b');
		expect(url).toBe('/api/routes/a%2Fb');
		const signal = new AbortController().signal;
		const fetch = vi.fn().mockResolvedValueOnce(Response.json({ id: 'a/b' }));
		vi.stubGlobal('fetch', fetch);
		await expect(routeData(url, signal)).resolves.toEqual({ id: 'a/b' });
		expect(fetch).toHaveBeenCalledWith(url, { signal });

		fetch.mockResolvedValueOnce(new Response(null, { status: 404 }));
		await expect(routeData(url, signal)).rejects.toThrow('404');
	});

	it('formats route type and keeps units metric', () => {
		expect(routeKind('loop')).toBe('Loop');
		expect(routeKind('out_and_back')).toBe('Out & Back');
		expect(routeKind('point_to_point')).toBe('Point to Point');
		expect(routeDistance(72_425)).toBe('72.4 km');
		expect(routeDistance(650)).toBe('0.65 km');
		expect(routeDistance(null)).toBe('—');
		expect(routeElevation(219.7)).toBe('220 m');
		expect(routeElevation(null)).toBe('—');
	});

	it('falls back to a username when the display name is blank', () => {
		const rider = { id: 'one', username: 'ada', display_name: ' Ada ' };
		expect(riderName(rider)).toBe('Ada');
		expect(riderName({ ...rider, display_name: '  ' })).toBe('ada');
	});

	it('keeps recording gaps and unwraps longitude across the date line', () => {
		const track: RecordedTrack = [
			[
				{ lat: 52, lon: 179.75, distance_m: 0, elevation_m: 34 },
				{ lat: 52, lon: -179.75, distance_m: 20 }
			],
			[
				{ lat: 53, lon: -179.5, distance_m: 20 },
				{ lat: 53, lon: -179.25, distance_m: 30 }
			]
		];
		expect(previewPoints(track).map((p) => [p.connected, p.distM, p.eleM])).toEqual([
			[false, 0, 34],
			[true, 20, null],
			[false, 20, null],
			[true, 30, null]
		]);
		expect(mapLines(track)).toEqual([
			[
				[179.75, 52],
				[180.25, 52]
			],
			[
				[180.5, 53],
				[180.75, 53]
			]
		]);
		expect(mapLines([])).toEqual([]);
	});
});
