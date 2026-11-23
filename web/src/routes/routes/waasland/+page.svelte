<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteURL } from 'svelte/reactivity';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDistance, formatElevation, intlLocale } from '#lib/format.js';
	import ElevationProfile from '#lib/components/ElevationProfile.svelte';
	import WaasRouteMap from '#lib/components/WaasRouteMap.svelte';
	import { calculate, initialize } from '#lib/waas/service.js';
	import { runtime } from '#lib/waas/runtime.js';
	import { defaults, readOptions, optionsQuery } from '#lib/waas/settings.js';
	import type { Options, Place, Ride, Wind } from '#lib/waas/types.js';

	let options = $state<Options>({ ...defaults, places: [], zones: {} });
	let rides = $state<Ride[]>([]);
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
	const elevationTrack = $derived(
		ride?.points.map((point) => ({ distM: point[3], eleM: point[2] })) ?? []
	);
	const sections = $derived(
		ride
			? options.profile === 'climb'
				? ride.streets
				: ride.sections.filter(
						(section) => section.kind === options.profile && section.meters >= 80
					)
			: []
	);
	const control =
		'mt-1 min-h-10 w-full rounded border border-zinc-700 bg-panel px-3 py-2 text-sm text-zinc-100';
	const number = (value: number, digits = 0) =>
		new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: digits }).format(value);
	const profiles = $derived([
		{ id: 'climb' as const, label: m.waas_climb() },
		{ id: 'cobbles' as const, label: m.waas_cobbles() },
		{ id: 'flow' as const, label: m.waas_flow() },
		{ id: 'gravel' as const, label: m.waas_gravel() }
	]);

	function dirty(event?: Event) {
		if ((event?.target as HTMLInputElement | undefined)?.id === 'place-search') return;
		generation++;
		request?.abort();
		busy = false;
		changed = true;
		rides = [];
		variant = 0;
		notices = [];
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
		error = '';
		progress = m.waas_working();
		shared = '';
		try {
			const result = await calculate(selection, controller.signal, (message) => {
				if (ticket === generation) progress = message;
			});
			if (ticket !== generation) return;
			rides = result.rides;
			variant = 0;
			notices = result.warnings;
			changed = false;
			const url = new SvelteURL(window.location.href);
			url.search = optionsQuery(selection);
			await goto(url, { state: page.state, shallow: true, replace: true });
		} catch (cause) {
			if (ticket !== generation) return;
			error = cause instanceof Error ? cause.message : m.waas_error();
			rides = [];
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
		if (!ride || changed || busy) return;
		const { planner } = await initialize();
		const blob = new Blob([planner.gpx(ride, options.start)], {
			type: 'application/gpx+xml;charset=utf-8'
		});
		const url = URL.createObjectURL(blob),
			anchor = document.createElement('a');
		anchor.href = url;
		anchor.download = `Waasland_${options.profile}_${(ride.meters / 1000).toFixed(1)}km_${Math.round(ride.gain)}m.gpx`;
		document.body.append(anchor);
		anchor.click();
		anchor.remove();
		window.setTimeout(() => URL.revokeObjectURL(url), 2000);
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
		options = readOptions(new URLSearchParams(window.location.search));
		void initialize()
			.then(async ({ planner, core }) => {
				places = (await runtime()).createRideOptions(planner).places;
				zones = planner.climbZones;
				bounds = core.bounds;
				for (const zone of zones) options.zones[zone.id] ??= 'free';
				void refreshWind();
				void plan();
			})
			.catch((cause) => {
				error = cause instanceof Error ? cause.message : m.waas_error();
			});
		const timer = window.setInterval(() => {
			if (!document.hidden) void refreshWind();
		}, 15 * 60000);
		return () => {
			generation++;
			request?.abort();
			windRequest?.abort();
			window.clearInterval(timer);
		};
	});
</script>

<svelte:head><title>{m.waas_title()}</title></svelte:head>

