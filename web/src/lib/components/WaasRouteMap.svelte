<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import {
		type Map as MapLibreMap,
		LngLatBounds,
		Marker,
		type GeoJSONSource,
		type StyleSpecification
	} from 'maplibre-gl';
	import 'maplibre-gl/dist/maplibre-gl.css';
	import { basemapStyle, createMap } from '#lib/basemap.js';
	import { appearance, appearanceColor, type ColorMode } from '#lib/appearance.js';
	import { m } from '#lib/paraglide/messages.js';
	import { dataUrl } from '#lib/waas/service.js';
	import { unwrapLongitude } from '#lib/waas/recorded.js';
	import type { Place } from '#lib/waas/types.js';
	import type { RecordedTrack, RoutePoint } from '#lib/recorded-routes.js';
	import type { MultiLineString, Feature } from 'geojson';
	import Icon from './Icon.svelte';
	import PlannerMapPreview from './PlannerMapPreview.svelte';

	let {
		track = [],
		reference = track,
		excluded = [],
		point = null,
		bounds = null,
		visits = [],
		compact = false
	}: {
		track?: RecordedTrack;
		reference?: RecordedTrack;
		excluded?: RecordedTrack;
		point?: RoutePoint | null;
		bounds?: [[number, number], [number, number]] | null;
		visits?: Place[];
		compact?: boolean;
	} = $props();

	let container: HTMLDivElement;
	let frame: HTMLDivElement;
	let dialog: HTMLDialogElement;
	let fullscreenButton = $state<HTMLButtonElement>();
	let map: MapLibreMap | undefined;
	let startMarker: Marker;
	let finishMarker: Marker;
	let profileMarker: Marker;
	let visitMarkers: Marker[] = [];
	let currentMode: ColorMode;
	let ready = $state(false);
	let preview = $state(false);
	let blank = $state(false);
	let fullscreen = $state(false);
	let modal = $state(false);
	let cycling = $state(false);
	let elevation = $state(false);
	let lastFit: RecordedTrack | undefined;
	let separatedEndpoints = false;
	let styleTimer = 0;
	const origin = $derived(reference[0]?.[0]?.lon ?? track[0]?.[0]?.lon ?? 0);
	const active = $derived(geometry(track, origin));
	const hidden = $derived(geometry(excluded, origin));
	const first = $derived(active.geometry.coordinates[0]?.[0]);
	const last = $derived(active.geometry.coordinates.at(-1)?.at(-1));

	function geometry(lines: RecordedTrack, around: number): Feature<MultiLineString> {
		return {
			type: 'Feature',
			properties: {},
			geometry: {
				type: 'MultiLineString',
				coordinates: lines.map((line) => line.map((p) => [unwrapLongitude(p.lon, around), p.lat]))
			}
		};
	}

	function fit() {
		if (!map || !ready || !first) return;
		const box = new LngLatBounds();
		for (const line of active.geometry.coordinates)
			for (const coordinate of line) box.extend([coordinate[0]!, coordinate[1]!]);
		const inset = Math.min(64, Math.max(35, container.clientWidth * 0.13));
		map.fitBounds(box, {
			padding:
				!compact && container.clientWidth < 500
					? { top: inset, left: inset, right: inset, bottom: 78 }
					: inset,
			maxZoom: 15,
			duration: 0
		});
	}

	function setTrack() {
		if (!map) return;
		(map.getSource('waas-excluded') as GeoJSONSource).setData(hidden);
		(map.getSource('waas-route') as GeoJSONSource).setData(active);
		if (!first || !last) {
			startMarker.remove();
			finishMarker.remove();
			return;
		}
		startMarker.setLngLat([first[0]!, first[1]!]);
		finishMarker.setLngLat([last[0]!, last[1]!]);
		if (!startMarker.getElement().isConnected) startMarker.addTo(map);
		if (!finishMarker.getElement().isConnected) finishMarker.addTo(map);
		positionEndpoints();
	}

	function positionEndpoints() {
		if (!map || !first || !last) return;
		const a = map.project([first[0]!, first[1]!]);
		const b = map.project([last[0]!, last[1]!]);
		const overlap = a.dist(b) < 29;
		if (overlap === separatedEndpoints) return;
		separatedEndpoints = overlap;
		startMarker.setOffset(overlap ? [-13, 0] : [0, 0]);
		finishMarker.setOffset(overlap ? [13, 0] : [0, 0]);
	}

	function marker(kind: string, label?: string) {
		const element = document.createElement('span');
		element.className = 'waas-marker waas-marker-' + kind;
		if (label) {
			element.title = label;
			element.setAttribute('aria-label', label);
			element.setAttribute('role', 'img');
		} else element.setAttribute('aria-hidden', 'true');
		return new Marker({ element });
	}

	function arrowImage() {
		const canvas = document.createElement('canvas');
		canvas.width = canvas.height = 24;
		const ctx = canvas.getContext('2d')!;
		ctx.beginPath();
		ctx.moveTo(9, 5);
		ctx.lineTo(16, 12);
		ctx.lineTo(9, 19);
		ctx.lineCap = 'round';
		ctx.lineJoin = 'round';
		ctx.strokeStyle = '#101d35';
		ctx.lineWidth = 6;
		ctx.stroke();
		ctx.strokeStyle = '#fff';
		ctx.lineWidth = 3;
		ctx.stroke();
		return ctx.getImageData(0, 0, 24, 24);
	}

	function loadLayers() {
		if (!map) return;
		window.clearTimeout(styleTimer);
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
			layout: { visibility: 'none' }
		});
		const empty = { type: 'FeatureCollection' as const, features: [] };
		map.addSource('waas-excluded', { type: 'geojson', data: empty, tolerance: 0.6 });
		map.addSource('waas-route', { type: 'geojson', data: empty, tolerance: 0.6 });
		map.addLayer({
			id: 'waas-excluded',
			type: 'line',
			source: 'waas-excluded',
			paint: {
				'line-color': $appearance.mode === 'dark' ? '#a6adbe' : '#626f83',
				'line-opacity': 0.78,
				'line-width': 4,
				'line-dasharray': [1, 1.65]
			},
			layout: { 'line-cap': 'round', 'line-join': 'round' }
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
			paint: { 'line-color': appearanceColor('--accent-primary'), 'line-width': 4.5 },
			layout: { 'line-cap': 'round', 'line-join': 'round' }
		});
		map.addImage('waas-chevron', arrowImage(), { pixelRatio: 2 });
		map.addLayer({
			id: 'waas-arrows',
			type: 'symbol',
			source: 'waas-route',
			layout: {
				'symbol-placement': 'line',
				'symbol-spacing': 110,
				'icon-image': 'waas-chevron',
				'icon-keep-upright': false,
				'icon-rotation-alignment': 'map',
				'icon-allow-overlap': true
			}
		});
		map.getCanvas().setAttribute('aria-label', m.planner_map_label());
		ready = true;
	}

	function useBlankStyle() {
		if (!map || blank || ready) return;
		blank = true;
		const style: StyleSpecification = {
			version: 8,
			sources: {},
			layers: [
				{
					id: 'background',
					type: 'background',
					paint: {
						'background-color': $appearance.mode === 'dark' ? '#19243a' : '#e5edf3'
					}
				}
			]
		};
		map.setStyle(style, { diff: false });
	}

	function fullscreenChanged() {
		modal = dialog.matches(':modal');
		fullscreen = document.fullscreenElement === frame || modal;
		map?.resize();
	}

	async function toggleFullscreen() {
		if (document.fullscreenElement === frame) {
			await document.exitFullscreen();
		} else if (modal) {
			dialog.close();
		} else {
			try {
				await frame.requestFullscreen();
			} catch {
				// The dialog fills the viewport on browsers that do not expose
				// element fullscreen, including iPhone Safari.
				dialog.close();
				dialog.showModal();
				fullscreenChanged();
				fullscreenButton?.focus();
			}
		}
	}

	function closeDialog() {
		if (!dialog.open) {
			dialog.show();
			fullscreenChanged();
			fullscreenButton?.focus();
		}
	}

	onMount(() => {
		let resize: ResizeObserver | undefined;
		document.addEventListener('fullscreenchange', fullscreenChanged);
		try {
			currentMode = $appearance.mode;
			map = createMap({
				container,
				style: basemapStyle(currentMode),
				center: first ? [first[0]!, first[1]!] : [4.139184, 51.164565],
				zoom: 10.5,
				scrollZoom: !compact,
				dragRotate: false,
				pitchWithRotate: false,
				touchPitch: false,
				locale: { 'Map.Title': m.planner_map_label() }
			});
			map.touchZoomRotate.disableRotation();
			startMarker = marker('start', m.planner_start());
			finishMarker = marker('finish', m.planner_finish());
			profileMarker = marker('profile');
			map.on('move', positionEndpoints);
			map.on('style.load', loadLayers);
			map.on('error', () => {
				if (!ready) useBlankStyle();
			});
			styleTimer = window.setTimeout(useBlankStyle, 6500);
			resize = new ResizeObserver(() => map?.resize());
			resize.observe(container);
		} catch {
			map?.remove();
			map = undefined;
			preview = true;
		}
		return () => {
			document.removeEventListener('fullscreenchange', fullscreenChanged);
			window.clearTimeout(styleTimer);
			resize?.disconnect();
			map?.remove();
		};
	});

	$effect(() => {
		if (ready) setTrack();
	});
	$effect(() => {
		if (!ready || reference === lastFit) return;
		lastFit = reference;
		untrack(fit);
	});
	$effect(() => {
		if (!map || !ready) return;
		if (!point) {
			profileMarker.remove();
			return;
		}
		profileMarker.setLngLat([unwrapLongitude(point.lon, origin), point.lat]);
		if (!profileMarker.getElement().isConnected) profileMarker.addTo(map);
	});
	$effect(() => {
		if (!map || !ready) return;
		for (const pin of visitMarkers) pin.remove();
		visitMarkers = visits.map((place, i) => {
			const pin = marker('visit', place.name);
			pin.getElement().textContent = String(i + 1);
			return pin.setLngLat([unwrapLongitude(place.point[1]!, origin), place.point[0]!]).addTo(map!);
		});
	});
	$effect(() => {
		if (!map || !ready) return;
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
					layout: { visibility: 'none' }
				},
				'waas-cycling'
			);
		}
		map.setLayoutProperty('waas-cycling', 'visibility', cycling ? 'visible' : 'none');
		if (map.getLayer('waas-height'))
			map.setLayoutProperty('waas-height', 'visibility', elevation ? 'visible' : 'none');
	});
	$effect(() => {
		const next = $appearance.mode;
		if (map && next !== currentMode) {
			currentMode = next;
			ready = false;
			blank = false;
			window.clearTimeout(styleTimer);
			styleTimer = window.setTimeout(useBlankStyle, 6500);
			map.setStyle(basemapStyle(next), { diff: false });
		}
	});
