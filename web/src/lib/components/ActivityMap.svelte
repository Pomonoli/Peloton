<script lang="ts">
	import { onMount } from 'svelte';
	import {
		type Map as MapLibreMap,
		LngLatBounds,
		Marker,
		NavigationControl,
		type GeoJSONSource,
		type ExpressionSpecification
	} from 'maplibre-gl';
	import 'maplibre-gl/dist/maplibre-gl.css';
	import { basemapStyle, createMap } from '#lib/basemap.js';
	import { appearance, appearanceColor } from '#lib/appearance.js';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDistance, formatDuration } from '#lib/format.js';
	import { metricLabel, metricValue } from '#lib/ride-display.js';
	import {
		analyzeTrack,
		metricLegend,
		metricRoute,
		missingMetricColor,
		pointAtTime,
		rideMetrics,
		type RideMetric,
		type RidePoint,
		type TrackPoint
	} from '#lib/ride-track.js';
	import RoutePreview from './RoutePreview.svelte';

	let {
		coordinates,
		heightClass = 'h-96',
		track = [],
		hoverT = null,
		onHover,
		replay
	}: {
		coordinates: [number, number][];
		heightClass?: string;
		track?: TrackPoint[] | RidePoint[];
		hoverT?: number | null;
		onHover?: (t: number | null) => void;
		replay?: {
			point: RidePoint;
			terrain: boolean;
			follow: boolean;
			playing: boolean;
			onExplore: () => void;
			onTerrainError: () => void;
		};
	} = $props();

	let container: HTMLDivElement;
	let map: MapLibreMap | undefined;
	let marker: Marker | undefined;
	let currentStyle: ReturnType<typeof basemapStyle>;
	let mapLoaded = $state(false);
	let mapUnavailable = $state(false);
	let selected = $state<RideMetric | null>(null);
	let inspected = $state<RidePoint | null>(null);
	let pinned = false;

	const isReplay = $derived(replay !== undefined);
	const terrainEnabled = $derived(replay?.terrain);
	const followEnabled = $derived(replay?.follow);
	const replayPlaying = $derived(replay?.playing);
	const route = $derived(
		track.length && 'gradient' in track[0]! ? (track as RidePoint[]) : analyzeTrack(track)
	);
	const legends = $derived(
		isReplay
			? null
			: (Object.fromEntries(
					rideMetrics.map((metric) => [metric, metricLegend(route, metric)])
				) as Record<RideMetric, ReturnType<typeof metricLegend>>)
	);
	const metric = $derived(selected && legends?.[selected].length ? selected : null);
	const legend = $derived(metric ? (legends?.[metric] ?? []) : []);
	const hasGaps = $derived(metric != null && route.some((p) => p.connected && p[metric] == null));
	const coloredRoute = $derived(isReplay ? null : metricRoute(route, metric));

	function inspect(point: RidePoint | null) {
		if (inspected?.t === point?.t) return;
		inspected = point;
		onHover?.(point?.t ?? null);
	}

	onMount(() => {
		const first = coordinates[0];
		const last = coordinates.at(-1);
		if (!first || !last) return;

		let styleTimer = 0;
		let terrainTimer = 0;
		function fallback() {
			mapUnavailable = true;
			mapLoaded = false;
			replay?.onTerrainError();
		}

		try {
			currentStyle = basemapStyle($appearance.mode);
			map = createMap({
				container,
				style: currentStyle,
				center: first,
				zoom: 12
			});
		} catch {
			fallback();
			return;
		}
		styleTimer = window.setTimeout(fallback, 8000);
		if (replay) {
			map.addControl(new NavigationControl({ visualizePitch: true }));
			for (const event of ['dragstart', 'rotatestart', 'pitchstart', 'zoomstart'] as const) {
				map.on(event, (e) => {
					if (e.originalEvent) replay?.onExplore();
				});
			}
		}

		let fitted = false;
		map.on('style.load', () => {
			if (!map) return;
			window.clearTimeout(styleTimer);
			mapUnavailable = false;
			// A replay follows the timed track, leaving recording gaps unconnected.
			const lines: [number, number][][] = [];
			if (replay) {
				for (const p of route) {
					if (!p.connected || !lines.length) lines.push([]);
					lines.at(-1)!.push([p.lon, p.lat]);
				}
			} else {
				lines.push(coordinates);
			}
			map.addSource('route', {
				type: 'geojson',
				data: {
					type: 'Feature',
					properties: {},
					geometry: {
						type: 'MultiLineString',
						coordinates: lines.filter((line) => line.length > 1)
					}
				}
			});
			for (const [id, color, width] of [
				['route-casing', '--map-casing', 7],
				['route-line', '--accent-primary', 4]
			] as const) {
				map.addLayer({
					id,
					type: 'line',
					source: 'route',
					paint: { 'line-color': appearanceColor(color), 'line-width': width },
					layout: { 'line-cap': 'round', 'line-join': 'round' }
				});
			}
			map.addSource('route-metric', {
				type: 'geojson',
				data: { type: 'FeatureCollection', features: [] }
			});
			map.addLayer({
				id: 'route-metric',
				type: 'line',
				source: 'route-metric',
				paint: { 'line-color': missingMetricColor, 'line-width': 4 },
				layout: { 'line-cap': 'round', 'line-join': 'round' }
			});
			map.addSource('endpoints', {
				type: 'geojson',
				data: {
					type: 'FeatureCollection',
					features: [first, last].map((p, i) => ({
						type: 'Feature',
						properties: { start: i === 0 },
						geometry: { type: 'Point', coordinates: p }
					}))
				}
			});
			map.addLayer({
				id: 'endpoint-markers',
				type: 'circle',
				source: 'endpoints',
				paint: {
					'circle-radius': 5,
					'circle-color': [
						'case',
						['get', 'start'],
						appearanceColor('--map-start'),
						appearanceColor('--map-finish')
					],
					'circle-stroke-color': appearanceColor('--map-marker'),
					'circle-stroke-width': 2
				}
			});

			if (!fitted) {
				const bounds = coordinates.reduce((b, c) => b.extend(c), new LngLatBounds(first, first));
				map.fitBounds(bounds, { padding: 32, duration: 0, maxZoom: 16 });
				fitted = true;
			}
			mapLoaded = true;
		});

		map.on('error', (event) => {
			if ('sourceId' in event && event.sourceId === 'replay-dem' && !('tile' in event)) {
				window.clearTimeout(terrainTimer);
				replay?.onTerrainError();
			}
		});
		map.on('sourcedata', (event) => {
			if (event.sourceId === 'replay-dem' && event.tile?.dem) {
				window.clearTimeout(terrainTimer);
			}
		});
		map.on('webglcontextlost', fallback);
		let lastInspection = 0;
		function inspectAt(e: { point: { x: number; y: number } }) {
			if (!map || !mapLoaded || mapUnavailable || replay) return;
			const { x, y } = e.point;
			const hits = map.queryRenderedFeatures(
				[
					[x - 14, y - 14],
					[x + 14, y + 14]
				],
				{ layers: ['route-metric'] }
			);
			let closest: RidePoint | null = null;
			let distance = 14;
			for (const hit of hits) {
				const i = Number(hit.properties.index);
				const a = route[i - 1];
				const b = route[i];
				if (!a || !b) continue;
				const start = map.project([a.lon, a.lat]);
				const end = map.project([b.lon, b.lat]);
				const dx = end.x - start.x;
				const dy = end.y - start.y;
				const fraction = Math.max(
					0,
					Math.min(1, ((x - start.x) * dx + (y - start.y) * dy) / (dx * dx + dy * dy || 1))
				);
				const d = Math.hypot(x - start.x - fraction * dx, y - start.y - fraction * dy);
				if (d < distance) {
					distance = d;
					// Map styling assigns the end sample's values to this interval.
					closest = metric || fraction >= 0.5 ? b : a;
				}
			}
			inspect(closest);
		}

		map.on('mousemove', (e) => {
			if (pinned || performance.now() - lastInspection < 32) return;
			lastInspection = performance.now();
			inspectAt(e);
		});
		map.on('mouseout', () => {
			if (!pinned) inspect(null);
		});
		map.on('click', (e) => {
			inspectAt(e);
			pinned = inspected != null;
		});

		let resizeFrame = 0;
		const observer = new ResizeObserver(() => {
			cancelAnimationFrame(resizeFrame);
			resizeFrame = requestAnimationFrame(() => map?.resize());
		});
		observer.observe(container);

		// Restart the terrain timeout each time a terrain source is enabled.
		const terrain = () => {
			window.clearTimeout(terrainTimer);
			if (!map || !mapLoaded) return;
			if (!terrainEnabled) {
				if (map.getTerrain()) {
					map.setTerrain(null);
					map.easeTo({ pitch: 0, duration: 300 });
				}
				return;
			}
			try {
				if (!map.getSource('replay-dem')) {
					const tiles =
						import.meta.env.VITE_TERRAIN_TILES_URL ||
						'https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png';
					map.addSource('replay-dem', {
						type: 'raster-dem',
						tiles: [tiles],
						encoding: 'terrarium',
						tileSize: 256,
						maxzoom: 15,
						attribution:
							'<a href="https://github.com/tilezen/joerd/blob/master/docs/attribution.md">Mapzen terrain data</a>'
					});
				}
				map.setTerrain({ source: 'replay-dem', exaggeration: 1 });
				map.easeTo({ pitch: 60, duration: 500 });
				terrainTimer = window.setTimeout(() => replay?.onTerrainError(), 10000);
			} catch {
				replay?.onTerrainError();
			}
		};
		applyTerrain = terrain;
		reloadStyle = (next) => {
			if (!map) return;
			window.clearTimeout(styleTimer);
			styleTimer = window.setTimeout(fallback, 8000);
			mapLoaded = false;
			map.setStyle(next, { diff: false });
		};

		return () => {
			window.clearTimeout(styleTimer);
			window.clearTimeout(terrainTimer);
			observer.disconnect();
			cancelAnimationFrame(resizeFrame);
			marker?.remove();
			map?.remove();
			map = undefined;
		};
	});

	let applyTerrain: (() => void) | undefined;
	let reloadStyle: ((next: ReturnType<typeof basemapStyle>) => void) | undefined;
	$effect(() => {
		const next = basemapStyle($appearance.mode);
		if (next === currentStyle) return;
		currentStyle = next;
		reloadStyle?.(next);
	});

	$effect(() => {
		if (mapLoaded && terrainEnabled !== undefined) applyTerrain?.();
	});

	$effect(() => {
		if (!mapLoaded || !map) return;
		const src = map.getSource<GeoJSONSource>('route-metric');
		src?.setData(coloredRoute ?? { type: 'FeatureCollection', features: [] });
		map.setLayoutProperty('route-line', 'visibility', metric ? 'none' : 'visible');
		map.setPaintProperty('route-metric', 'line-opacity', metric ? 1 : 0);
		const ramp: ExpressionSpecification =
			legend.length > 1
				? [
						'interpolate',
						['linear'],
						['number', ['get', 'value']],
						...legend.flatMap((step) => [step.value, step.color])
					]
				: ['literal', legend[0]?.color ?? missingMetricColor];
		map.setPaintProperty('route-metric', 'line-color', [
			'case',
			['==', ['get', 'value'], null],
			missingMetricColor,
			ramp
		]);
		// Accent changes can keep the same custom basemap.
		if ($appearance.mode) {
			map.setPaintProperty('route-line', 'line-color', appearanceColor('--accent-primary'));
			map.setPaintProperty('route-casing', 'line-color', appearanceColor('--map-casing'));
			map.setPaintProperty('endpoint-markers', 'circle-color', [
				'case',
				['get', 'start'],
				appearanceColor('--map-start'),
				appearanceColor('--map-finish')
			]);
		}
	});

	const activePoint = $derived.by(() => {
		if (replay) return replay.point;
		if (hoverT == null) return onHover ? null : inspected;
		const i = pointAtTime(route, hoverT);
		const p = route[i];
		const next = route[i + 1];
		return next && p && next.t - hoverT < hoverT - p.t ? next : (p ?? null);
	});

	$effect(() => {
		const p = activePoint;
		if (!mapLoaded || !map) return;
		if (!p) {
			marker?.remove();
			marker = undefined;
			return;
		}
		if (!marker) {
			const element = document.createElement('div');
			element.className = 'size-4 rounded-full border-2 border-white bg-accent-500 shadow-lg';
			element.style.pointerEvents = 'none';
			marker = new Marker({ element }).setLngLat([p.lon, p.lat]).addTo(map);
		} else {
			marker.setLngLat([p.lon, p.lat]);
		}
	});

	let lastCamera = 0;
	let following = false;
	$effect(() => {
		const p = activePoint;
		if (!mapLoaded || !map || !p || !followEnabled) {
			following = false;
			return;
		}
		const now = performance.now();
		if (following && replayPlaying && now - lastCamera < 200) return;
		map.easeTo({
			center: [p.lon, p.lat],
			zoom: following ? map.getZoom() : Math.max(14, map.getZoom()),
			duration: 260,
			easing: (n) => n
		});
		following = true;
		lastCamera = now;
	});
