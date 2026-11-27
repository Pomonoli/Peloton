<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteURL } from 'svelte/reactivity';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { m } from '#lib/paraglide/messages.js';
	import { formatClockTime, intlLocale } from '#lib/format.js';
	import Icon from '#lib/components/Icon.svelte';
	import PlannerElevation from '#lib/components/PlannerElevation.svelte';
	import PlannerStats from '#lib/components/PlannerStats.svelte';
	import {
		routeDistance,
		routeElevation,
		type RecordedTrack,
		type RoutePoint
	} from '#lib/recorded-routes.js';
	import WaasRouteMap from '#lib/components/WaasRouteMap.svelte';
	import { downloadGPX } from '#lib/waas/download.js';
	import { calculate, initialize } from '#lib/waas/service.js';
	import { runtime } from '#lib/waas/runtime.js';
	import { defaults, readOptions, optionsQuery } from '#lib/waas/settings.js';
	import type { Options, Place, Ride, Wind } from '#lib/waas/types.js';

	let options = $state<Options>({ ...defaults, places: [], zones: {} });
	let rides = $state.raw<Ride[]>([]);
	let plannedOptions = $state.raw<Options>();
	let point = $state.raw<RoutePoint | null>(null);
	let exportFailed = $state(false);
	let variant = $state(0);
	let busy = $state(false);
	let changed = $state(false);
	let error = $state('');
	let progress = $state('');
	let notices = $state<string[]>([]);
	let shared = $state('');
	let search = $state('');
	let places = $state<Place[]>([]);
	let zones = $state<{ id: string; name: string }[]>([]);
	let bounds = $state<[[number, number], [number, number]] | null>(null);
	let wind = $state<Wind | null>(null);
	let windBusy = $state(false);
	let windFailed = $state(false);
	let request: AbortController | undefined;
	let windRequest: AbortController | undefined;
	let generation = 0;
	const ride = $derived(rides[variant] ?? null);
	const matchingPlaces = $derived(
		places.filter((place) => place.name.toLowerCase().includes(search.toLowerCase()))
	);
	const track: RecordedTrack = $derived(
		ride?.points.length
			? [
					ride.points.map(([lat, lon, elevation_m, distance_m]) => ({
						lat,
						lon,
						elevation_m,
						distance_m
					}))
				]
			: []
	);
	const sections = $derived(
		ride?.profile === 'climb'
			? ride.streets
			: (ride?.sections.filter(
					(section) => section.kind === ride.profile && section.meters >= 80
				) ?? [])
	);
	const control = 'field mt-1 w-full';
	const number = (value: number, digits = 0) =>
		new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: digits }).format(value);
	const profiles = $derived([
		{ id: 'climb' as const, label: m.waas_climb(), icon: 'segments' as const },
		{ id: 'cobbles' as const, label: m.waas_cobbles(), icon: 'stats' as const },
		{ id: 'flow' as const, label: m.waas_flow(), icon: 'speed' as const },
		{ id: 'gravel' as const, label: m.waas_gravel(), icon: 'gear' as const }
	]);

	function dirty(event: Event) {
		if ((event.target as HTMLElement | null)?.id === 'place-search') return;
		generation++;
		request?.abort();
		busy = false;
		changed = true;
		exportFailed = false;
		error = '';
		shared = '';
	}

	async function plan() {
		if (
			options.profile === 'climb' &&
			options.goal === 'range' &&
			options.minGain > options.maxGain
		) {
			error = m.waas_range_error();
			return;
		}
		request?.abort();
		const controller = new AbortController();
		request = controller;
		const ticket = ++generation;
		const selection: Options = {
			...options,
			goal: options.profile === 'climb' ? options.goal : 'profile',
			places: [...options.places],
			zones: { ...options.zones }
		};
		busy = true;
		exportFailed = false;
		error = '';
		progress = m.waas_working();
		shared = '';
		try {
			const result = await calculate(selection, controller.signal, (message) => {
				if (ticket === generation) progress = message;
			});
			if (ticket !== generation) return;
			rides = result.rides;
			plannedOptions = selection;
			variant = 0;
			notices = result.warnings;
			changed = false;
			const url = new SvelteURL(window.location.href);
			url.search = optionsQuery(selection);
			await goto(url, { state: page.state, shallow: true, replace: true });
		} catch (cause) {
			if (ticket !== generation) return;
			error = cause instanceof Error ? cause.message : m.waas_error();
		} finally {
			if (ticket === generation) {
				busy = false;
				progress = '';
			}
		}
	}

	async function refreshWind() {
		windRequest?.abort();
		const controller = new AbortController();
		windRequest = controller;
		const timer = window.setTimeout(() => controller.abort(), 12000);
		windBusy = true;
		windFailed = false;
		try {
			const { planner } = await initialize();
			const result = await (
				await runtime()
			)
				.createRideOptions(planner)
				.weather(options.start, controller.signal);
			if (windRequest === controller) wind = result;
		} catch {
			if (windRequest === controller) {
				wind = null;
				windFailed = true;
			}
		} finally {
			window.clearTimeout(timer);
			if (windRequest === controller) windBusy = false;
		}
	}

	async function download() {
		if (!ride || !plannedOptions || changed || busy) return;
		const selected = ride;
		const { start, profile } = plannedOptions;
		exportFailed = false;
		try {
			const { planner } = await initialize();
			downloadGPX(
				planner.gpx(selected, start),
				`Waasland_${profile}_${(selected.meters / 1000).toFixed(1)}km_${Math.round(selected.gain)}m.gpx`
			);
		} catch {
			exportFailed = true;
		}
	}

	async function share() {
		const url = new SvelteURL(window.location.href);
		url.search = optionsQuery(options);
		url.hash = '';
		try {
			await navigator.clipboard.writeText(url.href);
			shared = m.waas_copied();
		} catch {
			shared = m.waas_copy_failed();
		}
	}

	onMount(() => {
		let mounted = true;
		options = readOptions(new URLSearchParams(window.location.search));
		void initialize()
			.then(async ({ planner, core }) => {
				const { createRideOptions } = await runtime();
				if (!mounted) return;
				places = createRideOptions(planner).places;
				zones = planner.climbZones;
				bounds = core.bounds;
				for (const zone of zones) options.zones[zone.id] ??= 'free';
				void refreshWind();
				if (!busy && !ride) void plan();
			})
			.catch((cause) => {
				if (!mounted) return;
				error = cause instanceof Error ? cause.message : m.waas_error();
			});
		const timer = window.setInterval(() => {
			if (!document.hidden) void refreshWind();
		}, 15 * 60000);
		return () => {
			mounted = false;
			generation++;
			request?.abort();
			windRequest?.abort();
			window.clearInterval(timer);
		};
	});
