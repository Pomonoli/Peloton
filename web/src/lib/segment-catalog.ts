import type { SortCriterion } from '#lib/stacked-sort.js';
import type { FeatureCollection, LineString } from 'geojson';
import { decodePolylineLonLat } from '#lib/polyline.js';

export interface CatalogSegment {
	id: string;
	name: string;
	distance_m: number;
	best_elapsed_s: number;
	average_speed_kmh: number | null;
	local_kom_holder: string;
	member_count: number;
	my_attempts: number;
	start: [number, number] | null;
	polyline?: string;
	polyline_precision?: number;
}

export type SortColumn = Exclude<
	keyof CatalogSegment,
	'id' | 'start' | 'polyline' | 'polyline_precision'
>;
export type CatalogSort = SortCriterion<SortColumn>;

export function defaultCatalogSort(mySegments: boolean): CatalogSort[] {
	return mySegments
		? [{ column: 'my_attempts', direction: 'desc' }]
		: [{ column: 'name', direction: 'asc' }];
}

export interface CatalogPage {
	segments: CatalogSegment[];
	has_more: boolean;
}

export interface MapCamera {
	center: [number, number];
	zoom: number;
}

export function catalogViewport(west: number, south: number, east: number, north: number) {
	const width = east < west ? east + 360 - west : east - west;
	if (!(width > 0 && width <= 120 && north > south && north - south <= 120)) return null;
	// MapLibre can pan through repeated copies of the world. Send canonical
	// longitudes, with west > east when the viewport crosses the antimeridian.
	const wrappedWest = ((((west + 180) % 360) + 360) % 360) - 180;
	const unwrappedEast = wrappedWest + width;
	return {
		min_lon: wrappedWest.toFixed(5),
		min_lat: Math.max(-90, south).toFixed(5),
		max_lon: (unwrappedEast > 180 ? unwrappedEast - 360 : unwrappedEast).toFixed(5),
		max_lat: Math.min(90, north).toFixed(5)
	};
}

export async function fetchSegmentCatalog(
	params: Record<string, string>,
	order: CatalogSort[],
	signal: AbortSignal
) {
	const query = new URLSearchParams(params);
	for (const { column, direction } of order) query.append('sort', `${column}:${direction}`);
	const res = await fetch(`/api/segments/catalog?${query}`, { signal });
	if (!res.ok) throw new Error('Could not load the segment catalog.');
	return (await res.json()) as CatalogPage;
}

export function catalogFeatures(
	rows: CatalogSegment[]
): FeatureCollection<LineString, { id: string }> {
	const features: FeatureCollection<LineString, { id: string }>['features'] = [];
	for (const row of rows) {
		const coordinates = decodePolylineLonLat(row.polyline ?? '', row.polyline_precision ?? 5);
		if (coordinates.length < 2) continue;
		// Keep a real dateline crossing on the short side of the map.
		for (let i = 1; i < coordinates.length; i++) {
			const point = coordinates[i]!;
			const delta = point[0] - coordinates[i - 1]![0];
			if (Math.abs(delta) > 180) point[0] -= 360 * Math.round(delta / 360);
		}
		features.push({
			type: 'Feature',
			id: row.id,
			properties: { id: row.id },
			geometry: { type: 'LineString', coordinates }
		});
	}
	return { type: 'FeatureCollection', features };
}
