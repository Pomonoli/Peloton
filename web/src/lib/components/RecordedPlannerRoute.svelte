<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import WaasRouteMap from './WaasRouteMap.svelte';
	import ElevationProfile from './ElevationProfile.svelte';
	import { canMoveStart, editTrack } from '#lib/waas/recorded.js';
	import { downloadGPX } from '#lib/waas/download.js';
	import {
		previewPoints,
		recordedRoutePath,
		routeData,
		routeDistance,
		type RouteDetail
	} from '#lib/recorded-routes.js';

	let { id }: { id: string } = $props();
	let route = $state<RouteDetail>();
	let failed = $state(false);
	let exportFailed = $state(false);
	let reverse = $state(false);
	let start = $state(0);
	let from = $state(0);
	let to = $state(100);
	const edited = $derived(reverse || start !== 0 || from !== 0 || to !== 100);
	const canRotate = $derived(route?.type === 'loop' && canMoveStart(route.track));
	const track = $derived(
		route ? (edited ? editTrack(route.track, from, to, start, reverse) : route.track) : []
	);
	const profile = $derived(previewPoints(track));
	const hasElevation = $derived(profile.filter((point) => point.eleM != null).length > 1);
	const distance = $derived(edited ? (profile.at(-1)?.distM ?? 0) : (route?.distance_m ?? 0));
	const newPlan = $derived(
		'/routes/waasland' +
			(route ? '?km=' + Math.min(200, Math.max(15, Math.round(route.distance_m / 500) / 2)) : '')
	);

	function reset() {
		reverse = false;
		start = from = 0;
		to = 100;
		exportFailed = false;
	}

	$effect(() => {
		const controller = new AbortController();
		route = undefined;
		failed = false;
		void routeData<RouteDetail>(recordedRoutePath(id), controller.signal)
			.then((result) => {
				if (!controller.signal.aborted) route = result;
			})
			.catch(() => {
				if (!controller.signal.aborted) failed = true;
			});
		return () => controller.abort();
	});

	async function exportEdits() {
		if (!route || !track.length) return;
		const name = route.name;
		const segments = track.map((line) =>
			line.map((point) => [point.lat, point.lon, point.elevation_m])
		);
		exportFailed = false;
		try {
			const module = '/waas-engine/gpx.js';
			const { writeGPX } = await import(/* @vite-ignore */ module);
			downloadGPX(writeGPX(name, '', segments), `peloton-route-${id}.gpx`);
		} catch {
			exportFailed = true;
		}
	}
</script>

<div class="mb-4 flex flex-wrap items-start justify-between gap-2">
	<div>
		<p class="text-xs font-semibold text-accent-300">{m.routes_recorded_in_planner()}</p>
		<h1 class="text-2xl font-semibold tracking-tight text-zinc-100">
			{route?.name ?? m.waas_title()}
		</h1>
	</div>
	<a
		href="/explore/routes"
		class="rounded-lg border border-zinc-800 px-3 py-2 text-sm text-accent-300"
		>{m.routes_back_to_catalog()}</a
	>
</div>
{#if failed}
	<p class="rounded-lg border border-zinc-800 bg-panel p-4 text-sm" role="alert">
		{m.routes_detail_failed()}
	</p>
{:else if !route}
	<p role="status" class="text-sm text-quiet">{m.routes_detail_loading()}</p>
{:else if !route.track.length}
	<p role="status" class="text-sm text-quiet">{m.routes_map_unavailable()}</p>
{:else}
	<div class="grid items-start gap-3 lg:grid-cols-[minmax(14rem,18rem)_minmax(0,1fr)]">
		<section class="order-2 space-y-3 rounded-xl border border-zinc-800 bg-panel p-4 lg:order-1">
			<p class="text-sm text-zinc-300">{m.routes_recorded_help()}</p>
			<p class="text-lg font-semibold tabular-nums text-zinc-100">{routeDistance(distance)}</p>
			<label class="flex items-center gap-2 text-sm">
				<input type="checkbox" class="accent-accent-500" bind:checked={reverse} />
				{m.routes_recorded_reverse()}
			</label>
			{#if canRotate}
				<label class="block text-sm">
					{m.routes_recorded_start()} <output>{start}%</output>
					<input
						type="range"
						min="0"
						max="95"
						step="1"
						bind:value={start}
						class="block w-full accent-accent-500"
						oninput={() => {
							from = 0;
							to = 100;
						}}
					/>
				</label>
			{/if}
			<details class="rounded-lg border border-zinc-800 p-2.5 text-sm">
				<summary class="cursor-pointer font-medium text-accent-300"
					>{m.routes_recorded_part()}</summary
				>
				<p class="my-2 text-xs text-quiet">{m.routes_recorded_segment_help()}</p>
				<label class="block">
					{m.routes_recorded_from()} <output>{from}%</output>
					<input
						type="range"
						min="0"
						max={to - 1}
						bind:value={from}
						class="block w-full accent-accent-500"
						oninput={() => (start = 0)}
					/>
				</label>
				<label class="mt-2 block">
					{m.routes_recorded_to()} <output>{to}%</output>
					<input
						type="range"
						min={from + 1}
						max="100"
						bind:value={to}
						class="block w-full accent-accent-500"
						oninput={() => (start = 0)}
					/>
				</label>
			</details>
			<div class="flex flex-wrap gap-2">
				{#if edited}
					<button
						type="button"
						onclick={exportEdits}
						disabled={!track.length}
						class="rounded-lg bg-accent-500 px-3 py-2 text-sm font-semibold text-on-accent disabled:opacity-50"
						>{m.routes_recorded_download()}</button
					>
					<button
						type="button"
						onclick={reset}
						class="rounded-lg border border-zinc-800 px-3 py-2 text-sm text-accent-300"
						>{m.routes_recorded_reset()}</button
					>
				{:else}
					<a
						href={recordedRoutePath(id) + '/gpx'}
						download
						class="rounded-lg bg-accent-500 px-3 py-2 text-sm font-semibold text-on-accent"
						>{m.routes_export_gpx()}</a
					>
				{/if}
			</div>
			{#if exportFailed}<p role="alert" class="text-sm text-red-300">
					{m.routes_export_failed()}
				</p>{/if}
			<a
				href={newPlan}
				class="block border-t border-zinc-800 pt-3 text-sm font-semibold text-accent-300 hover:underline"
				>{m.routes_recorded_clear()}</a
			>
		</section>
		<div class="order-1 min-w-0 space-y-3 lg:order-2">
			{#if track.length}
				<WaasRouteMap recorded={track} />
			{:else}
				<p role="status" class="rounded-lg border border-zinc-800 bg-panel p-4 text-sm">
					{m.routes_recorded_no_section()}
				</p>
			{/if}
			{#if hasElevation}
				<section class="rounded-xl border border-zinc-800 bg-panel p-4">
					<h2 class="mb-2 text-sm font-semibold">{m.waas_elevation_profile()}</h2>
					<ElevationProfile track={profile} />
				</section>
			{/if}
		</div>
	</div>
{/if}