</script>

<div class="flex {heightClass} min-h-0 w-full flex-col gap-2">
	{#if !replay && route.length > 1}
		<div class="flex flex-wrap items-center gap-x-4 gap-y-2">
			<label class="flex items-center gap-2 text-sm text-zinc-300">
				{m.ride_map_layer()}
				<select
					class="min-h-10 rounded-md border border-zinc-700 bg-panel px-2"
					bind:value={selected}
				>
					<option value={null}>{m.ride_standard()}</option>
					{#each rideMetrics as item (item)}
						<option value={item} disabled={!legends?.[item].length}
							>{metricLabel[item]()}{legends?.[item].length ? '' : ` (${m.ride_no_data()})`}</option
						>
					{/each}
				</select>
			</label>
			{#if metric}
				<ul
					class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-400"
					aria-label={m.ride_map_legend()}
				>
					{#each legend as step (step.value)}
						<li class="flex items-center gap-1">
							<span class="h-2 w-3 rounded-sm" style:background-color={step.color}
							></span>{metricValue(metric, step.value)}
						</li>
					{/each}
					{#if hasGaps}
						<li class="flex items-center gap-1">
							<span class="h-2 w-3 rounded-sm" style:background-color={missingMetricColor}
							></span>{m.ride_no_data()}
						</li>
					{/if}
				</ul>
			{/if}
		</div>
	{/if}

	<div class="relative min-h-0 flex-1">
		<div
			bind:this={container}
			class="h-full w-full rounded-lg border border-zinc-800 bg-zinc-900"
			aria-label={m.ride_route_map()}
		></div>
		{#if mapUnavailable}
			<RoutePreview
				{coordinates}
				track={route}
				point={activePoint}
				{metric}
				disconnected={terrainEnabled !== undefined}
			/>
		{/if}
		{#if activePoint && !replay}
			<div
				class="pointer-events-none absolute inset-x-2 top-2 z-10 w-fit max-w-[calc(100%-1rem)] rounded-md border border-zinc-700 bg-panel px-2 py-1.5 text-xs text-zinc-200 shadow-lg sm:px-3 sm:py-2"
			>
				<div class="flex flex-wrap gap-x-2 gap-y-1 sm:gap-x-4">
					<span class="metric-value">{formatDuration(activePoint.t)}</span>
					{#if activePoint.distM != null}<span class="metric-value"
							>{formatDistance(activePoint.distM)}</span
						>{/if}
					{#each rideMetrics as item (item)}
						{@const value = activePoint[item]}
						{#if value != null}<span
								>{metricLabel[item]()}
								<span class="metric-value">{metricValue(item, value)}</span></span
							>{/if}
					{/each}
					{#if activePoint.cadence != null}<span
							>{m.ride_cadence()} <span class="metric-value">{activePoint.cadence} rpm</span></span
						>{/if}
				</div>
			</div>
		{/if}
	</div>
</div>

{#if coordinates.length === 0}
	<p class="mt-2 text-xs text-zinc-500">{m.detail_map_no_track()}</p>
{/if}
