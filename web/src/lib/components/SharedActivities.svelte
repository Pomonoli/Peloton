<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { SvelteURLSearchParams } from 'svelte/reactivity';
	import { formatDate, formatDateOnly, formatDuration, intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';

	type Activity = { id: string; title: string; rider: string };
	type Ride = { activities: Activity[]; date: string; distance_m: number; duration_s: number };
	type Encounter = { activities: Activity[]; time: string; lat: number; lon: number };
	type Result = { rides: Ride[]; encounters: Encounter[]; shared_distance_m: number };
	type Metric = 'rides' | 'distance' | 'encounters';

	let { pair }: { pair: string } = $props();
	let result = $state.raw<Result>();
	let loading = $state(true);
	let failed = $state(false);
	let retry = $state(0);
	let metric = $state<Metric | null>(null);
	let map = $state<string>();
	const from = $derived(page.url.searchParams.get('from') ?? '');
	const to = $derived(page.url.searchParams.get('to') ?? '');
	const invalidRange = $derived(!!from && !!to && from > to);
	const number = $derived(new Intl.NumberFormat(intlLocale()));
	const kilometers = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 2 }));

	const stats = $derived([
		{
			id: 'rides',
			title: m.comparison_shared_rides(),
			value: number.format(result?.rides.length ?? 0)
		},
		{
			id: 'distance',
			title: m.comparison_shared_distance(),
			value: distance(result?.shared_distance_m ?? 0)
		},
		{
			id: 'encounters',
			title: m.comparison_crossed_paths(),
			value: number.format(result?.encounters.length ?? 0)
		}
	] satisfies { id: Metric; title: string; value: string }[]);

	$effect(() => {
		const query = new SvelteURLSearchParams(pair);
		if (from) query.set('from', from);
		if (to) query.set('to', to);
		const attempt = retry;
		result = undefined;
		map = undefined;
		failed = false;
		loading = !invalidRange;
		if (invalidRange) return;
		const request = new AbortController();
		void (async () => {
			try {
				const res = await fetch(`/api/head-to-head/shared?${query}`, { signal: request.signal });
				if (!res.ok) throw new Error();
				const next: Result = await res.json();
				if (!Array.isArray(next.rides) || !Array.isArray(next.encounters)) throw new Error();
				if (!request.signal.aborted && retry === attempt) result = next;
			} catch {
				if (!request.signal.aborted) failed = true;
			} finally {
				if (!request.signal.aborted) loading = false;
			}
		})();
		return () => request.abort();
	});

	function changeDate(key: 'from' | 'to', value: string) {
		const url = new URL(page.url.href);
		if (value) url.searchParams.set(key, value);
		else url.searchParams.delete(key);
		void goto(url, { reset: false });
	}

	function distance(meters: number) {
		return `${kilometers.format(meters / 1000)} km`;
	}

	function toggle(id: Metric) {
		metric = metric === id ? null : id;
		map = undefined;
	}
</script>

