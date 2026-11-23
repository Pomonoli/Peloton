<script lang="ts">
	import { memberURL } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import { onMount } from 'svelte';
	import SortHeader from '#lib/components/SortHeader.svelte';
	import { compareSortedValues, promoteSort, type SortCriterion } from '#lib/stacked-sort.js';
	import SportIcon from '#lib/components/SportIcon.svelte';
	import {
		intlLocale,
		formatDistance,
		formatDistanceEffort,
		formatDuration,
		formatElevation,
		formatSpeedKmh
	} from '#lib/format.js';

	let { data } = $props();

	type Facet = { value: string; count: number };
	type Year = {
		year: number;
		count: number;
		distance_m: number;
		moving_s: number;
		elevation_gain_m: number;
	};
	type Record = {
		id: string;
		title: string;
		type: string;
		discipline: string;
		distance_m: number;
		elapsed_duration_s: number;
	} | null;
	type Stats = {
		totals: { count: number; distance_m: number; moving_s: number; elevation_gain_m: number };
		sport_totals: {
			value: string;
			count: number;
			distance_m: number;
			moving_s: number;
			speed_kmh: number | null;
		}[];
		by_discipline: Facet[];
		by_year: Year[];
		records: { longest_distance: Record; longest_duration: Record };
	};

	let s = $state<Stats | null>(null);
	type SportColumn = keyof Stats['sport_totals'][number];
	const sportColumns = [
		{ key: 'value', label: m.col_sport() },
		{ key: 'count', label: m.nav_activities() },
		{ key: 'distance_m', label: m.col_distance() },
		{ key: 'moving_s', label: m.col_moving() },
		{ key: 'speed_kmh', label: m.col_average_speed() }
	] satisfies { key: SportColumn; label: string }[];
	let sportOrder = $state<SortCriterion<SportColumn>[]>([{ column: 'count', direction: 'desc' }]);
	const sports = $derived(
		[...(s?.sport_totals ?? [])].sort((left, right) => {
			for (const { column, direction } of sportOrder) {
				const result = compareSortedValues(left[column], right[column], direction);
				if (result !== 0) return result;
			}
			return left.value.localeCompare(right.value);
		})
	);

	let loading = $state(true);

	onMount(async () => {
		try {
			const res = await fetch(memberURL('/api/stats', data.member));
			if (res.ok) s = await res.json();
		} catch {
			/* ignore */
		}
		loading = false;
	});

	function humanize(v: string): string {
		return v.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
	}

	// max year distance for the bar widths
	const maxYearDist = $derived(s ? Math.max(1, ...s.by_year.map((y) => y.distance_m)) : 1);
</script>

