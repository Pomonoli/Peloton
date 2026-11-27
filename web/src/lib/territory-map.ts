import type { FeatureCollection, MultiPolygon, Polygon } from 'geojson';
import { LngLatBounds, type Map } from 'maplibre-gl';
import { emptyFeatures, mapLongitude } from './discovery-map.js';

export type TerritoryLegendEntry = {
	id: string;
	name: string;
	color: string;
	cells: number;
	area_km2: number;
	percentage: number;
};

export type Territory = {
	version: string;
	geojson: FeatureCollection<Polygon | MultiPolygon>;
	legend: TerritoryLegendEntry[];
};

export type TerritoryCell = {
	owner_id: string;
	distances: { rider_id: string; distance_m: number }[];
};

export function addTerritoryLayers(map: Map, dark: boolean) {
	const layers = map.getStyle().layers;
	const before = layers.find(
		(layer) =>
			layer.type === 'symbol' ||
			('source-layer' in layer && layer['source-layer'] === 'transportation')
	)?.id;
	map.addSource('territory', { type: 'geojson', data: emptyFeatures });
	map.addLayer(
		{
			id: 'territory-fill',
			type: 'fill',
			source: 'territory',
			layout: { visibility: 'none' },
			paint: {
				'fill-color': ['to-color', ['get', 'color']],
				'fill-opacity': 0.25,
				'fill-antialias': false
			}
		},
		before
	);
	map.addLayer(
		{
			id: 'territory-outline',
			type: 'line',
			source: 'territory',
			layout: { visibility: 'none', 'line-join': 'round' },
			paint: {
				'line-color': dark ? '#e2e8f0' : '#25364c',
				'line-width': ['interpolate', ['linear'], ['zoom'], 5, 0.7, 12, 1.4],
				'line-opacity': 0.55
			}
		},
		before
	);
}

export function territoryBounds(data: Territory): LngLatBounds | null {
	let bounds: LngLatBounds | null = null;
	let origin = 0;
	for (const { geometry } of data.geojson.features) {
		const polygons = geometry.type === 'Polygon' ? [geometry.coordinates] : geometry.coordinates;
		for (const polygon of polygons) {
			for (const [longitude, latitude] of polygon[0] ?? []) {
				if (longitude === undefined || latitude === undefined) continue;
				if (!bounds) origin = longitude;
				const point: [number, number] = [mapLongitude(longitude, origin), latitude];
				bounds = bounds ? bounds.extend(point) : new LngLatBounds(point, point);
			}
		}
	}
	return bounds;
}