{#snippet activities(items: Activity[])}
	<div class="grid min-w-0 gap-1 sm:grid-cols-2">
		{#each items as a (a.id)}
			<a
				href={`/activities/${a.id}?member=${encodeURIComponent(a.rider)}`}
				class="min-w-0 break-words text-sm font-medium text-accent-300 hover:underline"
			>
				{a.title || m.comparison_unnamed_ride()}
				<span class="font-normal text-quiet">· @{a.rider}</span>
			</a>
		{/each}
	</div>
{/snippet}

<div class="space-y-3">
	<div class="flex flex-wrap items-end gap-2 sm:gap-3">
		<label class="grid gap-1 text-xs text-quiet">
			{m.comparison_shared_from()}
			<input
				type="date"
				class="field"
				value={from}
				max={to || undefined}
				onchange={(event) => changeDate('from', event.currentTarget.value)}
			/>
		</label>
		<label class="grid gap-1 text-xs text-quiet">
			{m.comparison_shared_through()}
			<input
				type="date"
				class="field"
				value={to}
				min={from || undefined}
				onchange={(event) => changeDate('to', event.currentTarget.value)}
			/>
		</label>
	</div>

	{#if invalidRange}
		<p role="alert" class="surface p-3 text-sm">{m.comparison_shared_invalid_range()}</p>
	{:else if loading}
		<p role="status" class="surface p-3 text-sm text-quiet">{m.ride_loading()}</p>
	{:else if failed}
		<p role="alert" class="surface p-3 text-sm text-quiet">{m.comparison_shared_failed()}</p>
		<button
			type="button"
			class="min-h-10 text-sm text-accent-300 hover:underline"
			onclick={() => retry++}>{m.community_retry()}</button
		>
	{:else if result}
		<div class="grid grid-cols-3 gap-1.5 sm:gap-3">
			{#each stats as stat (stat.id)}
				<button
					type="button"
					class="surface stat min-w-0 text-left"
					aria-expanded={metric === stat.id}
					aria-controls="shared-activity-details"
					onclick={() => toggle(stat.id)}
					title={stat.id === 'encounters' ? m.comparison_crossed_paths_help() : undefined}
				>
					<span class="block text-xs font-medium text-quiet sm:text-sm">{stat.title}</span>
					<strong class="metric-value mt-0.5 block text-base tabular-nums sm:text-2xl"
						>{stat.value}</strong
					>
				</button>
			{/each}
		</div>

		<div id="shared-activity-details" hidden={metric === null} class="surface p-3 sm:p-4">
			{#if metric}
				<h2 class="section-title mb-2">{stats.find((stat) => stat.id === metric)?.title}</h2>
			{/if}
			{#if metric === 'rides' || metric === 'distance'}
				{#if !result.rides.length}
					<p class="text-sm text-quiet">{m.comparison_no_shared_rides()}</p>
				{:else}
					<ul class="divide-y divide-zinc-800">
						{#each result.rides as ride (ride.activities.map((a) => a.id).join(':'))}
							<li class="space-y-1.5 py-3">
								<p class="flex flex-wrap gap-x-3 gap-y-1 text-sm">
									<time datetime={ride.date}>{formatDateOnly(ride.date)}</time>
									<strong class="metric-value">{distance(ride.distance_m)}</strong>
									<span class="text-quiet">{formatDuration(Math.round(ride.duration_s))}</span>
								</p>
								{@render activities(ride.activities)}
							</li>
						{/each}
					</ul>
				{/if}
			{:else if metric === 'encounters'}
				<p class="mb-2 text-sm text-quiet">{m.comparison_crossed_paths_help()}</p>
				{#if !result.encounters.length}
					<p class="text-sm text-quiet">{m.comparison_no_crossed_paths()}</p>
				{:else}
					<ul class="divide-y divide-zinc-800">
						{#each result.encounters as event, i (`${event.time}:${event.activities[0]?.id}:${event.activities[1]?.id}`)}
							{@const mapId = `encounter-map-${i}`}
							<li class="space-y-1.5 py-3">
								<div class="flex flex-wrap items-center justify-between gap-x-3 text-sm">
									<time datetime={event.time}>{formatDate(event.time, { seconds: true })}</time>
									<button
										type="button"
										class="min-h-10 font-semibold text-accent-300 hover:underline"
										aria-expanded={map === mapId}
										aria-controls={mapId}
										onclick={() => (map = map === mapId ? undefined : mapId)}
										>{map === mapId
											? m.comparison_hide_location()
											: m.comparison_show_location()}</button
									>
								</div>
								{@render activities(event.activities)}
								<div id={mapId} hidden={map !== mapId}>
									{#if map === mapId}
										{#await import('./EncounterMap.svelte') then { default: EncounterMap }}
											<EncounterMap lat={event.lat} lon={event.lon} />
										{:catch}
											<p role="status" class="text-sm text-quiet">
												{m.comparison_map_unavailable()}
											</p>
										{/await}
									{/if}
								</div>
							</li>
						{/each}
					</ul>
				{/if}
			{/if}
		</div>
	{/if}
</div>

<style>
	.stat {
		min-height: 4.25rem;
		padding: clamp(0.35rem, 2.5vw, 1rem);
		overflow-wrap: anywhere;
	}

	.stat strong {
		white-space: normal;
	}

	.stat:hover,
	.stat[aria-expanded='true'] {
		border-color: var(--accent-primary);
		background: var(--surface-hover);
	}
</style>
