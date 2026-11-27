import type { FeatureCollection, Position } from 'geojson';
import {
	NavigationControl,
	type GeoJSONSource,
	type Map,
	type MapGeoJSONFeature,
	type LngLat,
	type Point
} from 'maplibre-gl';
import { appearanceColor } from '#lib/appearance.js';

export const emptyFeatures: FeatureCollection = { type: 'FeatureCollection', features: [] };

export function addMapNavigation(map: Map, position: 'top-right' | 'bottom-right' = 'top-right') {
	map.touchZoomRotate.disableRotation();
	map.addControl(new NavigationControl({ showCompass: false }), position);
}

export function mapLongitude(lon: number, origin: number) {
	return origin + (((((lon - origin + 180) % 360) + 360) % 360) - 180);
}

export function bindMapSelection(map: Map, select: (point: Point) => void) {
	map.on('click', (event) => select(event.point));
	const canvas = map.getCanvas();
	canvas.setAttribute('aria-keyshortcuts', 'Enter');
	canvas.addEventListener('keydown', (event) => {
		if (event.key !== 'Enter') return;
		event.preventDefault();
		select(map.project(map.getCenter()));
	});
}

// Query the rendered tile index and choose the nearest line, including on
// touch screens. Crossing lines and parallel roads remain separate targets.
export function nearestMapLine(map: Map, point: Point, layer: string) {
	if (!map.getLayer(layer)) return;
	const { x, y } = point;
	const radius = 8;
	const hits = map.queryRenderedFeatures(
		[
			[x - radius, y - radius],
			[x + radius, y + radius]
		],
		{ layers: [layer] }
	);
	const origin = map.unproject(point).lng;
	let best: { feature: MapGeoJSONFeature; location: LngLat } | undefined;
	let bestDistance = radius;
	for (const hit of hits) {
		const { geometry } = hit;
		const lines =
			geometry.type === 'LineString'
				? [geometry.coordinates]
				: geometry.type === 'MultiLineString'
					? geometry.coordinates
					: [];
		for (const line of lines) {
			let previous: Point | undefined;
			for (const coordinate of line) {
				const next = map.project([mapLongitude(coordinate[0]!, origin), coordinate[1]!]);
				if (previous) {
					const target = nearestPoint(point, previous, next);
					const distance = Math.hypot(point.x - target[0], point.y - target[1]);
					const tied = Math.abs(bestDistance - distance) < 0.25;
					if (
						distance < bestDistance - 0.25 ||
						(tied &&
							(!best || Number(hit.properties.density) > Number(best.feature.properties.density)))
					) {
						bestDistance = distance;
						best = { feature: hit, location: map.unproject(target) };
					}
				}
				previous = next;
			}
		}
	}
	return best;
}

function nearestPoint(point: Point, a: Point, b: Point): [number, number] {
	const dx = b.x - a.x;
	const dy = b.y - a.y;
	const length = dx * dx + dy * dy;
	const t = length
		? Math.max(0, Math.min(1, ((point.x - a.x) * dx + (point.y - a.y) * dy) / length))
		: 0;
	return [a.x + t * dx, a.y + t * dy];
}

export function addDiscoveryOverlay(map: Map, mode: 'light' | 'dark') {
	const color = mode === 'dark' ? '#7cddff' : '#006d9c';
	map.addSource('discovery-track', { type: 'geojson', data: emptyFeatures });
	map.addSource('discovery-location', { type: 'geojson', data: emptyFeatures });
	for (const [id, stroke, width] of [
		['discovery-casing', appearanceColor('--map-casing'), 9],
		['discovery-highlight', color, 5]
	] as const) {
		map.addLayer({
			id,
			type: 'line',
			source: 'discovery-track',
			layout: { 'line-cap': 'round', 'line-join': 'round' },
			paint: { 'line-color': stroke, 'line-width': width }
		});
	}
	map.addLayer({
		id: 'discovery-pin',
		type: 'circle',
		source: 'discovery-location',
		paint: {
			'circle-color': color,
			'circle-radius': 5,
			'circle-stroke-color': appearanceColor('--map-casing'),
			'circle-stroke-width': 2
		}
	});
}

export function setMapFeatures(map: Map | null | undefined, id: string, data: FeatureCollection) {
	(map?.getSource(id) as GeoJSONSource | undefined)?.setData(data);
}

export function showDiscoveryLocation(map: Map | null | undefined, location: Position | null) {
	setMapFeatures(
		map,
		'discovery-location',
		location
			? {
					type: 'FeatureCollection',
					features: [
						{ type: 'Feature', properties: {}, geometry: { type: 'Point', coordinates: location } }
					]
				}
			: emptyFeatures
	);
}

export function showDiscoveryTrack(map: Map | null | undefined, lines: Position[][]) {
	setMapFeatures(map, 'discovery-track', {
		type: 'FeatureCollection',
		features: lines
			.filter((line) => line.length > 1)
			.map((line) => ({
				type: 'Feature',
				properties: {},
				geometry: {
					type: 'LineString',
					coordinates: line.map(([lon, lat]) => [mapLongitude(lon!, line[0]![0]!), lat!])
				}
			}))
	});
}
