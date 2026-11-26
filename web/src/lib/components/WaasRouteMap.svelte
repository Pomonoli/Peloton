<script lang="ts">
	import { onMount } from 'svelte';
	import {
		Map as MapLibreMap,
		LngLatBounds,
		NavigationControl,
		type GeoJSONSource
	} from 'maplibre-gl';
	import 'maplibre-gl/dist/maplibre-gl.css';
	import { basemapStyle } from '#lib/basemap.js';
	import { appearance, appearanceColor } from '#lib/appearance.js';
	import { m } from '#lib/paraglide/messages.js';
	import { dataUrl } from '#lib/waas/service.js';
	import type { Ride } from '#lib/waas/types.js';
	import RoutePreview from './RoutePreview.svelte';
	import { mapLines, previewPoints, type RecordedTrack } from '#lib/recorded-routes.js';

	let {
		ride = null,
		bounds = null,
		recorded = null,
		compact = false
	}: {
		ride?: Ride | null;
		bounds?: [[number, number], [number, number]] | null;
		recorded?: RecordedTrack | null;
		compact?: boolean;
	} = $props();
	let container: HTMLDivElement;
	let map: MapLibreMap | undefined;
	let ready = $state(false);
	let unavailable = $state(false);
	let cycling = $state(false);
	let elevation = $state(false);
	let currentStyle: ReturnType<typeof basemapStyle>;
	const lines = $derived(
		recorded
			? mapLines(recorded)
			: [ride?.points.map((p) => [p[1], p[0]] as [number, number]) ?? []]
	);
	const coordinates = $derived(lines.flat());
	const fallbackTrack = $derived(recorded ? previewPoints(recorded) : []);

	function fit() {
		if (!map || !coordinates.length) return;
		const box = new LngLatBounds();
		for (const point of coordinates) box.extend(point);
		map.fitBounds(box, { padding: 45, maxZoom: 15, duration: 0 });
	}

	function draw() {
		if (!map || !ready) return;
		const line = {
			type: 'Feature' as const,
			properties: {},
			geometry: {
				type: 'MultiLineString' as const,
				coordinates: lines.filter((line) => line.length > 1)
			}
		};
		(map.getSource('waas-route') as GeoJSONSource).setData(line);
		const start = recorded?.[0]?.[0];
		const markers = [
			...(start
				? [{ point: [start.lat, start.lon], label: 'S' }]
				: ride?.points[0]
					? [{ point: ride.points[0], label: 'S' }]
					: []),
			...(ride?.visits ?? []).map((place, i) => ({ point: place.point, label: String(i + 1) }))
		];
		(map.getSource('waas-places') as GeoJSONSource).setData({
			type: 'FeatureCollection',
			features: markers.map(({ point, label }) => ({
				type: 'Feature',
				properties: { label },
				geometry: { type: 'Point', coordinates: [point[1]!, point[0]!] }
			}))
		});
		if (bounds && !map.getSource('waas-height')) {
			const [[south, west], [north, east]] = bounds;
			map.addSource('waas-height', {
				type: 'image',
				url: dataUrl('terrain.png'),
				coordinates: [
					[west, north],
					[east, north],
					[east, south],
					[west, south]
				]
			});
			map.addLayer(
				{
					id: 'waas-height',
					type: 'raster',
					source: 'waas-height',
					paint: { 'raster-opacity': 0.6 },
					layout: { visibility: elevation ? 'visible' : 'none' }
				},
				'waas-cycling'
			);
		}
		map.setLayoutProperty('waas-cycling', 'visibility', cycling ? 'visible' : 'none');
		if (map.getLayer('waas-height'))
			map.setLayoutProperty('waas-height', 'visibility', elevation ? 'visible' : 'none');
		fit();
	}

	onMount(() => {
		let timer = 0;
		try {
			currentStyle = basemapStyle($appearance.mode);
			map = new MapLibreMap({
				container,
				style: currentStyle,
				center: coordinates[0] ?? [4.139184, 51.164565],
				zoom: 10.5,
				attributionControl: { compact: true },
				scrollZoom: !compact
			});
			map.addControl(new NavigationControl({ showCompass: false }), 'bottom-right');
			timer = window.setTimeout(() => {
				if (!ready) unavailable = true;
			}, 10000);
			map.on('style.load', () => {
				if (!map) return;
				map.addSource('waas-cycling', {
					type: 'raster',
					tiles: ['https://tile.waymarkedtrails.org/cycling/{z}/{x}/{y}.png'],
					tileSize: 256,
					attribution: '© Waymarked Trails'
				});
				map.addLayer({
					id: 'waas-cycling',
					type: 'raster',
					source: 'waas-cycling',
					layout: { visibility: cycling ? 'visible' : 'none' }
				});
				map.addSource('waas-route', {
					type: 'geojson',
					data: { type: 'FeatureCollection', features: [] }
				});
				map.addLayer({
					id: 'waas-casing',
					type: 'line',
					source: 'waas-route',
					paint: { 'line-color': appearanceColor('--map-casing'), 'line-width': 8 },
					layout: { 'line-cap': 'round', 'line-join': 'round' }
				});
				map.addLayer({
					id: 'waas-line',
					type: 'line',
					source: 'waas-route',
					paint: { 'line-color': appearanceColor('--accent-primary'), 'line-width': 4 },
					layout: { 'line-cap': 'round', 'line-join': 'round' }
				});
				map.addLayer({
					id: 'waas-arrows',
					type: 'symbol',
					source: 'waas-route',
					layout: {
						'symbol-placement': 'line',
						'symbol-spacing': 140,
						'text-field': '›',
						'text-size': 21,
						'text-keep-upright': false
					},
					paint: {
						'text-color': appearanceColor('--accent-primary'),
						'text-halo-color': appearanceColor('--map-casing'),
						'text-halo-width': 2
					}
				});
				map.addSource('waas-places', {
					type: 'geojson',
					data: { type: 'FeatureCollection', features: [] }
				});
				map.addLayer({
					id: 'waas-place-dot',
					type: 'circle',
					source: 'waas-places',
					paint: {
						'circle-radius': 13,
						'circle-color': appearanceColor('--accent-primary'),
						'circle-stroke-color': appearanceColor('--map-casing'),
						'circle-stroke-width': 2
					}
				});
				map.addLayer({
					id: 'waas-place-label',
					type: 'symbol',
					source: 'waas-places',
					layout: { 'text-field': ['get', 'label'], 'text-size': 12, 'text-allow-overlap': true },
					paint: { 'text-color': '#ffffff' }
				});
				window.clearTimeout(timer);
				ready = true;
				unavailable = false;
				draw();
			});
		} catch (cause) {
			console.warn('Waasland map unavailable', cause);
			unavailable = true;
		}
		return () => {
			window.clearTimeout(timer);
			map?.remove();
		};
	});

	$effect(() => {
		void ride;
		void recorded;
		void bounds;
		void cycling;
		void elevation;
		if (ready) draw();
	});
	$effect(() => {
		const next = basemapStyle($appearance.mode);
		if (map && next !== currentStyle) {
			currentStyle = next;
			ready = false;
			map.setStyle(next, { diff: false });
		}
	});
