<script lang="ts">
	import FilterSheet from '#lib/components/FilterSheet.svelte';
	import { memberURL, type MemberView } from '#lib/member.js';
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
		type Map as MapLibreMap,
		LngLatBounds,
		type GeoJSONSource,
		type MapLayerMouseEvent,
		type Point
	} from 'maplibre-gl';
	import type { FeatureCollection, Position } from 'geojson';
	import 'maplibre-gl/dist/maplibre-gl.css';
	import { basemapStyle, createMap, basemapVariants, type BasemapVariant } from '#lib/basemap.js';
	import { appearance } from '#lib/appearance.js';
	import ActivityFilterBar from '#lib/components/ActivityFilterBar.svelte';
	import HeatmapResults from '#lib/components/HeatmapResults.svelte';
	import {
		addDiscoveryOverlay,
		addMapNavigation,
		bindMapSelection,
		emptyFeatures,
		nearestMapLine,
		setMapFeatures,
		showDiscoveryLocation,
		showDiscoveryTrack
	} from '#lib/discovery-map.js';
	import { ActivityFilter, type ActivityFacets } from '#lib/activity-filter.svelte.js';
	import {
		addTerritoryLayers,
		territoryBounds,
		type Territory,
		type TerritoryCell
	} from '#lib/territory-map.js';

	let { data: pageData } = $props();

	// Both heatmap endpoints share this response shape.
	type Heatmap = {
		matched: number;
		with_track: number;
		truncated?: boolean;
		facets: ActivityFacets;
		geojson: FeatureCollection;
	};
	const PREPARATION_TIMEOUT_MS = 30_000;
	const TERRITORY_ENDPOINT = '/api/activities/heatmap/territory';
	const TERRITORY_REFRESH_MS = 30_000;
	const preparationStalled = Symbol();

	const filter = new ActivityFilter(page.url.searchParams);
	const allRidersKey = '*';
	let riders = $state.raw<MemberView[]>([]);
	let rider = $derived(pageData.member?.username ?? '');
	const allRiders = $derived(rider === allRidersKey);
	const self = $derived(
		pageData.user
			? {
					id: pageData.user.id,
					username: pageData.user.username,
					display_name: pageData.user.displayName
				}
			: null
	);
	const currentRider = $derived(
		riders.find((member) => member.username === rider) ??
			(pageData.member?.username === rider ? pageData.member : null)
	);
	const options = $derived(
		riders.filter((member) => member.id !== self?.id && member.id !== pageData.member?.id)
	);
	let overview = $state.raw<{ url: string; result: Heatmap } | null>(null);
	let data = $state.raw<Heatmap | null>(null);
	let facets = $state.raw<ActivityFacets | null>(null);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let layer = $state<HeatmapMetric | 'territory'>('density');
	const territoryMode = $derived(layer === 'territory');
	const metric = $derived(layer === 'territory' ? 'density' : layer);
	let scaleMode = $state<'auto' | 'fixed'>('auto');
	let individual = $state(false);
	const original = $derived(!territoryMode && metric === 'density' && individual && !allRiders);
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
	let pinned = $state<{ values: Record<string, unknown>; location: Position } | null>(null);
	const inspected = $derived(pinned?.values ?? hovered);
	let selectedActivity = $state('');
	let selectedTrack = $state.raw<Position[][]>([]);
	let trackStatus = $state<'idle' | 'loading' | 'missing' | 'failed'>('idle');
	let trackRetry = $state(0);

	let territoryData = $state.raw<Territory | null>(null);
	let territoryPending = $state(0);
	let territoryError = $state<string | null>(null);
	let territoryLoading = $state(false);
	let territorySelection = $state.raw<[number, number] | null>(null);
	let territoryCell = $state.raw<TerritoryCell | null>(null);
	let territoryCellStatus = $state<'idle' | 'loading' | 'missing' | 'failed'>('idle');
	const territoryRiders = $derived(
		new Map(territoryData?.legend.map((rider) => [rider.id, rider]))
	);
	const number = new Intl.NumberFormat('en');
	const area = new Intl.NumberFormat('en', { maximumFractionDigits: 2 });
	const distance = new Intl.NumberFormat('en', { maximumFractionDigits: 3 });
	const percent = new Intl.NumberFormat('en', { style: 'percent', maximumFractionDigits: 1 });

	function territoryDistance(meters: number) {
		return meters < 1000
			? m.territory_distance_m({ value: distance.format(meters) })
			: m.territory_distance_km({ value: distance.format(meters / 1000) });
	}

	function clearDiscovery() {
		pinned = null;
		hovered = null;
		selectedActivity = '';
	}

	$effect(() => {
		void trackRetry;
		const id = selectedActivity;
		selectedTrack = [];
		trackStatus = id ? 'loading' : 'idle';
		if (!id) return;
		const ctrl = new AbortController();
		void fetch('/api/activities/' + encodeURIComponent(id) + '/heatmap-track', {
			signal: ctrl.signal
		})
			.then(async (response) => {
				if (!response.ok) throw new Error(String(response.status));
				const result: { lines: Position[][] } = await response.json();
				if (!ctrl.signal.aborted) {
					selectedTrack = result.lines;
					trackStatus = result.lines.length ? 'idle' : 'missing';
				}
			})
			.catch(() => {
				if (!ctrl.signal.aborted) trackStatus = 'failed';
			});
		return () => ctrl.abort();
	});

	function applyDiscovery() {
		showDiscoveryLocation(
			map,
			territoryMode ? (territoryCell ? territorySelection : null) : (pinned?.location ?? null)
		);
		showDiscoveryTrack(map, selectedTrack);
	}

	$effect(applyDiscovery);

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
	let territoryFitted = false;

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
		const fc = data?.geojson ?? emptyFeatures;
		(map.getSource('tracks') as GeoJSONSource | undefined)?.setData(fc);
		// Frame the tracks on the first result only; later filter changes keep
		// the viewport the user has set up.
		if (!territoryMode && !fitted && data) fitTracks();
	}

	function fitTracks() {
		if (!map) return;
		const b = territoryMode
			? territoryData && territoryBounds(territoryData)
			: overview && bounds(overview.result.geojson);
		if (!b) return;
		if (territoryMode) territoryFitted = true;
		else fitted = true;
		map.fitBounds(b, { padding: 40, duration: 0, maxZoom: 14 });
	}

	function showData(result: Heatmap | null) {
		data = result;
		hovered = null;
		applyData();
	}

	async function fetchPrepared<T>(
		url: string,
		signal: AbortSignal,
		onPending: (count: number) => void,
		version?: string
	): Promise<T | null> {
		let lowestPending = Infinity;
		let progressDeadline = performance.now() + PREPARATION_TIMEOUT_MS;
		while (!signal.aborted) {
			const res = await fetch(url, {
				signal: AbortSignal.any([signal, AbortSignal.timeout(PREPARATION_TIMEOUT_MS)]),
				...(version ? { headers: { 'If-None-Match': '"' + version + '"' }, cache: 'no-store' } : {})
			});
			if (res.status === 304) return null;
			if (!res.ok) throw new Error(String(res.status));
			if (res.status !== 202) return (await res.json()) as T;
			version = undefined;
			const result: { pending: number } = await res.json();
			if (signal.aborted) break;
			if (
				Number.isInteger(result.pending) &&
				result.pending > 0 &&
				result.pending < lowestPending
			) {
				lowestPending = result.pending;
				progressDeadline = performance.now() + PREPARATION_TIMEOUT_MS;
			}
			if (performance.now() >= progressDeadline) throw preparationStalled;
			onPending(result.pending);
			await new Promise((resolve) => setTimeout(resolve, 350));
		}
		return null;
	}

	async function load(url: string, signal: AbortSignal, onResult: (result: Heatmap) => void) {
		loading = true;
		error = null;
		pending = 0;
		try {
			const result = await fetchPrepared<Heatmap>(url, signal, (count) => (pending = count));
			if (!signal.aborted && result) {
				pending = 0;
				onResult(result);
			}
		} catch (err) {
			if (!signal.aborted) {
				error =
					err === preparationStalled ? m.heatmap_preparation_stalled() : m.heatmap_load_failed();
				showData(null);
			}
		} finally {
			if (!signal.aborted) loading = false;
		}
	}

	$effect(() => {
		if (!territoryMode) return;
		const ctrl = new AbortController();
		let refreshing = false;
		async function refresh() {
			if (refreshing || document.hidden) return;
			refreshing = true;
			if (!territoryData) territoryLoading = true;
			try {
				const result = await fetchPrepared<Territory>(
					TERRITORY_ENDPOINT,
					ctrl.signal,
					(count) => {
						territoryData = null;
						territoryLoading = true;
						territoryPending = count;
					},
					territoryData?.version
				);
				if (!ctrl.signal.aborted) {
					if (result) territoryData = result;
					territoryError = null;
				}
			} catch (err) {
				if (!ctrl.signal.aborted) {
					territoryData = null;
					territoryError =
						err === preparationStalled
							? m.heatmap_preparation_stalled()
							: m.territory_load_failed();
				}
			} finally {
				refreshing = false;
				if (!ctrl.signal.aborted) {
					territoryPending = 0;
					territoryLoading = false;
				}
			}
		}
		untrack(() => void refresh());
		const timer = setInterval(() => void refresh(), TERRITORY_REFRESH_MS);
		document.addEventListener('visibilitychange', refresh);
		return () => {
			ctrl.abort();
			clearInterval(timer);
			document.removeEventListener('visibilitychange', refresh);
			territoryData = null;
			territorySelection = null;
			territoryLoading = false;
			territoryPending = 0;
			territoryError = null;
		};
	});

	$effect(() => {
		const selection = territorySelection;
		const version = territoryData?.version;
		territoryCell = null;
		territoryCellStatus = 'idle';
		if (!territoryMode || !version || !selection) return;
		const ctrl = new AbortController();
		const [lon, lat] = selection;
		const query = new URLSearchParams({ lat: String(lat), lon: String(lon) });
		territoryCellStatus = 'loading';
		void fetchPrepared<TerritoryCell>(TERRITORY_ENDPOINT + '?' + query, ctrl.signal, () => {})
			.then((result) => {
				if (ctrl.signal.aborted || !result) return;
				territoryCell = result;
				territoryCellStatus = 'idle';
			})
			.catch((err) => {
				if (!ctrl.signal.aborted) {
					territoryCellStatus =
						err instanceof Error && err.message === '404' ? 'missing' : 'failed';
				}
			});
		return () => ctrl.abort();
	});

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
		if (territoryMode) return '';
		void filter.immediateKey;
		void debounced;
		const endpoint = original ? '/api/activities/heatmap' : '/api/activities/heatmap/performance';
		const params = untrack(() => filter.params());
		if (allRiders) params.set('riders', 'all');
		else if (rider) params.set('member', rider);
		return `${endpoint}?${params}`;
	});
	// The overview covers the selected rider and filters at a single resolution.
	// Viewport requests draw finer geometry using the same overview scale.
	$effect(() => {
		const url = overviewURL;
		const ctrl = new AbortController();
		overview = null;
		untrack(() => {
			clearDiscovery();
			showData(null);
			if (!url) return;
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
		map.addSource('tracks', { type: 'geojson', data: emptyFeatures });
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
		addTerritoryLayers(map, basemap === 'dark');
		addDiscoveryOverlay(map, basemap === 'dark' ? 'dark' : 'light');
		applyDiscovery();
		mapLoaded = true;
		applyPaint();
		applyData();
		applyTerritory();
	}

	function selectAt(point: Point) {
		if (!map || !mapLoaded) return;
		if (territoryMode) {
			if (territoryData?.geojson.features.length) {
				const location = map.unproject(point).wrap();
				territorySelection = [location.lng, location.lat];
			}
			return;
		}
		if (!data || !map.isSourceLoaded('tracks')) return;
		const hit = nearestMapLine(map, point, 'tracks-line');
		if (!hit) return;
		const values = hit.feature.properties;
		if (original) {
			if (typeof values.id === 'string') {
				void goto(memberURL(`/activities/${values.id}`, currentRider));
			}
			return;
		}
		selectedActivity = '';
		pinned = { values, location: hit.location.toArray() };
	}

	onMount(() => {
		const ctrl = new AbortController();
		void fetch('/api/riders', { signal: ctrl.signal })
			.then(async (response) => {
				if (!response.ok) return;
				const data: { members: MemberView[] } = await response.json();
				if (!ctrl.signal.aborted) riders = data.members;
			})
			.catch(() => {
				// Personal and community maps work without the optional rider directory.
			});
		if (!container) return () => ctrl.abort();
		currentStyle = basemapStyle(basemap);
		map = createMap({
			container,
			style: currentStyle,
			center: [10, 50],
			zoom: 4,
			dragRotate: false,
			touchPitch: false
		});
		addMapNavigation(map, 'bottom-right');
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
		map.on('mouseenter', 'territory-fill', () => {
			if (map) map.getCanvas().style.cursor = 'pointer';
		});
		map.on('mouseleave', 'territory-fill', () => {
			if (map) map.getCanvas().style.cursor = '';
		});
		bindMapSelection(map, selectAt);
		return () => ctrl.abort();
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
		map.setLayoutProperty('tracks-line', 'visibility', territoryMode ? 'none' : 'visible');
		map.setPaintProperty('tracks-glow', 'line-color', trackColor);
		map.setPaintProperty('tracks-line', 'line-color', color);
		map.setPaintProperty('tracks-line', 'line-width', raw ? 1.8 : 3.5);
		map.setPaintProperty('tracks-line', 'line-opacity', opacity);
	}

	$effect(applyPaint);

	function applyTerritory() {
		const visible = territoryMode;
		const result = territoryData;
		if (!map || !mapLoaded) return;
		const visibility = visible ? 'visible' : 'none';
		map.setLayoutProperty('territory-fill', 'visibility', visibility);
		map.setLayoutProperty('territory-outline', 'visibility', visibility);
		setMapFeatures(map, 'territory', result?.geojson ?? emptyFeatures);
		if (visible && !territoryFitted && result) fitTracks();
	}

	$effect(applyTerritory);

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
			{#if !territoryMode && !original}
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
			{#if !territoryMode && metric === 'density' && !allRiders}
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
				disabled={territoryMode ? !territoryData?.geojson.features.length : !data?.with_track}
				class="rounded border border-zinc-700 px-3 py-1.5 text-xs text-zinc-300 hover:border-accent-500 hover:text-accent-300 disabled:opacity-50"
			>
				{territoryMode ? m.territory_fit() : m.heatmap_fit_tracks()}
			</button>
		</div>
	{/snippet}
	{#snippet heatmapToolbar()}
		<header class="flex items-center justify-between gap-4 max-md:flex-wrap">
			<div>
				<h1 class="page-title">
					{m.member_heatmap()}
				</h1>
				{#if territoryMode}
					<p class="mt-1 text-sm text-quiet">{m.territory_window()}</p>
				{:else if !filter.from && !filter.to}<p class="mt-1 text-sm text-quiet">
						{m.heatmap_all_time()}
					</p>{/if}
				{#if !territoryMode && data}
					<p class="mt-1 text-sm text-zinc-400">
						<span class="font-medium text-zinc-200">{data.with_track}</span> tracks from {data.matched}
						{#if filter.active}matching{/if} activities{#if data.truncated}
							<span class="text-amber-400">(newest {data.with_track} shown)</span>{/if}
					</p>
				{/if}
			</div>
			<div class="flex flex-wrap gap-2">
				{#if !territoryMode}
					<label class="flex items-center gap-2 text-sm">
						{m.routes_rider()}
						<select
							bind:value={rider}
							class="min-h-10 rounded border border-zinc-700 bg-panel px-2"
						>
							<option value=""
								>{self?.display_name || self?.username || m.heatmap_my_rides()}</option
							>
							<option value={allRidersKey}>{m.routes_all_riders()}</option>
							{#if pageData.member}
								<option value={pageData.member.username}
									>{pageData.member.display_name || pageData.member.username}</option
								>
							{/if}
							{#each options as member (member.id)}
								<option value={member.username}>{member.display_name || member.username}</option>
							{/each}
						</select>
					</label>
				{/if}
				<label class="flex items-center gap-2 text-sm">
					{m.heatmap_layer()}
					<select bind:value={layer} class="min-h-10 rounded border border-zinc-700 bg-panel px-2">
						{#each heatmapMetrics as item (item)}
							<option value={item}>{heatmapMetricLabel(item)}</option>
						{/each}
						<option value="territory">{m.territory_layer()}</option>
					</select>
				</label>
			</div>
		</header>
	{/snippet}
	{#snippet heatmapFilters()}
		{#key rider}
			<ActivityFilterBar
				{filter}
				{facets}
				controls={mapControls}
				showSort={false}
				member={allRiders ? undefined : rider || undefined}
			/>
		{/key}
	{/snippet}
	<FilterSheet
		collapsible
		label={m.heatmap_filters()}
		active={filter.active}
		toolbar={heatmapToolbar}
		belowToolbar={territoryMode ? mapControls : undefined}
		reset={() => filter.clear()}
		children={territoryMode ? undefined : heatmapFilters}
	/>

	{#if territoryMode ? territoryError : error}
		<div
			role="alert"
			class="rounded border border-red-700/50 bg-red-950/30 px-3 py-2 text-xs text-red-300"
		>
			{territoryMode ? territoryError : error}
		</div>
	{/if}

	{#if !territoryMode && !original}
		<div
			role="group"
			class="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs"
			aria-label={m.heatmap_legend()}
		>
			{#if metric === 'density' && scale.length}
				<span>{m.heatmap_passages()}</span>
			{/if}
			{#each scale as tick (tick.value)}
				<span class="metric-value inline-flex items-center gap-1.5">
					<span class="h-2.5 w-5 rounded-sm" style:background={tick.color}></span>{heatmapValue(
						metric,
						tick.value
					)}
				</span>
			{/each}
			{#if (!loading && !scale.length) || (metric !== 'density' && metric !== 'speed')}
				<span class="metric-value inline-flex items-center gap-1.5">
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
			class="absolute right-[max(0.75rem,env(safe-area-inset-right))] top-[max(0.75rem,env(safe-area-inset-top))] z-10 min-h-10 rounded-md border border-zinc-700 bg-panel px-3 text-xs font-medium shadow hover:text-accent-300"
		>
			{fullscreen ? m.heatmap_exit_fullscreen() : m.heatmap_fullscreen()}
		</button>
		{#if territoryMode ? territoryLoading : loading}
			{@const preparing = territoryMode ? territoryPending : pending}
			<div
				class="pointer-events-none absolute top-3 left-3 max-w-[calc(100%-8rem)] rounded bg-zinc-900/80 px-2 py-1 text-xs text-zinc-300"
			>
				{preparing > 0
					? m.heatmap_preparing({ count: preparing })
					: territoryMode
						? m.territory_loading()
						: m.heatmap_loading()}
			</div>
		{:else if territoryMode && territoryData?.geojson.features.length === 0}
			<div class="pointer-events-none absolute inset-0 flex items-center justify-center">
				<p class="rounded bg-zinc-900/80 px-4 py-2 text-sm text-zinc-300">
					{m.territory_empty()}
				</p>
			</div>
		{:else if !territoryMode && data && data.geojson.features.length === 0}
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
		{#if territoryMode}
			{#if territoryData?.geojson.features.length}
				<div
					aria-live="polite"
					class="surface px-3 py-2 text-sm {fullscreen
						? 'absolute inset-x-2 bottom-[calc(2rem+env(safe-area-inset-bottom))] z-10 mx-auto max-h-[40dvh] max-w-max overflow-y-auto'
						: 'mt-2'}"
				>
					{#if territoryCell}
						<p class="font-medium" data-testid="territory-owner">
							{m.territory_owner({ name: territoryRiders.get(territoryCell.owner_id)?.name ?? '' })}
						</p>
						<dl class="mt-1 flex flex-wrap gap-x-5 gap-y-1">
							{#each territoryCell.distances as score (score.rider_id)}
								{@const owner = territoryRiders.get(score.rider_id)}
								{#if owner}
									<div>
										<dt class="flex items-center gap-1.5 text-xs text-quiet">
											<span class="size-2.5 shrink-0 rounded-sm" style:background={owner.color}
											></span>
											{owner.name}
										</dt>
										<dd class="metric-value">{territoryDistance(score.distance_m)}</dd>
									</div>
								{/if}
							{/each}
						</dl>
					{:else}
						<p class="text-quiet">
							{territoryCellStatus === 'loading'
								? m.territory_cell_loading()
								: territoryCellStatus === 'missing'
									? m.territory_unclaimed()
									: territoryCellStatus === 'failed'
										? m.territory_cell_failed()
										: m.territory_inspect()}
						</p>
					{/if}
				</div>
			{/if}
		{:else if !original}
			<div
				class="surface px-3 py-2 text-sm {fullscreen
					? 'absolute inset-x-2 bottom-[calc(2rem+env(safe-area-inset-bottom))] z-10 mx-auto max-w-max'
					: 'mt-2'}"
			>
				{#if inspected}
					<dl class="flex flex-wrap gap-x-5 gap-y-1">
						{#each heatmapMetrics as item (item)}
							{@const value = inspected[item]}
							<div>
								<dt class="text-xs text-quiet">
									{item === 'density' ? m.heatmap_passages() : heatmapMetricLabel(item)}
								</dt>
								<dd class="metric-value font-medium">
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
	</div>
	{#if territoryMode && territoryData?.legend.length}
		<div class="surface overflow-x-auto px-3 py-2">
			<table class="w-full text-left text-xs">
				<caption class="pb-2 text-left text-sm font-medium">{m.heatmap_legend()}</caption>
				<thead class="border-b border-zinc-700 text-quiet">
					<tr>
						<th scope="col" class="py-1 pr-2">{m.routes_rider()}</th>
						<th scope="col" class="px-2 py-1 text-right">{m.territory_hexagons()}</th>
						<th scope="col" class="px-2 py-1 text-right">{m.territory_area()}</th>
						<th scope="col" class="py-1 pl-2 text-right">{m.territory_share()}</th>
					</tr>
				</thead>
				<tbody>
					{#each territoryData.legend as entry (entry.id)}
						<tr class="border-b border-zinc-700/40 last:border-0">
							<th scope="row" class="py-1.5 pr-2 font-medium">
								<span class="flex items-center gap-2">
									<span class="size-3 shrink-0 rounded-sm" style:background={entry.color}></span>
									{entry.name}
								</span>
							</th>
							<td class="metric-value px-2 text-right">{number.format(entry.cells)}</td>
							<td class="metric-value px-2 text-right">{area.format(entry.area_km2)}</td>
							<td class="metric-value pl-2 text-right">{percent.format(entry.percentage / 100)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
	{#if original}<p class="text-xs text-zinc-500">{m.heatmap_open_activity()}</p>{/if}
	{#if !territoryMode && !original && pinned && typeof pinned.values.cell === 'string'}
		{#if trackStatus === 'loading'}
			<p role="status" class="text-sm text-quiet">{m.heatmap_track_loading()}</p>
		{:else if trackStatus === 'missing'}
			<p role="status" class="text-sm text-quiet">{m.detail_map_no_track()}</p>
		{:else if trackStatus === 'failed'}
			<p role="alert" class="text-sm">
				{m.heatmap_track_failed()}
				<button class="ml-1 min-h-10 text-accent-300 underline" onclick={() => trackRetry++}
					>{m.routes_retry()}</button
				>
			</p>
		{/if}
		{#key pinned}
			<HeatmapResults
				query={overviewURL.split('?')[1] ?? ''}
				cell={pinned.values.cell}
				selected={selectedActivity}
				fallbackRider={currentRider ?? self}
				onselect={(id) => (selectedActivity = id)}
				onclear={clearDiscovery}
			/>
		{/key}
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
