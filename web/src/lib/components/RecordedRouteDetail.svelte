<script lang="ts">
	import { onDestroy } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDate, formatSpeedKmh, intlLocale } from '#lib/format.js';
	import { memberProfile } from '#lib/member.js';
	import {
		previewPoints,
		recordedRoutePath,
		riderName,
		routeData,
		routeDistance,
		routeElevation,
		routeKind,
		type MatchingRide,
		type RouteDetail,
		type RouteRider
	} from '#lib/recorded-routes.js';
	import ElevationProfile from './ElevationProfile.svelte';
	import RouteRiderAvatar from './RouteRiderAvatar.svelte';
	import WaasRouteMap from './WaasRouteMap.svelte';

	let { route, showTitle = true }: { route: RouteDetail; showTitle?: boolean } = $props();
	type Sort = 'rider' | 'rides' | 'speed';
	let sort = $state<Sort>('rides');
	let descending = $state(true);
	let more = $state<MatchingRide[]>([]);
	let busy = $state(false);
	let failed = $state(false);
	let request: AbortController | undefined;
	const featured = $derived(route.riders[0]);
	const profile = $derived(previewPoints(route.track));
	const hasProfile = $derived(profile.filter((point) => point.eleM != null).length > 1);
	const names = $derived(new Map(route.riders.map((rider) => [rider.id, riderName(rider)])));
	const activities = $derived([...route.recent, ...more]);
	const collator = $derived(new Intl.Collator(intlLocale()));
	const sorted = $derived(route.riders.toSorted((a, b) => compare(a, b) * (descending ? -1 : 1)));

	function compare(a: RouteRider, b: RouteRider) {
		switch (sort) {
			case 'rider':
				return collator.compare(riderName(a), riderName(b));
			case 'rides':
				return a.rides - b.rides;
			case 'speed':
				return (a.best_speed_kmh ?? -1) - (b.best_speed_kmh ?? -1);
		}
	}

	function sortBy(key: Sort) {
		descending = sort === key ? !descending : key !== 'rider';
		sort = key;
	}

	async function loadMore() {
		request?.abort();
		const controller = new AbortController();
		request = controller;
		busy = true;
		failed = false;
		try {
			const result = await routeData<{ recent: MatchingRide[] }>(
				recordedRoutePath(route.id) + '/activities?offset=' + activities.length,
				controller.signal
			);
			const seen = new Set(activities.map((ride) => ride.id));
			more = [...more, ...result.recent.filter((ride) => !seen.has(ride.id))];
		} catch {
			if (!controller.signal.aborted) failed = true;
		} finally {
			if (!controller.signal.aborted) busy = false;
		}
	}
	onDestroy(() => request?.abort());
</script>

