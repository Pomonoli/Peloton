import { afterEach, describe, expect, it, vi } from 'vitest';
import {
	catalogFeatures,
	catalogViewport,
	fetchSegmentCatalog,
	type CatalogSegment
} from './segment-catalog';

const segment: CatalogSegment = {
	id: 'route',
	name: 'Alpine Ridge',
	distance_m: 800,
	best_elapsed_s: 50,
	average_speed_kmh: 57.6,
	local_kom_holder: 'Alice',
	member_count: 2,
	start: [7.98, 47.01]
};

describe('segment catalog viewport', () => {
	it('bounds a local map request and preserves wrapped longitude ranges', () => {
		expect(catalogViewport(7.99, 47.005, 8.01, 47.015)).toEqual({
			min_lon: '7.99000',
			min_lat: '47.00500',
			max_lon: '8.01000',
			max_lat: '47.01500'
		});
		const wrapped = {
			min_lon: '179.95000',
			min_lat: '9.90000',
			max_lon: '-179.95000',
			max_lat: '10.10000'
		};
		expect(catalogViewport(179.95, 9.9, -179.95, 10.1)).toEqual(wrapped);
		expect(catalogViewport(539.95, 9.9, 540.05, 10.1)).toEqual(wrapped);
		expect(catalogViewport(-180, -90, 180, 90)).toBeNull();
		expect(catalogViewport(10, 40, 11, 40)).toBeNull();
	});

	it('uses all stored route vertices and skips rows without line geometry', () => {
		const result = catalogFeatures([
			{ ...segment, polyline: 'os|}G_ruo@?_|B?_|B', polyline_precision: 5 },
			{ ...segment, id: 'no-geometry' }
		]);
		expect(result.features).toEqual([
			{
				type: 'Feature',
				id: 'route',
				properties: { id: 'route' },
				geometry: {
					type: 'LineString',
					coordinates: [
						[7.98, 47.01],
						[8, 47.01],
						[8.02, 47.01]
					]
				}
			}
		]);
		const dateline = catalogFeatures([{ ...segment, polyline: '_c`|@_v_ia@?~l`scA' }]);
		expect(dateline.features[0]?.geometry.coordinates).toEqual([
			[179.9, 10],
			[180.1, 10]
		]);
	});
});

afterEach(() => vi.unstubAllGlobals());

it('sends the criteria literally and passes cancellation through to the catalog request', async () => {
	const fetch = vi
		.fn()
		.mockResolvedValueOnce({
			ok: true,
			json: async () => ({ segments: [segment], has_more: true })
		})
		.mockResolvedValueOnce({ ok: false });
	vi.stubGlobal('fetch', fetch);
	const { signal } = new AbortController();
	const result = await fetchSegmentCatalog(
		{ q: ' 100%_grade ', mine: 'true', limit: '50' },
		signal
	);
	expect(result).toEqual({ segments: [segment], has_more: true });
	expect(fetch).toHaveBeenCalledWith('/api/segments/catalog?q=+100%25_grade+&mine=true&limit=50', {
		signal
	});
	await expect(fetchSegmentCatalog({ q: 'missing' }, signal)).rejects.toThrow();
});
