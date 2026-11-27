<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import {
		LngLatBounds,
		type Map as MapLibreMap,
		type ExpressionSpecification,
		type StyleSpecification
	} from 'maplibre-gl';
	import type { FeatureCollection, Position } from 'geojson';
	import 'maplibre-gl/dist/maplibre-gl.css';
	import { appearance, appearanceColor } from '#lib/appearance.js';
	import { basemapStyle, createMap } from '#lib/basemap.js';
	import {
		addDiscoveryOverlay,
		addMapNavigation,
		bindMapSelection,
		emptyFeatures,
		mapLongitude,
		nearestMapLine,
		setMapFeatures,
		showDiscoveryLocation,
		showDiscoveryTrack
	} from '#lib/discovery-map.js';
	import { intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		mapLines,
		recordedRoutePath,
		recordedRoutePlannerPath,
		routeData,
		type RecordedRoute,
		type RouteDetail
	} from '#lib/recorded-routes.js';
	import type { MapCamera } from '#lib/segment-catalog.js';
	import MapResultsHeader from './MapResultsHeader.svelte';
	import RecordedRouteCard from './RecordedRouteCard.svelte';
	import RecordedRouteDetail from './RecordedRouteDetail.svelte';

	let {
		filters,
		active,
		camera,
		onCamera,
		onreset
	}: {
		filters: string;
		active: boolean;
		camera: MapCamera | undefined;
		onCamera: (camera: MapCamera) => void;
		onreset: () => void;
	} = $props();
	type RouteMapData = { routes: RecordedRoute[]; geojson: FeatureCollection };
	const layer = 'favorite-routes-line';
	const source = 'favorite-routes';
	const steps = [1, 2, 3, 5] as const;
	const colors = $derived(
		$appearance.mode === 'dark'
			? (['#c49063', '#ed9447', '#ffac43', '#ffe653'] as const)
			: (['#a77866', '#ca6038', '#b53a28', '#8a1236'] as const)
	);
	const densityColor: ExpressionSpecification = $derived([
		'step',
		['get', 'density'],
		colors[0],
		steps[1],
		colors[1],
		steps[2],
		colors[2],
		steps[3],
		colors[3]
	]);
	let container: HTMLDivElement;
	let map: MapLibreMap | undefined;
	let style: string | StyleSpecification;
	let ready = $state(false);
	let unavailable = $state(false);
	let fitted = false;
	let styleTimer = 0;

	function waitForStyle() {
		clearTimeout(styleTimer);
		ready = false;
		unavailable = false;
		styleTimer = window.setTimeout(() => (unavailable = true), 8000);
	}
	let busy = $state(true);
	let failed = $state(false);
	let retry = $state(0);
	let data = $state.raw<RouteMapData>();
	let location = $state<Position | null>(null);
	let indexes = $state<number[]>([]);
	let selected = $state<RecordedRoute>();
	let hovered = $state<RecordedRoute>();
	const highlighted = $derived(hovered ?? selected);
	let visible = $state(24);
	let detailsId = $state('');
	let detail = $state.raw<RouteDetail>();
	let detailFailed = $state(false);
	let detailRetry = $state(0);
	let results = $derived(indexes.flatMap((index) => data?.routes[index] ?? []));
	const count = $derived(new Intl.NumberFormat(intlLocale()).format(results.length));

	function clearSelection() {
		location = null;
		indexes = [];
		selected = hovered = undefined;
		detailsId = '';
		visible = 24;
	}

	function fitRoutes() {
		if (!map || !data?.routes.length) return;
		const bounds = new LngLatBounds();
		let origin: number | undefined;
		for (const route of data.routes) {
			for (const line of route.track) {
				for (const p of line) {
					origin ??= p.lon;
					bounds.extend([mapLongitude(p.lon, origin), p.lat]);
				}
			}
		}
		if (!bounds.isEmpty()) {
			fitted = true;
			map.fitBounds(bounds, { padding: 44, duration: 0, maxZoom: 14 });
		}
	}

	$effect(() => {
		void retry;
		const query = filters;
		const ctrl = new AbortController();
		clearSelection();
		data = undefined;
		busy = true;
		failed = false;
		const timeout = window.setTimeout(async () => {
			try {
				const result = await routeData<RouteMapData>('/api/routes/map?' + query, ctrl.signal);
				if (!ctrl.signal.aborted) data = result;
			} catch {
				if (!ctrl.signal.aborted) failed = true;
			} finally {
				if (!ctrl.signal.aborted) busy = false;
			}
		}, 170);
		return () => {
			window.clearTimeout(timeout);
			ctrl.abort();
		};
	});

	$effect(() => {
		void detailRetry;
		const id = detailsId;
		detail = undefined;
		detailFailed = false;
		if (!id) return;
		const ctrl = new AbortController();
		void routeData<RouteDetail>(recordedRoutePath(id), ctrl.signal)
			.then((result) => {
				if (!ctrl.signal.aborted) detail = result;
			})
			.catch(() => {
				if (!ctrl.signal.aborted) detailFailed = true;
			});
		return () => ctrl.abort();
	});

	function selectAt(point: Parameters<typeof nearestMapLine>[1]) {
		if (!map || !ready || busy || !map.isSourceLoaded(source)) return;
		const hit = nearestMapLine(map, point, layer);
		if (!hit) return;
		// The tile index encodes array-valued GeoJSON properties as JSON strings.
		const matches: number[] | string = hit.feature.properties.routes;
		const routes: number[] = typeof matches === 'string' ? JSON.parse(matches) : matches;
		if (!Array.isArray(routes)) return;
		clearSelection();
		location = hit.location.toArray();
		indexes = routes;
	}

	onMount(() => {
		style = basemapStyle($appearance.mode);
		fitted = !!camera;
		try {
			map = createMap({
				container,
				style,
				center: camera?.center ?? [0, 25],
				zoom: camera?.zoom ?? 2,
				trackResize: false,
				dragRotate: false,
				touchPitch: false
			});
		} catch {
			unavailable = true;
			return;
		}
		addMapNavigation(map);
		waitForStyle();
		map.on('style.load', () => {
			if (!map) return;
			clearTimeout(styleTimer);
			unavailable = false;
			map.addSource(source, { type: 'geojson', data: emptyFeatures });
			map.addLayer({
				id: 'favorite-routes-casing',
				type: 'line',
				source,
				layout: { 'line-cap': 'round', 'line-join': 'round' },
				paint: { 'line-color': appearanceColor('--map-casing'), 'line-width': 6 }
			});
			map.addLayer({
				id: layer,
				type: 'line',
				source,
				layout: { 'line-cap': 'round', 'line-join': 'round', 'line-sort-key': ['get', 'density'] },
				paint: {
					'line-color': densityColor,
					'line-width': ['interpolate', ['linear'], ['zoom'], 4, 1.5, 12, 3.5]
				}
			});
			addDiscoveryOverlay(map, $appearance.mode);
			ready = true;
		});
		map.on('movestart', (event) => {
			if (event.originalEvent) fitted = true;
		});
		map.on('moveend', () => {
			if (map) onCamera({ center: map.getCenter().toArray(), zoom: map.getZoom() });
		});
		map.on('webglcontextlost', () => {
			ready = false;
			unavailable = true;
		});
		map.on('webglcontextrestored', () => (unavailable = false));
		map.on('mousemove', layer, () => {
			if (map) map.getCanvas().style.cursor = 'pointer';
		});
		map.on('mouseleave', layer, () => {
			if (map) map.getCanvas().style.cursor = '';
		});
		bindMapSelection(map, selectAt);

		const observer = new ResizeObserver(() => map?.resize());
		observer.observe(container);
		return () => {
			clearTimeout(styleTimer);
			observer.disconnect();
			map?.remove();
			map = undefined;
		};
	});

	$effect(() => {
		const next = basemapStyle($appearance.mode);
		if (!map || next === style) return;
		style = next;
		waitForStyle();
		map.setStyle(next, { diff: false });
	});

	$effect(() => {
		if (!ready) return;
		setMapFeatures(map, source, data?.geojson ?? emptyFeatures);
		if (!fitted && data) untrack(fitRoutes);
	});

	$effect(() => {
		if (!ready || !map) return;
		map.setPaintProperty(layer, 'line-color', densityColor);
	});

	$effect(() => {
		if (!ready) return;
		showDiscoveryLocation(map, location);
		showDiscoveryTrack(map, highlighted ? mapLines(highlighted.track) : []);
	});
