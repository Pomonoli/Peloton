<script lang="ts">
	import { onMount } from 'svelte';
	import {
		type Map as MapLibreMap,
		type FilterSpecification,
		type GeoJSONSource
	} from 'maplibre-gl';
	import 'maplibre-gl/dist/maplibre-gl.css';
	import { appearance, appearanceColor } from '#lib/appearance.js';
	import { basemapStyle, createMap } from '#lib/basemap.js';
	import { addMapNavigation } from '#lib/discovery-map.js';
	import { formatDuration, formatMetricDistance, formatTimeAndSpeed } from '#lib/format.js';
	import MetricReadout from './MetricReadout.svelte';
	import SegmentMetadata from './SegmentMetadata.svelte';
	import { memberURL, type MemberView } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		catalogFeatures,
		catalogViewport,
		fetchSegmentCatalog,
		type CatalogSegment,
		type CatalogSort,
		type MapCamera
	} from '#lib/segment-catalog.js';

	let {
		name,
		mine,
		order,
		member,
		center,
		camera,
		onCamera
	}: {
		name: string;
		mine: boolean;
		order: CatalogSort[];
		member: MemberView | null;
		center: [number, number] | undefined;
		camera: MapCamera | undefined;
		onCamera: (value: MapCamera) => void;
	} = $props();

	const minimumSegmentZoom = 6;
	let container: HTMLDivElement;
	let map: MapLibreMap | undefined;
	let style: ReturnType<typeof basemapStyle>;
	let ready = $state(false);
	let unavailable = $state(false);
	let viewport = $state<ReturnType<typeof catalogViewport>>(null);
	let viewportKey = '';
	let loading = $state(false);
	let failed = $state(false);
	let hasMore = $state(false);
	let rows = $state.raw<CatalogSegment[]>([]);
	let selectedId = $state('');
	let retry = $state(0);
	const features = $derived(catalogFeatures(rows));
	const selected = $derived(rows.find((row) => row.id === selectedId));
	let placed = false;
	let waitForStyle: (() => void) | undefined;

	function updateViewport() {
		if (!map || !ready) return;
		const zoom = map.getZoom();
		const midpoint = map.getCenter();
		onCamera({ center: [midpoint.lng, midpoint.lat], zoom });
		const b = map.getBounds();
		const next =
			zoom < minimumSegmentZoom
				? null
				: catalogViewport(b.getWest(), b.getSouth(), b.getEast(), b.getNorth());
		const key = next ? Object.values(next).join(',') : '';
		if (key === viewportKey) return;
		viewportKey = key;
		viewport = next;
	}

	onMount(() => {
		style = basemapStyle($appearance.mode);
		placed = !!(camera || center);
		try {
			map = createMap({
				container,
				style,
				center: camera?.center ?? center ?? [0, 25],
				zoom: camera?.zoom ?? (center ? 11 : 2),
				trackResize: false,
				dragRotate: false,
				touchPitch: false
			});
		} catch {
			unavailable = true;
			return;
		}

		addMapNavigation(map);
		let styleTimer = 0;
		waitForStyle = () => {
			clearTimeout(styleTimer);
			ready = false;
			unavailable = false;
			styleTimer = window.setTimeout(() => {
				unavailable = true;
			}, 8000);
		};
		waitForStyle();
		map.on('style.load', () => {
			if (!map) return;
			clearTimeout(styleTimer);
			unavailable = false;
			map.addSource('catalog-routes', { type: 'geojson', data: features });
			for (const [id, color, width] of [
				['catalog-casing', '--map-casing', 6],
				['catalog-route', '--plot-cadence', 3],
				['catalog-highlight-casing', '--map-casing', 9],
				['catalog-highlight', '--accent-primary', 6]
			] as const) {
				map.addLayer({
					id,
					type: 'line',
					source: 'catalog-routes',
					layout: { 'line-cap': 'round', 'line-join': 'round' },
					paint: { 'line-color': appearanceColor(color), 'line-width': width },
					...(id.startsWith('catalog-highlight')
						? { filter: ['==', ['get', 'id'], selectedId] }
						: {})
				});
			}
			ready = true;
			updateViewport();
		});
		map.on('movestart', (event) => {
			if (event.originalEvent) placed = true;
		});
		map.on('moveend', updateViewport);
		map.on('webglcontextlost', () => {
			ready = false;
			unavailable = true;
		});
		map.on('mousemove', 'catalog-route', () => {
			if (map) map.getCanvas().style.cursor = 'pointer';
		});
		map.on('mouseleave', 'catalog-route', () => {
			if (map) map.getCanvas().style.cursor = '';
		});
		map.on('click', (event) => {
			if (!map || !ready) return;
			const { x, y } = event.point;
			const hits = map.queryRenderedFeatures(
				[
					[x - 8, y - 8],
					[x + 8, y + 8]
				],
				{
					layers: ['catalog-highlight', 'catalog-route']
				}
			);
			const ids = [
				...new Set(
					hits.map((hit) => hit.properties.id).filter((id): id is string => typeof id === 'string')
				)
			];
			// Repeated taps can reach every route where they overlap.
			const next = ids[(ids.indexOf(selectedId) + 1) % ids.length];
			selectedId = next ?? '';
		});

		let resizeFrame = 0;
		const observer = new ResizeObserver(() => {
			cancelAnimationFrame(resizeFrame);
			resizeFrame = requestAnimationFrame(() => {
				map?.resize();
				updateViewport();
			});
		});
		observer.observe(container);
		return () => {
			clearTimeout(styleTimer);
			cancelAnimationFrame(resizeFrame);
			observer.disconnect();
			map?.remove();
			map = undefined;
		};
	});

	$effect(() => {
		const next = basemapStyle($appearance.mode);
		if (!map || style === next) return;
		style = next;
		waitForStyle?.();
		map.setStyle(next, { diff: false });
	});

	$effect(() => {
		if (!ready || !map || !center || placed) return;
		placed = true;
		map.jumpTo({ center, zoom: 11 });
	});

	let lastFilters = '';
	$effect(() => {
		const bounds = viewport;
		const query = name;
		const onlyMine = mine;
		const criteria = order;
		const subject = member?.username;
		void retry;
		if (!bounds || !ready || unavailable) {
			if (!bounds) {
				rows = [];
				selectedId = '';
				hasMore = false;
			}
			loading = false;
			failed = false;
			return;
		}
		const filters = JSON.stringify([query, onlyMine, criteria, subject]);
		if (lastFilters !== filters) {
			lastFilters = filters;
			rows = [];
			selectedId = '';
			hasMore = false;
		}
		loading = true;
		failed = false;
		const request = new AbortController();
		const timer = setTimeout(async () => {
			try {
				const page = await fetchSegmentCatalog(
					{
						...bounds,
						limit: '150',
						...(query ? { q: query } : {}),
						...(onlyMine ? { mine: 'true' } : {}),
						...(subject ? { member: subject } : {})
					},
					criteria,
					request.signal
				);
				if (request.signal.aborted) return;
				rows = page.segments;
				hasMore = page.has_more;
				if (!rows.some((row) => row.id === selectedId)) selectedId = '';
			} catch {
				if (!request.signal.aborted) {
					failed = true;
					rows = [];
					selectedId = '';
					hasMore = false;
				}
			} finally {
				if (!request.signal.aborted) loading = false;
			}
		}, 150);
		return () => {
			clearTimeout(timer);
			request.abort();
		};
	});

	$effect(() => {
		if (!ready || !map) return;
		map.getSource<GeoJSONSource>('catalog-routes')?.setData(features);
	});

	$effect(() => {
		if (!ready || !map) return;
		const filter: FilterSpecification = ['==', ['get', 'id'], selectedId];
		map.setFilter('catalog-highlight', filter);
		map.setFilter('catalog-highlight-casing', filter);
		if ($appearance.mode) {
			map.setPaintProperty('catalog-casing', 'line-color', appearanceColor('--map-casing'));
			map.setPaintProperty('catalog-route', 'line-color', appearanceColor('--plot-cadence'));
			map.setPaintProperty(
				'catalog-highlight-casing',
				'line-color',
				appearanceColor('--map-casing')
			);
			map.setPaintProperty('catalog-highlight', 'line-color', appearanceColor('--accent-primary'));
		}
	});