</script>

<svelte:head><title>{m.waas_title()}</title></svelte:head>

{#snippet primaryAction(extra: string)}
	{#if ride && !busy && !changed}
		<button type="button" class="action action-primary {extra}" onclick={download}>
			<Icon name="download" size={17} />{m.waas_download()}
		</button>
	{:else}
		<button
			type="submit"
			form="planner-settings"
			class="action action-primary {extra}"
			disabled={busy}
		>
			<Icon name="planner" size={17} />{busy ? m.waas_working() : m.waas_generate()}
		</button>
	{/if}
{/snippet}

<div class="planner-has-actions">
	<header class="mb-3 sm:mb-4">
		<div class="flex flex-wrap items-end justify-between gap-3">
			<h1 class="page-title">{m.waas_title()}</h1>
			<div class="hidden flex-wrap gap-2 md:flex">
				<button type="button" class="action" onclick={share}
					><Icon name="share" size={17} />{m.waas_share()}</button
				>
				{@render primaryAction('')}
			</div>
		</div>
	</header>
	{#if shared}<p role="status" class="mb-3 text-sm text-accent-300">{shared}</p>{/if}
	{#if exportFailed}<p role="alert" class="mb-3 text-sm text-red-300">
			{m.routes_export_failed()}
		</p>{/if}

	<div class="planner-workspace">
		<div class="relative min-w-0 lg:col-start-1 lg:row-start-1" id="planner-map">
			<WaasRouteMap {track} {bounds} {point} visits={ride?.visits ?? []} />
			{#if busy || changed}
				<p
					role="status"
					class="pointer-events-none absolute top-3 left-1/2 max-w-[62%] -translate-x-1/2 rounded-lg border border-zinc-800 bg-panel px-3 py-2 text-center text-xs font-semibold text-zinc-300 shadow-sm"
				>
					{busy ? progress : m.waas_dirty()}
				</p>
			{/if}
		</div>
		<div class="contents lg:grid lg:gap-3 planner-side">
			{#if ride}
				<div class="order-1">
					<PlannerStats distance={ride.meters} elevation={ride.elevation} />
				</div>
			{/if}
			<form
				id="planner-settings"
				class="surface order-3 space-y-3 p-3 sm:p-4 lg:order-2"
				oninput={dirty}
				onsubmit={(event) => {
					event.preventDefault();
					void plan();
				}}
			>
				<h2 class="section-title">{m.planner_settings()}</h2>
				<fieldset>
					<legend class="mb-2 text-sm font-semibold text-zinc-200">{m.waas_profile()}</legend>
					<div class="grid grid-cols-2 gap-1.5">
						{#each profiles as profile (profile.id)}
							<label
								class="flex min-h-11 cursor-pointer items-center gap-1.5 rounded-lg border px-2 py-1.5 text-xs font-semibold focus-within:outline-2 focus-within:outline-accent-500 {options.profile ===
								profile.id
									? 'border-accent-500 bg-accent-500/12 text-accent-300'
									: 'border-zinc-800 text-zinc-300 hover:bg-zinc-950'}"
							>
								<input
									class="sr-only"
									type="radio"
									name="profile"
									value={profile.id}
									bind:group={options.profile}
								/>
								<Icon name={profile.icon} size={16} />
								{profile.label}
							</label>
						{/each}
					</div>
				</fieldset>

				<label class="block text-sm text-zinc-300" for="departure"
					>{m.waas_departure()}<select
						id="departure"
						class={control}
						bind:value={options.start}
						onchange={refreshWind}
						><option value="sn">{m.waas_start_sn()}</option><option value="wm"
							>{m.waas_start_wm()}</option
						><option value="ti">{m.waas_start_ti()}</option><option value="te"
							>{m.waas_start_te()}</option
						></select
					></label
				>
				<div class="grid grid-cols-2 gap-3">
					<label class="text-sm text-zinc-300" for="distance"
						>{m.waas_distance()}<input
							id="distance"
							class={control}
							type="number"
							min="15"
							max="200"
							step="0.5"
							required
							bind:value={options.length}
						/></label
					><label class="text-sm text-zinc-300" for="margin"
						>{m.waas_margin()}<select id="margin" class={control} bind:value={options.tolerance}
							>{#each [0.5, 2, 5, 10] as margin (margin)}<option value={margin}
									>± {number(margin, 1)}</option
								>{/each}</select
						></label
					>
				</div>
				<label class="block text-sm text-zinc-300" for="direction"
					>{m.waas_direction()}<select id="direction" class={control} bind:value={options.direction}
						><option value="auto">{m.waas_direction_auto()}</option><option value="cw"
							>{m.waas_direction_cw()}</option
						><option value="ccw">{m.waas_direction_ccw()}</option></select
					></label
				>
				<section class="border-y border-zinc-800 py-3" aria-label={m.waas_weather()}>
					<div class="flex items-center justify-between gap-2">
						<span class="text-sm font-medium text-zinc-200">{m.waas_weather()}</span><button
							type="button"
							class="text-sm text-accent-400 underline disabled:opacity-40"
							onclick={refreshWind}
							disabled={windBusy}>{m.waas_weather_refresh()}</button
						>
					</div>
					<div class="mt-2 text-sm text-zinc-300" role="status">
						{#if windBusy}{m.waas_weather_loading()}{:else if wind}<span
								aria-hidden="true"
								class="mr-2 inline-block text-xl text-accent-400"
								style:transform={`rotate(${wind.direction}deg)`}>↓</span
							>{m.waas_weather_from({ direction: wind.compass, speed: number(wind.speed, 1) })}
							<p class="mt-1 text-xs text-zinc-400">
								{m.waas_weather_gusts({ speed: number(wind.gusts) })} · {formatClockTime(
									wind.timestamp,
									{ timeZone: 'Europe/Brussels' }
								)}
							</p>{:else if windFailed}{m.waas_weather_unavailable()}{/if}
					</div>
					<a
						href="https://open-meteo.com/"
						target="_blank"
						rel="noopener noreferrer"
						class="mt-1 block text-xs text-zinc-500 underline">{m.waas_weather_source()}</a
					>
				</section>
				<details class="text-sm text-zinc-300">
					<summary class="cursor-pointer font-medium"
						>{m.waas_places()} {options.places.length ? `(${options.places.length})` : ''}</summary
					><label class="sr-only" for="place-search">{m.waas_place_search()}</label><input
						id="place-search"
						type="search"
						class={control}
						placeholder={m.waas_place_search()}
						bind:value={search}
					/>
					<div class="mt-2 max-h-60 space-y-1 overflow-auto">
						{#each matchingPlaces as place (place.id)}<label
								class="flex min-h-9 cursor-pointer items-center gap-2"
								><input
									type="checkbox"
									value={place.id}
									bind:group={options.places}
									class="accent-accent-500"
								/>{place.name}</label
							>{/each}{#if !matchingPlaces.length}<p>{m.waas_no_places()}</p>{/if}
					</div>
					<p class="mt-2 text-xs leading-relaxed text-zinc-400">{m.waas_places_help()}</p>
				</details>
				{#if options.profile === 'climb'}
					<label class="block text-sm text-zinc-300" for="goal"
						>{m.waas_goal()}<select id="goal" class={control} bind:value={options.goal}
							><option value="max">{m.waas_max()}</option><option value="varied"
								>{m.waas_varied()}</option
							><option value="range">{m.waas_range()}</option></select
						></label
					>
					{#if options.goal === 'range'}<div class="grid grid-cols-2 gap-2">
							<label class="text-sm" for="gain-min"
								>{m.waas_min_gain()}<input
									id="gain-min"
									required
									type="number"
									class={control}
									min="0"
									max="5000"
									bind:value={options.minGain}
								/></label
							><label class="text-sm" for="gain-max"
								>{m.waas_max_gain()}<input
									id="gain-max"
									required
									type="number"
									class={control}
									min="0"
									max="5000"
									bind:value={options.maxGain}
								/></label
							>
						</div>{/if}
					<details class="text-sm text-zinc-300">
						<summary class="cursor-pointer font-medium">{m.waas_climb_options()}</summary>
						<div class="mt-3 space-y-2">
							{#each zones as zone (zone.id)}<label
									class="grid grid-cols-[1fr_100px] items-center gap-2"
									for={'zone-' + zone.id}
									>{zone.name}<select
										id={'zone-' + zone.id}
										class={control}
										bind:value={options.zones[zone.id]}
										><option value="free">{m.waas_zone_free()}</option><option value="include"
											>{m.waas_zone_include()}</option
										><option value="avoid">{m.waas_zone_avoid()}</option></select
									></label
								>{/each}<label class="block" for="passages"
								>{m.waas_passages()}<select id="passages" class={control} bind:value={options.laps}
									>{#each [1, 2, 3] as count (count)}<option value={count}>{count}</option
										>{/each}</select
								></label
							><label class="block" for="surface"
								>{m.waas_surface()}<select id="surface" class={control} bind:value={options.surface}
									><option value="smooth">{m.waas_surface_smooth()}</option><option value="any"
										>{m.waas_surface_any()}</option
									></select
								></label
							>
						</div>
					</details>
				{/if}

				{#if ride && !busy && !changed}
					<button type="submit" class="action w-full"
						><Icon name="reset" size={17} />{m.planner_recalculate()}</button
					>
				{/if}
				{#if error}<p role="alert" class="rounded-lg bg-red-950 p-3 text-sm text-red-300">
						{error}
					</p>{/if}
			</form>

			{#if ride && rides.length > 1}
				<section
					class="surface order-1 flex items-center justify-between gap-2 px-3 py-2 text-xs sm:px-4"
				>
					<span class="font-medium text-zinc-400"
						>{m.waas_route_number({ current: variant + 1, total: rides.length })}</span
					>
					<button
						type="button"
						class="action"
						disabled={changed || busy}
						onclick={() => (variant = (variant + 1) % rides.length)}
					>
						{m.waas_next()}<Icon name="chevron" size={15} />
					</button>
				</section>
			{/if}
		</div>

		{#if ride && plannedOptions}
			<div class="order-2 min-w-0 space-y-3 lg:col-start-1 lg:row-start-2">
				<PlannerElevation {track} elevation={ride.elevation} onpoint={(next) => (point = next)} />
				{#each notices as notice (notice)}
					<p role="status" class="rounded-lg bg-amber-950 p-3 text-sm text-amber-200">{notice}</p>
				{/each}
				{#if Math.abs(ride.meters - plannedOptions.length * 1000) > plannedOptions.tolerance * 1000}
					<p role="status" class="rounded-lg bg-amber-950 p-3 text-sm text-amber-200">
						{m.waas_distance_warning()}
					</p>
				{/if}
				{#if plannedOptions.goal === 'range' && (ride.gain < plannedOptions.minGain || ride.gain > plannedOptions.maxGain)}
					<p role="status" class="rounded-lg bg-amber-950 p-3 text-sm text-amber-200">
						{m.waas_gain_warning()}
					</p>
				{/if}
			</div>
			<details class="surface order-4 min-w-0 p-3 sm:p-4 lg:col-start-1 lg:row-start-3">
				<summary class="cursor-pointer font-semibold text-zinc-100"
					>{m.planner_route_details()}</summary
				>
				{#if ride.chunks[0]?.scheldeRound}
					<h2 class="section-title mt-3">{m.waas_schelde_round()}</h2>
				{/if}
				<div class="mt-3 flex flex-wrap gap-x-6 gap-y-1 text-sm text-zinc-400">
					<span>{m.waas_reuse({ distance: routeDistance(ride.reverse + ride.repeat) })}</span>
					{#if ride.profile === 'climb'}
						<span
							>{m.waas_climb_density()} · {number((1000 * ride.gain) / ride.meters, 1)} m/km</span
						>
					{:else if ride.profile === 'flow'}
						<span
							>{m.waas_cycle_distance()} · {routeDistance(
								Object.values(ride.spine).reduce((sum, value) => sum + value, 0)
							)}</span
						>
						<span>{m.waas_stops({ count: ride.events.length })}</span>
					{:else}
						<span>{m.waas_surface_distance()} · {routeDistance(ride.matchedSurface)}</span>
					{/if}
				</div>
				{#if sections.length}
					<h2 class="mt-4 text-sm font-semibold">{m.waas_sections()}</h2>
					<ul class="mt-1 divide-y divide-zinc-800">
						{#each sections.slice(0, 16) as section, index (index)}
							<li class="flex justify-between gap-3 py-2 text-sm">
								<span class="min-w-0 wrap-anywhere text-zinc-200"
									>{section.name}{section.passes > 1
										? ` · ${m.waas_passages_label({ count: section.passes })}`
										: ''}</span
								>
								<span class="metric-value shrink-0 text-zinc-400"
									>{ride.profile === 'climb'
										? routeElevation(section.gain)
										: routeDistance(section.meters)}</span
								>
							</li>
						{/each}
					</ul>
				{/if}
				<p class="mt-3 text-xs leading-relaxed text-zinc-400">{m.waas_estimate_help()}</p>
			</details>
		{/if}
	</div>
	<div class="planner-mobile-actions">
		<button
			type="button"
			class="action"
			aria-label={m.waas_share()}
			title={m.waas_share()}
			onclick={share}><Icon name="share" size={18} /></button
		>
		{@render primaryAction('flex-1')}
	</div>
</div>
