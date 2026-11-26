<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { downloadGPX } from '#lib/waas/download.js';
	import { recordedElevation } from '#lib/waas/metrics.js';
	import {
		canMoveStart,
		cutTrack,
		finiteElevation,
		moveTrackStart,
		rebaseTrack,
		reverseTrack,
		trackDistance
	} from '#lib/waas/recorded.js';
	import {
		recordedRoutePath,
		routeData,
		routeDistance,
		routeKind,
		type RouteDetail,
		type RoutePoint
	} from '#lib/recorded-routes.js';
	import Icon from './Icon.svelte';
	import WaasRouteMap from './WaasRouteMap.svelte';
	import PlannerElevation from './PlannerElevation.svelte';
	import PlannerStats from './PlannerStats.svelte';
	import PlannerTrim from './PlannerTrim.svelte';

	let { id }: { id: string } = $props();
	let route = $state.raw<RouteDetail>();
	let measureElevation = $state<Awaited<ReturnType<typeof recordedElevation>>>();
	let failed = $state(false);
	let exportFailed = $state(false);
	let reverse = $state(false);
	let start = $state(0);
	let from = $state(0);
	let trimEnd = $state<number | null>(null);
	let point = $state.raw<RoutePoint | null>(null);
	const original = $derived(route ? rebaseTrack(route.track) : []);
	const length = $derived(trackDistance(original));
	const to = $derived(trimEnd ?? length);
	const trimmed = $derived(from > 0 || to < length);
	const edited = $derived(reverse || start > 0 || trimmed);
	const canRotate = $derived(route?.type === 'loop' && canMoveStart(original));
	const rotated = $derived(start > 0 ? moveTrackStart(original, start) : original);
	const kept = $derived(trimmed ? rebaseTrack(cutTrack(rotated, from, to)) : rotated);
	const excluded = $derived(
		trimmed ? [...cutTrack(rotated, 0, from), ...cutTrack(rotated, to, length)] : []
	);
	const track = $derived(reverse ? reverseTrack(kept) : kept);
	const distance = $derived(trackDistance(track));
	const elevation = $derived(measureElevation?.(track, finiteElevation) ?? null);
	const newPlan = $derived(
		'/routes/waasland' +
			(route ? '?km=' + Math.min(200, Math.max(15, Math.round(distance / 500) / 2)) : '')
	);

	function gpxCoordinate(value: number) {
		return value.toFixed(7).replace(/0+$/, '').replace(/\.$/, '');
	}

	function reset() {
		reverse = false;
		start = from = 0;
		trimEnd = null;
		exportFailed = false;
	}

	$effect(() => {
		const controller = new AbortController();
		void Promise.all([
			routeData<RouteDetail>(recordedRoutePath(id), controller.signal),
			recordedElevation()
		])
			.then(([result, metrics]) => {
				if (controller.signal.aborted) return;
				route = result;
				measureElevation = metrics;
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
			line.map((p) => [gpxCoordinate(p.lat), gpxCoordinate(p.lon), finiteElevation(p)])
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

{#snippet exportAction(extra: string)}
	{#if edited}
		<button
			type="button"
			onclick={exportEdits}
			disabled={!track.length}
			class="action action-primary {extra}"
		>
			<Icon name="download" size={17} />{m.routes_recorded_download()}
		</button>
	{:else}
		<a href={recordedRoutePath(id) + '/gpx'} download class="action action-primary {extra}">
			<Icon name="download" size={17} />{m.routes_export_gpx()}
		</a>
	{/if}
{/snippet}

<div class:planner-has-actions={track.length > 0}>
	<header class="mb-3 sm:mb-4">
		<div class="mb-2 flex flex-wrap items-center justify-between gap-x-3">
			<a
				href="/explore/routes"
				class="inline-flex min-h-8 items-center gap-1 text-xs font-semibold text-accent-300 hover:underline"
			>
				<Icon name="back" size={15} />{m.routes_back_to_catalog()}
			</a>
			<a
				href={newPlan}
				class="inline-flex min-h-8 items-center gap-1 text-xs font-semibold text-accent-300 hover:underline"
			>
				<Icon name="plus" size={15} />{m.planner_new()}
			</a>
		</div>
		<div class="flex flex-wrap items-end justify-between gap-3">
			<div class="min-w-0">
				<h1 class="page-title">{route?.name ?? m.waas_title()}</h1>
				{#if route}<p class="mt-1 text-xs text-zinc-400">
						{m.routes_recorded_in_planner()} · {routeKind(route.type)}
					</p>{/if}
			</div>
			{#if track.length}{@render exportAction('max-md:hidden')}{/if}
		</div>
	</header>
	{#if failed}
		<p class="surface p-4 text-sm" role="alert">{m.routes_detail_failed()}</p>
	{:else if !route}
		<p role="status" class="text-sm text-quiet">{m.routes_detail_loading()}</p>
	{:else if !original.length}
		<p role="status" class="text-sm text-quiet">{m.routes_map_unavailable()}</p>
	{:else}
		<div class="planner-workspace">
			<div class="min-w-0 lg:col-start-1 lg:row-start-1">
				<WaasRouteMap {track} reference={original} {excluded} {point} />
			</div>
			<div class="contents lg:grid lg:gap-3 planner-side">
				<div class="order-1">
					<PlannerStats {distance} {elevation} />
				</div>
				<section
					class="surface order-3 space-y-4 p-3 sm:p-4 lg:order-2"
					aria-label={m.planner_edit()}
				>
					<div class="flex items-center justify-between gap-2">
						<h2 class="section-title">{m.planner_edit()}</h2>
						{#if edited}
							<button
								type="button"
								onclick={reset}
								class="inline-flex min-h-9 items-center gap-1 px-1 text-xs font-semibold text-accent-300 hover:underline"
							>
								<Icon name="reset" size={15} />{m.routes_recorded_reset()}
							</button>
						{/if}
					</div>
					{#if length > 0}
						<PlannerTrim
							{length}
							from={reverse ? length - to : from}
							to={reverse ? length - from : to}
							onchange={(low, high) => {
								from = reverse ? length - high : low;
								trimEnd = reverse ? length - low : high;
							}}
						/>
						<div
							class="-mt-2 flex items-center justify-between gap-2 border-b border-zinc-800 pb-3 text-xs text-zinc-400"
						>
							{m.planner_trim_distance()}
							<strong class="text-sm tabular-nums text-zinc-100">{routeDistance(distance)}</strong>
						</div>
					{/if}
					<button
						type="button"
						aria-pressed={reverse}
						onclick={() => (reverse = !reverse)}
						class="action w-full justify-start aria-pressed:border-accent-500 aria-pressed:bg-accent-500/10 aria-pressed:text-accent-300"
					>
						<Icon name="swap" size={18} />{m.routes_recorded_reverse()}
					</button>
					{#if canRotate}
						<label class="block text-xs text-zinc-300">
							<span class="flex items-center justify-between gap-2"
								>{m.routes_recorded_start()} <strong>{routeDistance(start)}</strong></span
							>
							<input
								type="range"
								min="0"
								max={length * 0.95}
								step="any"
								bind:value={start}
								aria-valuetext={routeDistance(start)}
								class="block h-10 w-full accent-accent-500"
							/>
						</label>
					{/if}
				</section>
			</div>
			<div class="order-2 min-w-0 lg:col-start-1 lg:row-start-2">
				<PlannerElevation {track} {elevation} onpoint={(next) => (point = next)} />
			</div>
		</div>
		{#if exportFailed}<p role="alert" class="mt-3 text-sm text-red-300">
				{m.routes_export_failed()}
			</p>{/if}
		{#if track.length}
			<div class="planner-mobile-actions">
				{#if edited}
					<button
						type="button"
						onclick={reset}
						class="action"
						aria-label={m.routes_recorded_reset()}
						title={m.routes_recorded_reset()}><Icon name="reset" size={18} /></button
					>
				{/if}
				{@render exportAction('flex-1')}
			</div>
		{/if}
	{/if}
</div>