<div class="mb-3 flex flex-wrap items-start justify-between gap-2 md:mb-5">
	<div>
		<h1 class="text-2xl font-semibold text-zinc-100">{m.waas_title()}</h1>
		<p class="mt-1 max-w-2xl text-sm text-zinc-400">{m.waas_intro()}</p>
	</div>
	<div class="flex flex-wrap gap-2">
		<button
			type="button"
			class="rounded border border-zinc-700 px-3 py-2 text-sm text-zinc-300 hover:border-accent-500"
			onclick={share}>{m.waas_share()}</button
		>
		<button
			type="button"
			disabled={!ride || busy || changed}
			class="rounded bg-accent-500 px-3 py-2 text-sm font-semibold text-on-accent disabled:opacity-40"
			onclick={download}>{m.waas_download()}</button
		>
	</div>
</div>
{#if shared}<p role="status" class="mb-4 text-sm text-accent-300">{shared}</p>{/if}

<div class="grid items-start gap-3 lg:grid-cols-[310px_minmax(0,1fr)] lg:grid-rows-[auto_auto_1fr]">
	<form
		class="order-2 space-y-3 rounded-xl border border-zinc-800 bg-panel p-3 sm:p-4 lg:order-none lg:col-start-1 lg:row-start-1 lg:row-span-3"
		onchange={dirty}
		onsubmit={(event) => {
			event.preventDefault();
			void plan();
		}}
	>
		<fieldset>
			<legend class="mb-2 text-sm font-semibold text-zinc-200">{m.waas_profile()}</legend>
			<div class="grid grid-cols-2 gap-2">
				{#each profiles as profile (profile.id)}<label
						class="cursor-pointer rounded border px-3 py-2 text-sm {options.profile === profile.id
							? 'border-accent-500 bg-accent-500/15 text-accent-300'
							: 'border-zinc-700 text-zinc-300'}"
						><input
							class="sr-only"
							type="radio"
							name="profile"
							value={profile.id}
							bind:group={options.profile}
						/>{profile.label}</label
					>{/each}
			</div>
		</fieldset>
		<label class="block text-sm text-zinc-300" for="departure"
			>{m.waas_departure()}<select
				id="departure"
				class={control}
				bind:value={options.start}
				onchange={() => {
					dirty();
					void refreshWind();
				}}
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
		<section
			class="rounded border border-zinc-800 bg-accent-500/5 p-3"
			aria-label={m.waas_weather()}
		>
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
						{m.waas_weather_gusts({ speed: number(wind.gusts) })} · {new Intl.DateTimeFormat(
							intlLocale(),
							{ hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Brussels' }
						).format(wind.timestamp)}
					</p>{:else if windFailed}{m.waas_weather_unavailable()}{/if}
			</div>
			<a
				href="https://open-meteo.com/"
				target="_blank"
				rel="noopener noreferrer"
				class="mt-2 block text-xs text-zinc-500 underline">{m.waas_weather_source()}</a
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
							type="number"
							class={control}
							min="0"
							max="5000"
							bind:value={options.minGain}
						/></label
					><label class="text-sm" for="gain-max"
						>{m.waas_max_gain()}<input
							id="gain-max"
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
		<button
			type="submit"
			class="min-h-11 w-full rounded bg-accent-500 px-4 py-2 font-semibold text-on-accent disabled:opacity-50"
			disabled={busy}>{busy ? m.waas_working() : m.waas_generate()}</button
		>
		{#if busy}<p role="status" class="text-sm text-zinc-400">{progress}</p>{:else if changed}<p
				role="status"
				class="text-sm text-amber-200"
			>
				{m.waas_dirty()}
			</p>{/if}
		{#if ride && !busy && !changed}<a
				href="#planner-map"
				class="flex min-h-11 items-center justify-center rounded-lg border border-zinc-800 text-sm font-semibold text-accent-300 lg:hidden"
				>{m.waas_view_route()}</a
			>{/if}
		{#if error}<p role="alert" class="rounded bg-red-950 p-3 text-sm text-red-300">{error}</p>{/if}
	</form>
	<section
		class="order-3 min-w-0 rounded-xl border border-zinc-800 bg-panel p-3 sm:p-4 lg:order-none lg:col-start-2 lg:row-start-1"
	>
		<h2 class="text-lg font-semibold text-zinc-100">
			{ride?.chunks[0]?.scheldeRound
				? m.waas_schelde_round()
				: profiles.find((profile) => profile.id === options.profile)?.label}
		</h2>
		<div class="mt-3 grid grid-cols-3 gap-3">
			<div>
				<p class="text-xs text-zinc-400">{m.waas_distance()}</p>
				<strong class="text-xl text-zinc-100">{ride ? formatDistance(ride.meters) : '—'}</strong>
			</div>
			<div>
				<p class="text-xs text-zinc-400">{m.waas_estimated_gain()}</p>
				<strong class="text-xl text-zinc-100">{ride ? formatElevation(ride.gain) : '—'}</strong>
			</div>
			<div>
				<p class="text-xs text-zinc-400">
					{options.profile === 'climb'
						? m.waas_climb_density()
						: options.profile === 'flow'
							? m.waas_cycle_distance()
							: m.waas_surface_distance()}
				</p>
				<strong class="text-xl text-zinc-100"
					>{ride
						? options.profile === 'climb'
							? `${number((1000 * ride.gain) / ride.meters, 1)} m/km`
							: formatDistance(
									options.profile === 'flow'
										? Object.values(ride.spine).reduce((sum, value) => sum + value, 0)
										: ride.matchedSurface
								)
						: '—'}</strong
				>
			</div>
		</div>
		{#if ride && rides.length > 1}<div
				class="mt-4 flex items-center justify-between border-t border-zinc-800 pt-3 text-sm"
			>
				<span class="text-zinc-400"
					>{m.waas_route_number({ current: variant + 1, total: rides.length })}</span
				><button
					type="button"
					class="rounded border border-zinc-700 px-3 py-1.5 text-accent-300 disabled:opacity-40"
					disabled={changed || busy}
					onclick={() => (variant = (variant + 1) % rides.length)}>{m.waas_next()}</button
				>
			</div>{/if}
	</section>
	<div id="planner-map" class="order-1 min-w-0 lg:order-none lg:col-start-2 lg:row-start-2">
		<WaasRouteMap {ride} {bounds} />
	</div>
	<div class="order-4 min-w-0 space-y-3 lg:order-none lg:col-start-2 lg:row-start-3">
		{#if ride}
			{#each notices as notice (notice)}<p
					role="status"
					class="rounded border border-amber-700 bg-amber-950 p-3 text-sm text-amber-200"
				>
					{notice}
				</p>{/each}
			{#if Math.abs(ride.meters - options.length * 1000) > options.tolerance * 1000}<p
					role="status"
					class="rounded border border-amber-700 bg-amber-950 p-3 text-sm text-amber-200"
				>
					{m.waas_distance_warning()}
				</p>{/if}
			{#if options.goal === 'range' && (ride.gain < options.minGain || ride.gain > options.maxGain)}<p
					role="status"
					class="text-sm text-amber-200"
				>
					{m.waas_gain_warning()}
				</p>{/if}
			<section class="rounded-lg border border-zinc-800 bg-panel p-3 sm:p-4">
				<h2 class="mb-4 text-sm font-semibold text-zinc-200">{m.waas_elevation_profile()}</h2>
				<ElevationProfile track={elevationTrack} />
				<p class="mt-3 text-xs leading-relaxed text-zinc-400">{m.waas_estimate_help()}</p>
			</section>
			<section class="rounded-lg border border-zinc-800 bg-panel p-3 sm:p-4">
				<h2 class="text-sm font-semibold text-zinc-200">{m.waas_sections()}</h2>
				<p class="mt-2 text-sm text-zinc-400">
					{m.waas_reuse({
						distance: formatDistance(ride.reverse + ride.repeat)
					})}{options.profile === 'flow' ? ` · ${m.waas_stops({ count: ride.events.length })}` : ''}
				</p>
				<ul class="mt-3 divide-y divide-zinc-800">
					{#each sections.slice(0, 16) as section, index (index)}<li
							class="flex justify-between gap-3 py-2 text-sm"
						>
							<span class="text-zinc-200"
								>{section.name}{section.passes > 1
									? ` · ${m.waas_passages_label({ count: section.passes })}`
									: ''}</span
							><span class="shrink-0 text-zinc-400"
								>{options.profile === 'climb'
									? formatElevation(section.gain)
									: formatDistance(section.meters)}</span
							>
						</li>{/each}
				</ul>
			</section>
		{:else if !busy}<p class="text-sm text-zinc-400">{m.waas_idle()}</p>{/if}
		<p class="text-sm leading-relaxed text-zinc-400">{m.waas_import_help()}</p>
	</div>
</div>