</script>

<div class="space-y-3">
	<div class="relative h-[min(62dvh,38rem)] min-h-72 overflow-hidden surface">
		<div bind:this={container} class="h-full w-full" aria-label={m.segment_map_label()}></div>
		{#if unavailable}
			<p
				class="absolute inset-0 grid place-items-center bg-panel p-4 text-center text-sm text-quiet"
				role="status"
			>
				{m.segment_map_unavailable()}
			</p>
		{:else}
			<div
				id="selected-catalog-segment"
				class="pointer-events-none absolute inset-x-2 bottom-7 sm:left-3 sm:right-auto sm:w-80"
				aria-live="polite"
			>
				{#if selected}
					<section
						class="pointer-events-auto space-y-2 rounded-lg border border-zinc-700 bg-panel p-3 shadow-lg"
						aria-label={selected.name}
					>
						<div class="flex items-start justify-between gap-2">
							<h2 class="truncate font-medium" title={selected.name}>{selected.name}</h2>
							<button
								type="button"
								class="-mt-1 flex size-8 shrink-0 items-center justify-center rounded text-zinc-400 hover:bg-zinc-800"
								aria-label={m.segment_map_clear_selection()}
								onclick={() => (selectedId = '')}>×</button
							>
						</div>
						<dl class="grid grid-cols-2 gap-2 text-xs sm:text-sm">
							<div>
								<dt class="text-quiet">{m.detail_stat_distance()}</dt>
								<dd class="metric-value">{formatMetricDistance(selected.distance_m)}</dd>
							</div>
							<div class="text-right">
								<dt class="text-quiet">{m.segment_leaderboard_time()}</dt>
								<dd>
									<MetricReadout
										value={formatTimeAndSpeed(
											formatDuration(selected.best_elapsed_s),
											selected.average_speed_kmh
										)}
									/>
								</dd>
							</div>
						</dl>
						<div class="text-xs text-quiet">
							<SegmentMetadata
								holder={selected.local_kom_holder ?? ''}
								riders={selected.member_count}
								attempts={selected.my_attempts}
								attemptsLabel={member ? m.segment_attempts() : m.segment_my_attempts()}
							/>
						</div>
						<a
							href={memberURL(`/segment/${selected.id}`, member)}
							class="inline-flex min-h-8 items-center text-sm text-accent-300 hover:underline"
							>{m.segment_map_details()}</a
						>
					</section>
				{/if}
			</div>
		{/if}
	</div>

	<div class="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-1">
		{#if !unavailable}
			{#if !ready || (loading && !features.features.length)}
				<p class="text-sm text-quiet" role="status">{m.community_loading()}</p>
			{:else if failed}
				<p class="text-sm text-red-300" role="status">{m.segment_catalog_failed()}</p>
				<button
					type="button"
					class="min-h-10 text-sm text-accent-300 hover:underline"
					onclick={() => (retry += 1)}>{m.community_retry()}</button
				>
			{:else if !viewport}
				<p class="text-sm text-quiet" role="status">{m.segment_map_zoom_in()}</p>
			{:else if !features.features.length}
				<p class="text-sm text-quiet" role="status">{m.segment_map_empty()}</p>
			{/if}

			{#if features.features.length}
				<label class="flex flex-wrap items-center gap-2 text-sm text-zinc-300">
					{m.segment_map_select()}
					<select
						bind:value={selectedId}
						aria-controls="selected-catalog-segment"
						class="min-h-10 max-w-full rounded-md border border-zinc-700 bg-panel px-2"
					>
						<option value="">{m.segment_map_select_placeholder()}</option>
						{#each rows as row (row.id)}
							{#if row.polyline}<option value={row.id}>{row.name}</option>{/if}
						{/each}
					</select>
				</label>
			{/if}
		{/if}
	</div>
	{#if !unavailable && hasMore}
		<p class="text-sm text-quiet" role="status">{m.segment_map_more()}</p>
	{/if}
</div>