<div class="space-y-8">
	<header>
		<h1 class="text-3xl font-semibold tracking-tight max-md:text-2xl">Stats</h1>
		<p class="mt-1 text-sm text-zinc-400">
			Lifetime totals, year-by-year trends, and activity records.
		</p>
	</header>

	{#if loading}
		<div class="text-sm text-zinc-500">Loading…</div>
	{:else if !s}
		<p role="alert" class="text-sm text-red-300">{m.stats_failed()}</p>
	{:else if s.totals.count === 0}
		<div class="rounded-lg border border-zinc-800 bg-panel p-8 text-center text-sm text-zinc-400">
			No activities yet.
		</div>
	{:else}
		<!-- lifetime totals -->
		<div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
			{#each [{ label: 'Activities', value: s.totals.count.toLocaleString(intlLocale()) }, { label: 'Distance', value: formatDistance(s.totals.distance_m) }, { label: 'Moving time', value: formatDuration(s.totals.moving_s) }, { label: 'Elevation', value: formatElevation(s.totals.elevation_gain_m) }] as c (c.label)}
				<div class="rounded-xl border border-zinc-800 bg-panel p-5">
					<div class="text-xs uppercase tracking-wide text-zinc-500">{c.label}</div>
					<div class="mt-1 text-2xl font-semibold tabular-nums text-zinc-100">{c.value}</div>
				</div>
			{/each}
		</div>

		<!-- records -->
		<section>
			<h2 class="mb-3 text-sm font-medium text-zinc-300">Records</h2>
			<div class="grid gap-4 sm:grid-cols-2">
				{#each [{ label: 'Longest distance', r: s.records.longest_distance, distanceRecord: true }, { label: 'Longest duration', r: s.records.longest_duration, distanceRecord: false }] as card (card.label)}
					<div class="rounded-xl border border-zinc-800 bg-panel p-5">
						<div class="text-xs uppercase tracking-wide text-zinc-500">{card.label}</div>
						{#if card.r}
							<a
								href={`/activities/${card.r.id}`}
								class="mt-2 flex items-center gap-3 hover:text-accent-300"
							>
								<span
									class="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-zinc-800 text-zinc-300"
								>
									<SportIcon type={card.r.discipline || card.r.type} size={18} />
								</span>
								<div class="min-w-0 flex-1">
									<div class="truncate text-sm font-medium">
										{card.r.title || `${humanize(card.r.discipline || card.r.type)} Activity`}
									</div>
								</div>
								<div class="text-right tabular-nums">
									<div class="text-lg font-semibold text-zinc-100">
										{card.distanceRecord
											? formatDistance(card.r.distance_m)
											: formatDuration(card.r.elapsed_duration_s)}
									</div>
									{#if card.distanceRecord && card.r.type === 'ride'}
										<div class="text-sm text-quiet">
											{formatDistanceEffort(card.r.distance_m, card.r.elapsed_duration_s)}
										</div>
									{/if}
								</div>
							</a>
						{:else}
							<div class="mt-2 text-sm text-zinc-500">—</div>
						{/if}
					</div>
				{/each}
			</div>
		</section>

		<!-- by year -->
		{#if s.by_year.length > 0}
			<section>
				<h2 class="mb-3 text-sm font-medium text-zinc-300">By year</h2>
				<div class="overflow-hidden rounded-xl border border-zinc-800">
					<div class="overflow-x-auto">
						<table class="w-full text-sm">
							<thead class="bg-panel text-xs uppercase tracking-wide text-zinc-500">
								<tr>
									<th class="px-4 py-2 text-left font-medium">Year</th>
									<th class="px-4 py-2 text-right font-medium">Activities</th>
									<th class="px-4 py-2 text-right font-medium">Distance</th>
									<th class="px-4 py-2 text-right font-medium">Moving</th>
									<th class="px-4 py-2 text-right font-medium">Elevation</th>
									<th class="w-1/4 px-4 py-2"></th>
								</tr>
							</thead>
							<tbody>
								{#each s.by_year as y (y.year)}
									<tr class="border-t border-zinc-800/70">
										<td class="px-4 py-2 font-medium text-zinc-200">{y.year}</td>
										<td class="px-4 py-2 text-right tabular-nums text-zinc-300">{y.count}</td>
										<td class="px-4 py-2 text-right tabular-nums text-zinc-300"
											>{formatDistance(y.distance_m)}</td
										>
										<td class="px-4 py-2 text-right tabular-nums text-zinc-300"
											>{formatDuration(y.moving_s)}</td
										>
										<td class="px-4 py-2 text-right tabular-nums text-zinc-300"
											>{formatElevation(y.elevation_gain_m)}</td
										>
										<td class="px-4 py-2">
											<div class="h-2 rounded-full bg-zinc-800">
												<div
													class="h-2 rounded-full bg-accent-500"
													style:width={`${(y.distance_m / maxYearDist) * 100}%`}
												></div>
											</div>
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			</section>
		{/if}

		{#if s.sport_totals.length > 0}
			<section>
				<h2 class="mb-3 text-sm font-medium">{m.stats_by_sport()}</h2>
				<div class="overflow-x-auto rounded-xl border border-zinc-800">
					<table class="w-full bg-panel text-sm">
						<thead class="text-xs text-quiet">
							<tr>
								{#each sportColumns as column (column.key)}
									{@const index = sportOrder.findIndex(
										(criterion) => criterion.column === column.key
									)}
									<SortHeader
										label={column.label}
										{index}
										direction={sportOrder[index]?.direction}
										align={column.key === 'value' ? 'text-left' : 'text-right'}
										onclick={() => (sportOrder = promoteSort(sportOrder, column.key))}
									/>
								{/each}
							</tr>
						</thead>
						<tbody class="divide-y divide-zinc-800 tabular-nums">
							{#each sports as sport (sport.value)}
								<tr>
									<th scope="row" class="p-2 text-left font-medium sm:px-4">
										<a
											href={memberURL(`/activities?type=${sport.value}`, data.member)}
											class="hover:text-accent-300">{humanize(sport.value)}</a
										>
									</th>
									<td class="p-2 text-right sm:px-4">{sport.count.toLocaleString(intlLocale())}</td>
									<td class="p-2 text-right sm:px-4">{formatDistance(sport.distance_m || null)}</td>
									<td class="p-2 text-right sm:px-4"
										>{sport.moving_s > 0 ? formatDuration(sport.moving_s) : '—'}</td
									>
									<td class="p-2 text-right sm:px-4">{formatSpeedKmh(sport.speed_kmh)}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{/if}
	{/if}
</div>
