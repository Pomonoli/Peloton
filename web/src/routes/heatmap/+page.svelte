<script lang="ts">
	import FilterSheet from '#lib/components/FilterSheet.svelte';
	import { memberURL } from '#lib/member.js';
	import { onDestroy, onMount, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages.js';
	import {
		adaptiveHeatmapScale,
		fixedHeatmapScale,
		heatmapColor,
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

	// Both heatmap endpoints share this response shape.
	type Heatmap = {
		matched: number;
		with_track: number;
		truncated?: boolean;
		facets: ActivityFacets;
		geojson: FeatureCollection;
	};
	const EMPTY: FeatureCollection = { type: 'FeatureCollection', features: [] };
	const PREPARATION_TIMEOUT_MS = 30_000;

	const filter = new ActivityFilter(page.url.searchParams);
	let overview = $state.raw<{ url: string; result: Heatmap } | null>(null);
	let data = $state.raw<Heatmap | null>(null);
	let facets = $state.raw<ActivityFacets | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let metric = $state<HeatmapMetric>('density');
	let scaleMode = $state<'auto' | 'fixed'>('auto');
	let individual = $state(false);
	const original = $derived(metric === 'density' && individual);
	const scale = $derived(
		scaleMode === 'fixed'
			? fixedHeatmapScale(metric)
			: adaptiveHeatmapScale(
					metric,
					overview?.result.geojson.features.map((f) => f.properties?.[metric]) ?? []
				)
	);
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
	let mapFrame: HTMLDivElement;
	let fullscreenButton: HTMLButtonElement;
	let fullscreen = $state(false);
	let nativeFullscreen = false;

	function resizeMap() {
		requestAnimationFrame(() => map?.resize());
	}

	function fullscreenChanged() {
		if (document.fullscreenElement === mapFrame) {
			nativeFullscreen = true;
			fullscreen = true;
		} else if (nativeFullscreen) {
			nativeFullscreen = false;
			fullscreen = false;
			fullscreenButton?.focus();
		}
		resizeMap();
	}

	async function toggleFullscreen() {
		if (fullscreen) {
			if (document.fullscreenElement === mapFrame) {
				await document.exitFullscreen();
			} else {
				fullscreen = false;
				resizeMap();
			}
			return;
		}
		if (mapFrame.requestFullscreen) {
			try {
				await mapFrame.requestFullscreen();
				return;
			} catch {
				// Some mobile and embedded browsers restrict native fullscreen.
			}
		}
		fullscreen = true;
		resizeMap();
	}

	function exitOnEscape(event: KeyboardEvent) {
		if (event.key === 'Escape' && fullscreen && !nativeFullscreen) {
			event.preventDefault();
			void toggleFullscreen();
		}
	}

	$effect(() => {
		if (!fullscreen) return;
		const overflow = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		return () => {
			document.body.style.overflow = overflow;
		};
	});

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
		if (!fitted && data) fitTracks();
	}

	function fitTracks() {
		if (!map || !overview) return;
		const b = bounds(overview.result.geojson);
		if (!b) return;
		fitted = true;
		map.fitBounds(b, { padding: 40, duration: 0, maxZoom: 14 });
	}

	function showData(result: Heatmap | null) {
		data = result;
		hovered = null;
		pinned = null;
		applyData();
	}

	async function load(url: string, signal: AbortSignal, onResult: (result: Heatmap) => void) {
		loading = true;
		error = null;
		pending = 0;
		let lowestPending = Infinity;
		let progressDeadline = performance.now() + PREPARATION_TIMEOUT_MS;
		try {
			while (!signal.aborted) {
				const res = await fetch(url, {
					signal: AbortSignal.any([signal, AbortSignal.timeout(PREPARATION_TIMEOUT_MS)])
				});
				if (!res.ok) throw new Error(m.heatmap_load_failed());
				const result = await res.json();
				if (signal.aborted) return;
				if (res.status === 202) {
					if (
						Number.isInteger(result.pending) &&
						result.pending > 0 &&
						result.pending < lowestPending
					) {
						lowestPending = result.pending;
						progressDeadline = performance.now() + PREPARATION_TIMEOUT_MS;
					}
					if (performance.now() >= progressDeadline) {
						error = m.heatmap_preparation_stalled();
						showData(null);
						return;
					}
					pending = result.pending;
					await new Promise((resolve) => setTimeout(resolve, 350));
					continue;
				}
				pending = 0;
				onResult(result as Heatmap);
				return;
			}
		} catch {
			if (!signal.aborted) {
				error = m.heatmap_load_failed();
				showData(null);
			}
		} finally {
			if (!signal.aborted) loading = false;
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
	const overviewURL = $derived.by(() => {
		void filter.immediateKey;
		void debounced;
		const endpoint = original ? '/api/activities/heatmap' : '/api/activities/heatmap/performance';
		return memberURL(`${endpoint}?${untrack(() => filter.params())}`, pageData.member);
	});
	// The overview covers the selected rider and filters at a single resolution.
	// Viewport requests draw finer geometry using the same overview scale.
	$effect(() => {
		const url = overviewURL;
		const ctrl = new AbortController();
		overview = null;
		untrack(() => {
			showData(null);
			void load(url, ctrl.signal, (result) => {
				overview = { url, result };
				facets = result.facets;
			});
		});
		return () => ctrl.abort();
	});
	$effect(() => {
		const base = overview;
		if (!base || base.url !== overviewURL) return;
		const area = original || !base.result.with_track ? '' : viewport;
		if (!area) {
			loading = false;
			pending = 0;
			error = null;
			untrack(() => showData(base.result));
			return;
		}
		const url = `${base.url}&${area}`;
		const ctrl = new AbortController();
		untrack(() => void load(url, ctrl.signal, showData));
		return () => ctrl.abort();
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
			paint: { 'line-color': heatmapColor(metric, scale), 'line-width': 3.5, 'line-opacity': 0.9 }
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
			const next = heatmapViewport(
				map.getZoom(),
				bounds.getWest(),
				bounds.getSouth(),
				bounds.getEast(),
				bounds.getNorth()
			);
			if (next !== viewport) viewport = next;
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
		const color = raw ? trackColor : heatmapColor(metric, scale);
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
		clearTimeout(debounceTimer);
		map?.remove();
		map = null;
	});
</script>

<svelte:document onfullscreenchange={fullscreenChanged} />
<svelte:window onkeydown={exitOnEscape} />

<section class="space-y-4 max-md:space-y-2.5">
	{#snippet mapControls()}
		<div class="flex flex-wrap items-center gap-2">
			<label class="flex items-center gap-2 text-sm">
				{m.heatmap_layer()}
				<select bind:value={metric} class="min-h-10 rounded border border-zinc-700 bg-panel px-2">
					{#each heatmapMetrics as layer (layer)}
						<option value={layer}>{heatmapMetricLabel(layer)}</option>
					{/each}
				</select>
			</label>
			{#if !original}
				<label class="flex items-center gap-2 text-sm">
					{m.heatmap_scale()}
					<select
						bind:value={scaleMode}
						class="min-h-10 rounded border border-zinc-700 bg-panel px-2"
					>
						<option value="auto">{m.heatmap_scale_auto()}</option>
						<option value="fixed">{m.heatmap_scale_fixed()}</option>
					</select>
				</label>
			{/if}
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
				onclick={fitTracks}
				disabled={!data || data.with_track === 0}
				class="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-300 hover:border-accent-500 hover:text-accent-300 disabled:opacity-50"
			>
				{m.heatmap_fit_tracks()}
			</button>
		</div>
	{/snippet}
	<FilterSheet
		collapsible
		label={m.heatmap_filters()}
		active={filter.active}
		reset={() => filter.clear()}
	>
		{#snippet toolbar()}
			<header class="flex items-center justify-between gap-4 max-md:flex-wrap">
				<div>
					<h1 class="text-3xl font-semibold tracking-tight max-md:text-2xl">
						{m.member_heatmap()}
					</h1>
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
			</header>
		{/snippet}
		<ActivityFilterBar
			{filter}
			{facets}
			controls={mapControls}
			showSort={false}
			member={pageData.member?.username}
		/>
	</FilterSheet>

	{#if error}
		<div
			role="alert"
			class="rounded border border-red-700/50 bg-red-950/30 px-3 py-2 text-xs text-red-300"
		>
			{error}
		</div>
	{/if}

	{#if !original}
		<div
			role="group"
			class="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs"
			aria-label={m.heatmap_legend()}
		>
			{#if metric === 'density' && scale.length}
				<span>{m.heatmap_passages()}</span>
			{/if}
			{#each scale as tick (tick.value)}
				<span class="inline-flex items-center gap-1.5">
					<span class="h-2.5 w-5 rounded-sm" style:background={tick.color}></span>{heatmapValue(
						metric,
						tick.value
					)}
				</span>
			{/each}
			{#if (!loading && !scale.length) || (metric !== 'density' && metric !== 'speed')}
				<span class="inline-flex items-center gap-1.5">
					<span class="h-2.5 w-5 rounded-sm" style:background={missingHeatmapColor}
					></span>{m.heatmap_insufficient()}
				</span>
			{/if}
		</div>
	{/if}

	<div
		bind:this={mapFrame}
		class={fullscreen ? 'fixed inset-0 z-[1000] h-dvh w-screen bg-zinc-900' : 'relative'}
	>
		<div
			bind:this={container}
			class={fullscreen
				? 'h-full w-full'
				: 'h-[70dvh] min-h-72 w-full overflow-hidden max-md:h-[60dvh] rounded-lg border border-zinc-800 bg-zinc-900'}
		></div>
		<button
			type="button"
			bind:this={fullscreenButton}
			onclick={toggleFullscreen}
			class="absolute right-3 top-3 z-10 min-h-10 rounded-md border border-zinc-700 bg-panel px-3 text-xs font-medium shadow hover:text-accent-300"
		>
			{fullscreen ? m.heatmap_exit_fullscreen() : m.heatmap_fullscreen()}
		</button>
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
