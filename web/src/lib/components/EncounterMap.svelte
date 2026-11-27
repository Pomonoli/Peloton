<script lang="ts">
	import { onMount } from 'svelte';
	import { AttributionControl, Marker, NavigationControl } from 'maplibre-gl';
	import 'maplibre-gl/dist/maplibre-gl.css';
	import { appearance } from '#lib/appearance.js';
	import { basemapStyle, createMap } from '#lib/basemap.js';
	import { m } from '#lib/paraglide/messages.js';

	let { lat, lon }: { lat: number; lon: number } = $props();
	let container: HTMLDivElement;
	let unavailable = $state(false);
	let ready = $state(false);

	onMount(() => {
		try {
			const map = createMap({
				container,
				style: basemapStyle($appearance.mode),
				center: [lon, lat],
				zoom: 15,
				scrollZoom: false,
				locale: { 'Map.Title': m.comparison_encounter_location() }
			});
			const marker = new Marker().setLngLat([lon, lat]).addTo(map);
			map.addControl(new NavigationControl({ showCompass: false }), 'top-right');
			map.addControl(new AttributionControl({ compact: true }), 'bottom-right');
			map.on('load', () => {
				ready = true;
				unavailable = false;
			});
			map.on('error', () => {
				if (!ready) unavailable = true;
			});
			let style = $appearance.mode;
			const stop = appearance.subscribe(({ mode }) => {
				if (mode !== style) {
					style = mode;
					map.setStyle(basemapStyle(mode));
				}
			});
			return () => {
				stop();
				marker.remove();
				map.remove();
			};
		} catch {
			unavailable = true;
		}
	});
</script>

<div class="relative overflow-hidden rounded-md border border-zinc-700">
	<div class="h-56 w-full bg-zinc-800" bind:this={container}></div>
	{#if unavailable}
		<p role="status" class="surface absolute inset-x-2 top-2 p-2 text-sm">
			{m.comparison_map_unavailable()}
		</p>
	{/if}
	<p class="surface absolute bottom-2 left-2 px-2 py-1 text-xs tabular-nums">
		{lat.toFixed(5)}, {lon.toFixed(5)}
	</p>
</div>