<div class="min-w-0 space-y-4">
	{#if showTitle}
		<header class="flex flex-wrap items-start justify-between gap-1.5">
			<h2 class="min-w-0 text-xl font-semibold tracking-tight text-zinc-100 sm:text-2xl">
				{route.name}
			</h2>
			<span class="rounded-full bg-accent-500/12 px-2.5 py-1 text-xs font-semibold text-accent-300"
				>{routeKind(route.type)}</span
			>
		</header>
	{/if}

	{#if profile.length}
		<WaasRouteMap recorded={route.track} compact />
	{:else}
		<p class="rounded-lg border border-zinc-800 bg-zinc-900 p-4 text-sm text-quiet">
			{m.routes_map_unavailable()}
		</p>
	{/if}

	<dl class="grid grid-cols-3 gap-x-3 gap-y-2 border-b border-zinc-800 pb-3 text-sm">
		{@render metric(m.routes_distance(), routeDistance(route.distance_m))}
		{@render metric(m.routes_elevation(), routeElevation(route.elevation_gain_m))}
		{@render metric(m.routes_route_type(), routeKind(route.type))}
		{@render metric(m.routes_total_rides(), String(route.rides))}
		{@render metric(m.routes_riders(), String(route.rider_count))}
		{@render metric(m.routes_best_speed(), formatSpeedKmh(route.best_speed_kmh))}
	</dl>

	{#if profile.length}
		<div class="grid grid-cols-[1.35fr_1fr] gap-2 text-center text-sm">
			<a
				href={'/routes/waasland/recorded/' + encodeURIComponent(route.id)}
				class="grid min-h-11 place-items-center rounded-lg bg-accent-500 px-2 font-semibold text-on-accent hover:bg-accent-400"
				>{m.routes_open_planner()}</a
			>
			<a
				href={recordedRoutePath(route.id) + '/gpx'}
				download
				class="grid min-h-11 place-items-center rounded-lg border border-accent-500 px-2 font-semibold text-accent-300 hover:bg-accent-500/10"
				>{m.routes_export_gpx()}</a
			>
		</div>
	{/if}

	<section class="border-b border-zinc-800 pb-3">
		<h3 class="mb-2 text-sm font-semibold text-zinc-100">{m.waas_elevation_profile()}</h3>
		{#if hasProfile}
			<ElevationProfile track={profile} height={64} />
		{:else}
			<p class="text-xs text-quiet">{m.routes_elevation_unavailable()}</p>
		{/if}
	</section>

	<section>
		<h3 class="mb-1 text-sm font-semibold text-zinc-100">{m.routes_leaderboard()}</h3>
		<table class="w-full table-fixed text-xs sm:text-sm">
			<thead class="border-b border-zinc-800 text-quiet">
				<tr>
					{#each [['rider', m.routes_rider()], ['rides', m.routes_rides()], ['speed', m.routes_best_speed()]] as [key, label], index (key)}
						<th
							scope="col"
							class={index === 0 ? 'w-1/2 text-left' : 'text-right'}
							aria-sort={sort === key ? (descending ? 'descending' : 'ascending') : undefined}
						>
							<button
								class="min-h-9 font-medium hover:text-accent-300"
								onclick={() => sortBy(key as Sort)}
							>
								{label}{#if sort === key}
									<span aria-hidden="true">{descending ? '↓' : '↑'}</span>{/if}
							</button>
						</th>
					{/each}
				</tr>
			</thead>
			<tbody class="divide-y divide-zinc-800/75">
				{#each sorted as rider (rider.id)}
					<tr>
						<td>
							<a
								href={memberProfile(rider)}
								class="flex min-h-10 min-w-0 items-center gap-2 py-1 font-semibold hover:text-accent-300"
							>
								<RouteRiderAvatar {rider} size="size-7" />
								<span class="truncate">{riderName(rider)}</span>
								{#if rider.id === featured?.id}
									<span
										class="text-accent-300"
										title={m.routes_featured_rider()}
										aria-label={m.routes_featured_rider()}
									>
										<svg
											viewBox="0 0 24 24"
											class="size-4"
											fill="none"
											stroke="currentColor"
											stroke-width="1.8"
											stroke-linejoin="round"
											aria-hidden="true"><path d="m3 7 4.5 4L12 5l4.5 6L21 7l-2 11H5z" /></svg
										>
									</span>
								{/if}
							</a>
						</td>
						<td class="text-right tabular-nums">{rider.rides}</td>
						<td class="whitespace-nowrap text-right tabular-nums"
							>{formatSpeedKmh(rider.best_speed_kmh)}</td
						>
					</tr>
				{/each}
			</tbody>
		</table>
	</section>

	<section class="border-t border-zinc-800 pt-3">
		<h3 class="mb-1 text-sm font-semibold text-zinc-100">{m.routes_recent_rides()}</h3>
		<ul class="divide-y divide-zinc-800/75">
			{#each activities as activity (activity.id)}
				<li>
					<a
						href={'/activities/' + encodeURIComponent(activity.id)}
						class="grid min-h-12 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 py-1.5 text-xs hover:text-accent-300"
					>
						<span class="min-w-0">
							<span class="block truncate text-sm font-semibold"
								>{activity.title || m.nav_activities()}</span
							>
							<span class="block truncate text-quiet"
								>{names.get(activity.rider_id)} · {formatDate(activity.start_time)}</span
							>
						</span>
						<span class="whitespace-nowrap text-right tabular-nums text-zinc-300">
							{routeDistance(activity.distance_m)}<br />{formatSpeedKmh(activity.speed_kmh)}
						</span>
					</a>
				</li>
			{/each}
		</ul>
		{#if failed}<p role="alert" class="text-sm text-red-300">
				{m.routes_activity_load_failed()}
			</p>{/if}
		{#if activities.length < route.rides}
			<button
				class="mt-1 min-h-10 text-sm font-semibold text-accent-300 disabled:opacity-50"
				onclick={loadMore}
				disabled={busy}
			>
				{busy ? m.routes_loading() : m.routes_more_rides()}
			</button>
		{/if}
	</section>
</div>

{#snippet metric(label: string, value: string)}
	<div class="flex min-w-0 flex-col-reverse">
		<dt class="text-xs text-quiet">{label}</dt>
		<dd class="font-semibold text-zinc-100">{value}</dd>
	</div>
{/snippet}