</script>

<div class="relative overflow-hidden surface">
	<div
		bind:this={container}
		class={compact ? 'h-56 w-full sm:h-64' : 'h-[440px] w-full max-md:h-[42dvh] max-md:min-h-60'}
		aria-label={m.waas_map_label()}
	></div>
	{#if !ready && coordinates.length}
		<RoutePreview
			{coordinates}
			track={fallbackTrack}
			point={null}
			metric={null}
			disconnected={!!recorded}
		/>
	{/if}
	<div
		class="absolute left-3 top-3 flex max-w-[calc(100%-1.5rem)] flex-wrap gap-1 surface p-1 shadow-sm"
	>
		{#if !compact}
			<button
				type="button"
				class="rounded px-2 py-1.5 text-sm text-zinc-300"
				onclick={() => {
					cycling = false;
					elevation = false;
				}}>{m.waas_roads()}</button
			>
			<button
				type="button"
				aria-pressed={cycling}
				class="rounded px-2 py-1.5 text-sm {cycling
					? 'bg-accent-500/20 text-accent-300'
					: 'text-zinc-300'}"
				onclick={() => (cycling = !cycling)}>{m.waas_cycle_layer()}</button
			>
			<button
				type="button"
				aria-pressed={elevation}
				class="rounded px-2 py-1.5 text-sm {elevation
					? 'bg-accent-500/20 text-accent-300'
					: 'text-zinc-300'}"
				onclick={() => (elevation = !elevation)}>{m.waas_height_layer()}</button
			>
		{/if}
		<button type="button" class="rounded px-2 py-1.5 text-sm text-zinc-300" onclick={fit}
			>{m.waas_fit_route()}</button
		>
	</div>
	{#if unavailable}<p
			class="absolute bottom-3 left-3 right-12 rounded bg-panel p-2 text-sm text-zinc-400"
			role="status"
		>
			{m.waas_map_unavailable()}
		</p>{/if}
</div>