</script>

<div class="grid min-w-0 gap-3 xl:grid-cols-[minmax(0,1.65fr)_minmax(20rem,1fr)]">
	<div class="min-w-0 space-y-2">
		<div class="surface relative h-[min(62dvh,38rem)] min-h-72 overflow-hidden">
			<div bind:this={container} class="h-full w-full" aria-label={m.routes_map_label()}></div>
			{#if unavailable}
				<p class="absolute top-3 left-3 surface px-3 py-2 text-sm" role="alert">
					{m.routes_map_load_failed()}
				</p>
			{/if}
		</div>
		<div class="flex flex-wrap items-center justify-between gap-2 text-xs text-quiet">
			<div
				class="flex flex-wrap items-center gap-x-3 gap-y-1"
				role="group"
				aria-label={m.routes_map_density()}
			>
				<span>{m.routes_map_density()}</span>
				{#each steps as step, i (step)}
					<span class="inline-flex items-center gap-1">
						<span class="h-1.5 w-4 rounded-full" style:background={colors[i]}></span>
						{step}{i === 2 ? '–4' : i === steps.length - 1 ? '+' : ''}
					</span>
				{/each}
			</div>
			<button
				type="button"
				class="min-h-10 rounded border border-zinc-700 px-3 hover:border-accent-500 hover:text-accent-300 disabled:opacity-50"
				disabled={!ready || !data?.geojson.features.length}
				onclick={fitRoutes}>{m.heatmap_fit_tracks()}</button
			>
		</div>
	</div>

	<section
		class="surface min-w-0 space-y-3 p-3 xl:max-h-[min(62dvh,38rem)] xl:overflow-y-auto"
		aria-busy={busy}
	>
		{#if busy}
			<p role="status" class="text-sm text-quiet">{m.routes_loading()}</p>
		{:else if failed}
			<p role="alert" class="text-sm">
				{m.routes_load_failed()}
				<button class="ml-1 min-h-10 text-accent-300 underline" onclick={() => retry++}
					>{m.routes_retry()}</button
				>
			</p>
		{:else if !data?.routes.length}
			<p role="status" class="text-sm text-quiet">
				{active ? m.routes_empty() : m.routes_empty_catalog()}
				{#if active}
					<button class="ml-1 min-h-10 text-accent-300 underline" onclick={onreset}
						>{m.mobile_filters_reset()}</button
					>
				{/if}
			</p>
		{:else if !data.geojson.features.length}
			<p role="status" class="text-sm text-quiet">{m.routes_map_no_tracks()}</p>
		{:else if !location}
			<p class="text-sm text-quiet">{m.routes_map_inspect()}</p>
		{:else}
			<MapResultsHeader
				title={results.length === 1 ? m.routes_map_match_one() : m.routes_map_matches({ count })}
				onclear={clearSelection}
			/>
			<ul class="space-y-3">
				{#each results.slice(0, visible) as route (route.id)}
					<li class="space-y-1.5">
						<RecordedRouteCard
							{route}
							selected={selected?.id === route.id}
							onselect={() => {
								selected = selected?.id === route.id ? undefined : route;
								if (detailsId !== selected?.id) detailsId = '';
							}}
							onhover={(over) => (hovered = over ? route : undefined)}
						/>
						<div class="flex flex-wrap gap-x-4 px-1 text-sm">
							<button
								type="button"
								class="min-h-10 text-accent-300 hover:underline"
								aria-expanded={detailsId === route.id}
								onclick={() => {
									selected = route;
									detailsId = detailsId === route.id ? '' : route.id;
								}}>{m.routes_detail_label()}</button
							>
							<a
								class="inline-flex min-h-10 items-center text-accent-300 hover:underline"
								href={recordedRoutePlannerPath(route.id)}
							>
								{m.routes_open_planner()}
							</a>
						</div>
						{#if detailsId === route.id}
							<div class="surface p-3">
								{#if detail}
									<RecordedRouteDetail route={detail} showTitle={false} />
								{:else if detailFailed}
									<p role="alert" class="text-sm">
										{m.routes_detail_failed()}
										<button
											class="ml-1 min-h-10 text-accent-300 underline"
											onclick={() => detailRetry++}>{m.routes_retry()}</button
										>
									</p>
								{:else}
									<p role="status" class="text-sm text-quiet">{m.routes_detail_loading()}</p>
								{/if}
							</div>
						{/if}
					</li>
				{/each}
			</ul>
			{#if visible < results.length}
				<button
					type="button"
					class="min-h-11 w-full surface px-3 text-sm font-semibold text-accent-300"
					onclick={() => (visible += 24)}
				>
					{m.routes_more_routes()}
				</button>
			{/if}
		{/if}
	</section>
</div>
