<script lang="ts">
	import { memberURL } from '#lib/member.js';
	import { onDestroy, onMount, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages.js';
	import {
		heatmapColor,
		heatmapLegend,
		heatmapMetricLabel,
		heatmapMetrics,
		heatmapValue,
		heatmapViewport,
		missingHeatmapColor,
		type HeatmapMetric
	} from '#lib/performance-heatmap.js';
	import {
		Map as MapLibreMap,
		LngLatBounds,
		type GeoJSONSource,
		type MapLayerMouseEvent
	} from 'maplibre-gl';
	import type { FeatureCollection } from 'geojson';
	import 'maplibre-gl/dist/maplibre-gl.css';
	import { basemapStyle, basemapVariants, type BasemapVariant } from '#lib/basemap.js';
	import { appearance } from '#lib/appearance.js';
	import ActivityFilterBar from '#lib/components/ActivityFilterBar.svelte';
	import { ActivityFilter, type ActivityFacets } from '#lib/activity-filter.svelte.js';

	let { data: pageData } = $props();

	// Every GPS track the filter matches, drawn as translucent lines so the
	// routes you ride most burn brightest.
	type Heatmap = {
		matched: number;
		with_track: number;
		truncated?: boolean;
		facets: ActivityFacets;
		geojson: FeatureCollection;
	};
	const EMPTY: FeatureCollection = { type: 'FeatureCollection', features: [] };

	const filter = new ActivityFilter(page.url.searchParams);
	let data = $state<Heatmap | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let metric = $state<HeatmapMetric>('density');
	let individual = $state(false);
	const original = $derived(metric === 'density' && individual);
	const legend = $derived(heatmapLegend(metric));
	let viewport = $state('');
	let pending = $state(0);
	let hovered = $state<Record<string, unknown> | null>(null);
	let pinned = $state<Record<string, unknown> | null>(null);
	const inspected = $derived(pinned ?? hovered);

	// View settings (basemap, track color) are per-browser conveniences kept in
	// localStorage; blocked storage just means the defaults.
	function loadSetting(key: string): string | null {
		try {
			return localStorage.getItem(key);
		} catch {
			return null;
		}
	}
	function saveSetting(key: string, value: string) {
		try {
			localStorage.setItem(key, value);
		} catch {
			/* storage blocked */
		}
	}

	// Default to the app appearance and preserve any explicit basemap choice.
	const BASEMAP_KEY = 'cairn.heatmap.basemap';
	type BasemapPreference = 'system' | BasemapVariant;
	const BASEMAP_LABELS: Record<BasemapPreference, string> = {
		system: 'Auto',
		dark: 'Dark',
		light: 'Light',
		standard: 'Standard'
	};
	const variants = basemapVariants();
	const basemapOptions: BasemapPreference[] = ['system', ...variants];
	const savedBasemap = loadSetting(BASEMAP_KEY);
	let basemapPreference = $state<BasemapPreference>(
		variants.find((v) => v === savedBasemap) ?? 'system'
	);
	const basemap = $derived(basemapPreference === 'system' ? $appearance.mode : basemapPreference);

	function setBasemap(v: BasemapPreference) {
		basemapPreference = v;
		saveSetting(BASEMAP_KEY, v);
	}

	// Track color: a hue slider at the default's saturation and lightness, so
	// every choice stays as vivid as the original orange, which the default hue
	// maps to exactly.
	const HUE_KEY = 'cairn.heatmap.trackHue';
	const DEFAULT_HUE = 19;
	const savedHue = Number(loadSetting(HUE_KEY) ?? DEFAULT_HUE);
	let trackHue = $state(
		Number.isInteger(savedHue) && savedHue >= 0 && savedHue < 360 ? savedHue : DEFAULT_HUE
	);
	const trackLightness = $derived(basemap === 'dark' ? 60 : 39);
	const trackColor = $derived(`hsl(${trackHue}, 81%, ${trackLightness}%)`);
	const trackOpacity = $derived(basemap === 'dark' ? 0.45 : 0.72);

	function setTrackHue(h: number) {
		trackHue = h;
	}

	let container = $state<HTMLDivElement | null>(null);
	let map: MapLibreMap | null = null;
	let currentStyle: ReturnType<typeof basemapStyle>;
	let mapLoaded = false;
	let fitted = false;

	function bounds(fc: FeatureCollection): LngLatBounds | null {
		let b: LngLatBounds | null = null;
		for (const f of fc.features) {
			if (f.geometry.type !== 'LineString') continue;
			for (const c of f.geometry.coordinates) {
				const [lng, lat] = c;
				if (lng === undefined || lat === undefined) continue;
				const ll: [number, number] = [lng, lat];
				b = b ? b.extend(ll) : new LngLatBounds(ll, ll);
			}
		}
		return b;
	}

	function applyData() {
		if (!map || !mapLoaded) return;
		const fc = data?.geojson ?? EMPTY;
		(map.getSource('tracks') as GeoJSONSource | undefined)?.setData(fc);
		// Frame the tracks on the first result only; later filter changes keep
		// the viewport the user has set up.
		if (!fitted && fc.features.length > 0) fitTracks();
	}

	function fitTracks() {
		if (!map) return;
		const b = bounds(data?.geojson ?? EMPTY);
		if (!b) return;
		fitted = true;
		map.fitBounds(b, { padding: 40, duration: 0, maxZoom: 14 });
	}

	let abort: AbortController | null = null;
	async function load(
		params: URLSearchParams,
		tracks: boolean,
		area: string,
		subject: typeof pageData.member
	) {
		abort?.abort();
		const ctrl = new AbortController();
		abort = ctrl;
		loading = true;
		error = null;
		pending = 0;
		hovered = null;
		pinned = null;
		if (!tracks) for (const [key, value] of new URLSearchParams(area)) params.set(key, value);
		const endpoint = tracks ? '/api/activities/heatmap' : '/api/activities/heatmap/performance';
		try {
			while (!ctrl.signal.aborted) {
				const res = await fetch(memberURL(`${endpoint}?${params}`, subject), {
					signal: ctrl.signal
				});
				if (!res.ok) throw new Error(m.heatmap_load_failed());
				const result = await res.json();
				if (ctrl.signal.aborted) return;
				if (res.status === 202) {
					pending = result.pending;
					await new Promise((resolve) => setTimeout(resolve, 350));
					continue;
				}
				pending = 0;
				data = result as Heatmap;
				applyData();
				return;
			}
		} catch {
			if (!ctrl.signal.aborted) {
				error = m.heatmap_load_failed();
				data = null;
				applyData();
			}
		} finally {
			if (!ctrl.signal.aborted) loading = false;
		}
	}

	function fitAll() {
		if (original || !viewport) {
			fitTracks();
		} else {
			fitted = false;
			viewport = '';
		}
	}

	// Chip/select edits re-query at once; dates and ranges after a debounce.
	let debounced = $state(0);
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;
	let debounceInit = false;
	$effect(() => {
		void filter.textKey;
		if (!debounceInit) {
			debounceInit = true;
			return;
		}
		clearTimeout(debounceTimer);
		debounceTimer = setTimeout(() => (debounced += 1), 350);
	});
	$effect(() => {
		void filter.immediateKey;
		void debounced;
		const tracks = original;
		const area = tracks ? '' : viewport;
		load(
			untrack(() => filter.params()),
			tracks,
			area,
			pageData.member
		);
	});

	// Tracks source + layers. Runs on every style load: setStyle() drops
	// everything the new style doesn't define.
	function addTracks() {
		if (!map) return;
		map.addSource('tracks', { type: 'geojson', data: EMPTY });
		// Two passes: a wide, faint glow that accumulates where tracks overlap,
		// and a thin core so single passes still read as a line.
		map.addLayer({
			id: 'tracks-glow',
			type: 'line',
			source: 'tracks',
			layout: { 'line-cap': 'round', 'line-join': 'round' },
			paint: { 'line-color': trackColor, 'line-width': 7, 'line-opacity': 0.07, 'line-blur': 3 }
		});
		map.addLayer({
			id: 'tracks-line',
			type: 'line',
			source: 'tracks',
			layout: { 'line-cap': 'round', 'line-join': 'round' },
			paint: { 'line-color': heatmapColor(metric), 'line-width': 3.5, 'line-opacity': 0.9 }
		});
		mapLoaded = true;
		applyPaint();
		applyData();
	}

	onMount(() => {
		if (!container) return;
		currentStyle = basemapStyle(basemap);
		map = new MapLibreMap({
			container,
			style: currentStyle,
			center: [10, 50],
			zoom: 4,
			attributionControl: { compact: true }
		});
		map.on('style.load', addTracks);
		// Layer-bound listeners resolve the layer per event, so they survive
		// style swaps.
		map.on('moveend', () => {
			if (!map) return;
			const bounds = map.getBounds();
			viewport = heatmapViewport(
				map.getZoom(),
				bounds.getWest(),
				bounds.getSouth(),
				bounds.getEast(),
				bounds.getNorth()
			);
		});
		map.on('mousemove', 'tracks-line', (event) => {
			if (map) map.getCanvas().style.cursor = 'pointer';
			if (!original) hovered = featureValues(event);
		});
		map.on('mouseleave', 'tracks-line', () => {
			if (map) map.getCanvas().style.cursor = '';
			hovered = null;
		});
		map.on('click', 'tracks-line', (event) => {
			const values = featureValues(event);
			if (original && typeof values?.id === 'string') void goto(`/activities/${values.id}`);
			else pinned = values;
		});
	});
	$effect(() => {
		const next = basemapStyle(basemap);
		if (!map || next === currentStyle) return;
		currentStyle = next;
		mapLoaded = false;
		// Re-add the track overlays on style.load after switching a basemap.
		map.setStyle(next, { diff: false });
	});
	function featureValues(event: MapLayerMouseEvent): Record<string, unknown> | null {
		return event.features?.[0]?.properties ?? null;
	}

	function applyPaint() {
		const raw = original;
		const color = raw ? trackColor : heatmapColor(metric);
		const opacity = raw ? trackOpacity : 0.94;
		if (!map?.getLayer('tracks-line')) return;
		map.setLayoutProperty('tracks-glow', 'visibility', raw ? 'visible' : 'none');
		map.setPaintProperty('tracks-glow', 'line-color', trackColor);
		map.setPaintProperty('tracks-line', 'line-color', color);
		map.setPaintProperty('tracks-line', 'line-width', raw ? 1.8 : 3.5);
		map.setPaintProperty('tracks-line', 'line-opacity', opacity);
	}

	$effect(applyPaint);

	onDestroy(() => {
		abort?.abort();
		map?.remove();
		map = null;
	});
</script>

<section class="space-y-6">
	<header class="flex items-center justify-between gap-4 max-md:flex-wrap">
		<div>
			<h1 class="text-3xl font-semibold tracking-tight max-md:text-2xl">{m.member_heatmap()}</h1>
			{#if !filter.from && !filter.to}<p class="mt-1 text-sm text-quiet">
					{m.heatmap_all_time()}
				</p>{/if}
			{#if data}
				<p class="mt-1 text-sm text-zinc-400">
					<span class="font-medium text-zinc-200">{data.with_track}</span> tracks from {data.matched}
					{#if filter.active}matching{/if} activities{#if data.truncated}
						<span class="text-amber-400">(newest {data.with_track} shown)</span>{/if}
				</p>
			{/if}
		</div>
		<div class="flex flex-wrap items-center gap-2">
			<label class="flex items-center gap-2 text-sm">
				{m.heatmap_layer()}
				<select bind:value={metric} class="min-h-10 rounded border border-zinc-700 bg-panel px-2">
					{#each heatmapMetrics as layer (layer)}
						<option value={layer}>{heatmapMetricLabel(layer)}</option>
					{/each}
				</select>
			</label>
			{#if variants.length > 1}
				<div
					role="group"
					aria-label="Basemap"
					class="flex overflow-hidden rounded border border-zinc-700 text-xs"
				>
					{#each basemapOptions as v (v)}
						<button
							type="button"
							aria-pressed={basemapPreference === v}
							onclick={() => setBasemap(v)}
							class="px-2.5 py-1.5 transition-colors"
							class:bg-zinc-800={basemapPreference === v}
							class:text-accent-300={basemapPreference === v}
							class:text-zinc-400={basemapPreference !== v}
							class:hover:text-zinc-100={basemapPreference !== v}
						>
							{BASEMAP_LABELS[v]}
						</button>
					{/each}
				</div>
			{/if}
			{#if metric === 'density'}
				<label class="flex min-h-10 items-center gap-2 text-xs">
					<input type="checkbox" bind:checked={individual} />
					{m.heatmap_original_tracks()}
				</label>
			{/if}
			{#if original}
				<div class="flex items-center gap-1">
					<input
						type="range"
						min="0"
						max="359"
						step="1"
						class="hue w-28"
						aria-label="Track color"
						title="Track color"
						style:--thumb={trackColor}
						value={trackHue}
						oninput={(e) => setTrackHue(e.currentTarget.valueAsNumber)}
						onchange={() => saveSetting(HUE_KEY, String(trackHue))}
					/>
					<button
						type="button"
						class="rounded p-1 text-zinc-500 hover:text-zinc-200"
						class:invisible={trackHue === DEFAULT_HUE}
						disabled={trackHue === DEFAULT_HUE}
						aria-label="Reset track color"
						title="Reset track color"
						onclick={() => {
							setTrackHue(DEFAULT_HUE);
							saveSetting(HUE_KEY, String(DEFAULT_HUE));
						}}
					>
						<svg
							class="h-3.5 w-3.5"
							viewBox="0 0 24 24"
							fill="none"
							stroke="currentColor"
							stroke-width="2"
							stroke-linecap="round"
							stroke-linejoin="round"
						>
							<path d="M3 12a9 9 0 1 0 3-6.7L3 8 M3 3v5h5" />
						</svg>
					</button>
				</div>
			{/if}
			<button
				type="button"
				onclick={fitAll}
				disabled={!data || data.with_track === 0}
				class="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-300 hover:border-accent-500 hover:text-accent-300 disabled:opacity-50"
			>
				{m.heatmap_fit_tracks()}
			</button>
		</div>
	</header>

	<ActivityFilterBar
		{filter}
		facets={data?.facets ?? null}
		showSort={false}
		member={pageData.member?.username}
	/>

	{#if error}
		<div class="rounded border border-red-700/50 bg-red-950/30 px-3 py-2 text-xs text-red-300">
			{error}
		</div>
	{/if}

	{#if !original}
		<div
			class="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs"
			aria-label={m.heatmap_legend()}
		>
			{#each legend as tick (tick.value)}
				<span class="inline-flex items-center gap-1.5">
					<span class="h-2.5 w-5 rounded-sm" style:background={tick.color}></span>{tick.label}
				</span>
			{/each}
			{#if metric !== 'density' && metric !== 'speed'}
				<span class="inline-flex items-center gap-1.5">
					<span class="h-2.5 w-5 rounded-sm" style:background={missingHeatmapColor}
					></span>{m.heatmap_insufficient()}
				</span>
			{/if}
		</div>
	{/if}

	<div class="relative">
		<div
			bind:this={container}
			class="h-[70vh] min-h-96 w-full overflow-hidden rounded-lg border border-zinc-800 bg-zinc-900"
		></div>
		{#if loading}
			<div
				class="pointer-events-none absolute top-3 left-3 rounded bg-zinc-900/80 px-2 py-1 text-xs text-zinc-300"
			>
				{pending > 0 ? m.heatmap_preparing({ count: pending }) : m.heatmap_loading()}
			</div>
		{:else if data && data.geojson.features.length === 0}
			<div class="pointer-events-none absolute inset-0 flex items-center justify-center">
				<p class="rounded bg-zinc-900/80 px-4 py-2 text-sm text-zinc-400">
					{data.with_track > 0
						? m.heatmap_no_viewport()
						: filter.active
							? 'No GPS tracks match this filter.'
							: 'No GPS tracks yet.'}
				</p>
			</div>
		{/if}
	</div>
	{#if original}
		<p class="text-xs text-zinc-500">{m.heatmap_open_activity()}</p>
	{:else}
		<div class="min-h-16 rounded-lg border border-zinc-800 bg-panel px-3 py-2 text-sm">
			{#if inspected}
				<dl class="flex flex-wrap gap-x-5 gap-y-1">
					{#each heatmapMetrics as item (item)}
						{@const value = inspected[item]}
						<div>
							<dt class="text-xs text-quiet">
								{item === 'density' ? m.heatmap_passages() : heatmapMetricLabel(item)}
							</dt>
							<dd class="font-medium tabular-nums">
								{heatmapValue(item, typeof value === 'number' ? value : null)}
							</dd>
						</div>
					{/each}
				</dl>
			{:else}
				<p class="text-quiet">{m.heatmap_inspect()}</p>
			{/if}
		</div>
	{/if}
</section>

<style>
	/* Hue slider: the track shows the spectrum, the thumb the chosen color. */
	.hue {
		appearance: none;
		height: 6px;
		border-radius: 9999px;
		cursor: pointer;
		background: linear-gradient(
			to right,
			hsl(0, 81%, 60%),
			hsl(60, 81%, 60%),
			hsl(120, 81%, 60%),
			hsl(180, 81%, 60%),
			hsl(240, 81%, 60%),
			hsl(300, 81%, 60%),
			hsl(359, 81%, 60%)
		);
	}
	.hue::-webkit-slider-thumb {
		appearance: none;
		width: 14px;
		height: 14px;
		border-radius: 9999px;
		background: var(--thumb);
		border: 2px solid var(--surface-panel);
		box-shadow: 0 0 0 1px rgb(0 0 0 / 0.5);
	}
	.hue::-moz-range-track {
		background: transparent;
	}
	.hue::-moz-range-thumb {
		width: 10px;
		height: 10px;
		border-radius: 9999px;
		background: var(--thumb);
		border: 2px solid var(--surface-panel);
		box-shadow: 0 0 0 1px rgb(0 0 0 / 0.5);
	}
	.hue:focus-visible {
		outline: 2px solid var(--accent-primary);
		outline-offset: 4px;
	}
</style>