</script>

<div class="map-frame surface" class:compact bind:this={frame}>
	<dialog
		class="map-stage"
		bind:this={dialog}
		open
		role={modal ? 'dialog' : 'presentation'}
		aria-label={modal ? m.planner_map_label() : undefined}
		onclose={closeDialog}
	>
		<div
			class="map-container"
			bind:this={container}
			role="region"
			aria-label={m.planner_map_label()}
		></div>
		{#if !ready && first}
			<PlannerMapPreview
				lines={active.geometry.coordinates}
				excluded={hidden.geometry.coordinates}
				{point}
				{origin}
			/>
		{/if}
		{#if !compact || first}
			<div class="map-controls" aria-label={m.planner_map_label()}>
				<div class="map-buttons">
					{#if !preview}
						<button
							type="button"
							aria-label={m.planner_zoom_in()}
							title={m.planner_zoom_in()}
							disabled={!ready}
							onclick={() => map?.zoomIn()}><Icon name="plus" size={18} /></button
						>
						<button
							type="button"
							aria-label={m.planner_zoom_out()}
							title={m.planner_zoom_out()}
							disabled={!ready}
							onclick={() => map?.zoomOut()}><Icon name="minus" size={18} /></button
						>
					{/if}
					{#if !preview}
						<button
							type="button"
							aria-label={m.waas_fit_route()}
							title={m.waas_fit_route()}
							disabled={!first || !ready}
							onclick={fit}><Icon name="fit" size={18} /></button
						>
					{/if}
					<button
						type="button"
						bind:this={fullscreenButton}
						aria-label={fullscreen ? m.planner_exit_fullscreen() : m.planner_fullscreen()}
						title={fullscreen ? m.planner_exit_fullscreen() : m.planner_fullscreen()}
						onclick={toggleFullscreen}
					>
						<Icon name={fullscreen ? 'shrink' : 'expand'} size={18} />
					</button>
				</div>
			</div>
		{/if}
		{#if !compact && ready}
			<details class="map-layers">
				<summary aria-label={m.planner_layers()} title={m.planner_layers()}
					><Icon name="layers" size={19} /></summary
				>
				<div class="layer-options surface">
					<button
						type="button"
						aria-pressed={!cycling && !elevation}
						onclick={() => {
							cycling = false;
							elevation = false;
						}}>{m.waas_roads()}</button
					>
					<button type="button" aria-pressed={cycling} onclick={() => (cycling = !cycling)}
						>{m.waas_cycle_layer()}</button
					>
					{#if bounds}<button
							type="button"
							aria-pressed={elevation}
							onclick={() => (elevation = !elevation)}>{m.waas_height_layer()}</button
						>{/if}
				</div>
			</details>
		{/if}
		{#if first && !compact}
			<div class="map-legend">
				<span><i class="key-start"></i>{m.planner_start()}</span>
				<span><i class="key-finish"></i>{m.planner_finish()}</span>
				{#if excluded.length}
					<span><i class="key-kept"></i>{m.planner_kept()}</span>
					<span><i class="key-trimmed"></i>{m.planner_excluded()}</span>
				{/if}
			</div>
		{/if}
		{#if (blank || preview) && !compact}
			<span class="map-status" role="status">{m.planner_map_preview()}</span>
		{/if}
	</dialog>
</div>

<style>
	.map-frame {
		--planner-map-height: clamp(27rem, 58dvh, 44rem);
		position: relative;
		overflow: hidden;
		background: var(--surface-muted);
		isolation: isolate;
	}
	.map-frame.compact {
		--planner-map-height: 16rem;
	}
	.map-container {
		width: 100%;
		height: var(--planner-map-height);
		overscroll-behavior: contain;
	}
	.map-frame :global(.maplibregl-canvas) {
		touch-action: none;
	}
	.map-frame:fullscreen,
	.map-stage:modal {
		--planner-map-height: 100dvh;
		width: 100%;
		height: 100%;
		border: none;
		border-radius: 0;
	}
	.map-stage {
		position: relative;
		margin: 0;
		inset: auto;
		width: 100%;
		max-width: none;
		max-height: none;
		padding: 0;
		overflow: hidden;
		background: var(--surface-muted);
	}
	.map-stage:modal {
		position: fixed;
		inset: 0;
	}

	.map-controls {
		position: absolute;
		top: 0.75rem;
		right: 0.75rem;
	}
	.map-buttons,
	.map-layers summary,
	.map-legend,
	.map-status {
		border: 1px solid var(--line-subtle);
		border-radius: var(--radius-control);
		background: var(--surface-panel);
		box-shadow: var(--panel-shadow);
		color: var(--text-strong);
	}
	.map-buttons > button,
	.map-layers summary {
		display: grid;
		width: 2.625rem;
		height: 2.625rem;
		place-items: center;
		cursor: pointer;
	}
	.map-buttons > button + button {
		border-top: 1px solid var(--line-subtle);
	}
	.map-buttons > button:first-child {
		border-radius: var(--radius-control) var(--radius-control) 0 0;
	}
	.map-buttons > button:last-child {
		border-radius: 0 0 var(--radius-control) var(--radius-control);
	}
	.map-buttons > button:hover:not(:disabled),
	.map-layers summary:hover {
		background: var(--surface-hover);
		color: var(--accent-text);
	}
	.map-buttons > button:disabled {
		opacity: 0.42;
		cursor: default;
	}
	.map-layers {
		position: absolute;
		top: 0.75rem;
		left: 0.75rem;
	}
	.map-layers summary {
		list-style: none;
	}
	.map-layers summary::-webkit-details-marker {
		display: none;
	}
	.layer-options {
		display: grid;
		gap: 0.15rem;
		margin-top: 0.35rem;
		padding: 0.25rem;
	}
	.layer-options button {
		padding: 0.48rem 0.6rem;
		border-radius: 0.4rem;
		font-size: 0.78rem;
		text-align: left;
		cursor: pointer;
	}
	.layer-options button:hover,
	.layer-options button[aria-pressed='true'] {
		color: var(--accent-text);
		background: var(--surface-active);
	}
	.map-legend {
		position: absolute;
		bottom: 0.75rem;
		left: 0.75rem;
		display: flex;
		flex-wrap: wrap;
		gap: 0.28rem 0.65rem;
		max-width: calc(100% - 1.5rem);
		padding: 0.38rem 0.55rem;
		font-size: 0.68rem;
		font-weight: 650;
	}
	.map-legend span {
		display: flex;
		align-items: center;
		gap: 0.3rem;
	}
	.map-legend i {
		display: inline-block;
		width: 0.52rem;
		height: 0.52rem;
		border-radius: 50%;
	}
	.key-start {
		background: var(--map-start);
	}
	.key-finish {
		background: var(--map-finish);
	}
	.map-legend .key-kept,
	.map-legend .key-trimmed {
		width: 0.9rem;
		height: 0;
		border-radius: 0;
		border-top: 2px solid var(--accent-primary);
	}
	.map-legend .key-trimmed {
		border-top: 2px dashed var(--text-muted);
	}
	.map-status {
		position: absolute;
		top: 3.75rem;
		left: 0.75rem;
		padding: 0.35rem 0.5rem;
		font-size: 0.7rem;
	}
	:global(.waas-marker) {
		display: grid;
		width: 24px;
		height: 24px;
		place-items: center;
		border: 2px solid white;
		border-radius: 50%;
		color: white;
		background: #203251;
		box-shadow: 0 1px 5px #0005;
		font: 700 11px system-ui;
		pointer-events: none;
	}
	:global(.waas-marker-start) {
		background: #137443;
	}
	:global(.waas-marker-finish) {
		background: #bb2345;
	}
	:global(.waas-marker-start)::after {
		content: '';
		width: 0;
		height: 0;
		margin-left: 2px;
		border-block: 5px solid transparent;
		border-left: 7px solid white;
	}
	:global(.waas-marker-finish)::after {
		content: '';
		width: 10px;
		height: 10px;
		background: conic-gradient(white 25%, transparent 0 50%, white 0 75%, transparent 0) 0 0 / 7px
			7px;
	}
	:global(.waas-marker-profile) {
		z-index: 1;
		width: 16px;
		height: 16px;
		outline: 2px solid var(--accent-primary);
		background: var(--accent-primary);
	}
	@media (width < 48rem) {
		.map-frame {
			--planner-map-height: clamp(18rem, 44dvh, 26rem);
		}
		.map-frame.compact {
			--planner-map-height: 14rem;
		}
	}
</style>
