<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { formatSpeedKmh, intlLocale } from '#lib/format.js';
	import {
		riderName,
		routeDistance,
		routeElevation,
		routeKind,
		type RecordedRoute
	} from '#lib/recorded-routes.js';
	import RecordedRouteThumbnail from './RecordedRouteThumbnail.svelte';
	import RouteRiderAvatar from './RouteRiderAvatar.svelte';

	let {
		route,
		selected,
		onselect,
		onhover
	}: {
		route: RecordedRoute;
		selected: boolean;
		onselect: () => void;
		onhover?: (over: boolean) => void;
	} = $props();

	const featured = $derived(route.riders[0]);
	const other = $derived(route.riders.slice(1, 3));
	const more = $derived(route.rider_count - other.length - 1);
	const otherNames = $derived(
		new Intl.ListFormat(intlLocale(), { style: 'long', type: 'conjunction' }).format(
			other.map(riderName)
		)
	);
</script>

<button
	type="button"
	class="route-card grid w-full min-w-0 items-center gap-3 rounded-xl border p-3 text-left transition-colors hover:border-accent-500/60 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-500 {selected
		? 'border-accent-500 bg-accent-500/7'
		: 'border-zinc-800 bg-panel'}"
	aria-label={route.name}
	aria-pressed={selected}
	onclick={onselect}
	onpointerenter={(event) => {
		if (event.pointerType === 'mouse') onhover?.(true);
	}}
	onpointerleave={(event) => {
		if (event.pointerType === 'mouse') onhover?.(false);
	}}
>
	<div class="h-full min-h-[5.8rem] min-w-0 sm:min-h-24">
		<RecordedRouteThumbnail track={route.track} />
	</div>
	<div class="min-w-0">
		<h2 class="line-clamp-2 text-base font-semibold leading-tight text-zinc-100 sm:text-lg">
			{route.name}
		</h2>
		<div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-zinc-300">
			<span>{routeDistance(route.distance_m)}</span>
			<span aria-hidden="true">·</span>
			<span>{routeElevation(route.elevation_gain_m)}</span>
			<span aria-hidden="true">·</span>
			<span>{routeKind(route.type)}</span>
		</div>
		<dl
			class="mt-2 grid grid-cols-[auto_auto_1fr] gap-x-2 border-t border-zinc-800/70 pt-2 text-xs sm:gap-x-4"
		>
			<div>
				<dt class="text-quiet">{m.routes_total_rides()}</dt>
				<dd class="font-semibold tabular-nums text-zinc-100">{route.rides}</dd>
			</div>
			<div>
				<dt class="text-quiet">{m.routes_riders()}</dt>
				<dd class="font-semibold tabular-nums text-zinc-100">{route.rider_count}</dd>
			</div>
			<div>
				<dt class="text-quiet">{m.routes_best_speed()}</dt>
				<dd class="whitespace-nowrap font-semibold tabular-nums text-zinc-100">
					{formatSpeedKmh(route.best_speed_kmh)}
				</dd>
			</div>
		</dl>
	</div>
	{#if featured}
		<div class="riders grid min-w-0 items-center gap-x-3 gap-y-1 border-zinc-800/70">
			<div class="min-w-0">
				<p class="mb-1 text-xs text-quiet">{m.routes_featured_rider()}</p>
				<div class="flex min-w-0 items-center gap-2">
					<RouteRiderAvatar rider={featured} size="size-7" />
					<span class="truncate text-sm font-semibold text-zinc-100">{riderName(featured)}</span>
					<span
						class="ml-auto shrink-0 whitespace-nowrap rounded-full bg-accent-500/13 px-2 py-0.5 text-xs font-semibold text-accent-300"
					>
						{m.routes_ride_count({ count: featured.rides })}
					</span>
				</div>
			</div>
			{#if other.length}
				<div class="flex min-w-0 items-center gap-2">
					<span class="flex shrink-0 -space-x-1.5">
						{#each other as rider (rider.id)}
							<RouteRiderAvatar {rider} size="size-6 border border-zinc-800" />
						{/each}
						{#if more > 0}
							<span
								class="grid size-6 place-items-center rounded-full border border-zinc-800 bg-zinc-900 text-[10px] text-zinc-300"
							>
								{m.routes_other_riders({ count: more })}
							</span>
						{/if}
					</span>
					<span class="min-w-0 text-xs leading-tight text-quiet">
						{m.routes_also_ridden()} <span class="text-zinc-300">{otherNames}</span>
					</span>
				</div>
			{/if}
		</div>
	{/if}
</button>

<style>
	.route-card {
		grid-template-columns: minmax(4.5rem, 28%) minmax(0, 1fr);
		container-type: inline-size;
	}
	.riders {
		grid-column: 1 / -1;
		grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr);
		border-top-width: 1px;
		padding-top: 0.5rem;
	}
	@container (max-width: 330px) {
		.riders {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	@media (min-width: 90rem) {
		.route-card {
			grid-template-columns: minmax(5.25rem, 23%) minmax(0, 1fr) minmax(9.2rem, 31%);
		}
		.riders {
			grid-column: auto;
			grid-template-columns: minmax(0, 1fr);
			align-self: stretch;
			border-top-width: 0;
			border-left-width: 1px;
			padding-top: 0;
			padding-left: 0.75rem;
		}
	}
</style>
