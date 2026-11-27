<script lang="ts">
	import { memberURL } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import Icon from '#lib/components/Icon.svelte';
	import MetricStrip from '#lib/components/MetricStrip.svelte';
	import { activityMetrics, type ActivityTotals } from '#lib/dashboard-metrics.js';
	import FilterSheet from '#lib/components/FilterSheet.svelte';
	import SortControls from '#lib/components/SortControls.svelte';
	import SortHeader from '#lib/components/SortHeader.svelte';
	import { compareSortedValues, promoteSort, type SortCriterion } from '#lib/stacked-sort.js';
	import {
		intlLocale,
		formatDistance,
		formatDuration,
		formatElevation,
		formatSpeed
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
		moving_duration_s: number;
		avg_speed_mps: number | null;
	} | null;
	type Stats = {
		totals: ActivityTotals;
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
		{ key: 'count', label: m.nav_activities(), shortLabel: m.metric_activity_count_short() },
		{ key: 'distance_m', label: m.col_distance() },
		{ key: 'moving_s', label: m.col_moving(), shortLabel: m.detail_label_time() },
		{ key: 'speed_kmh', label: m.col_average_speed(), shortLabel: m.metric_avg_speed_short() }
	] satisfies { key: SportColumn; label: string; shortLabel?: string }[];
	const initialSportOrder: SortCriterion<SportColumn>[] = [{ column: 'count', direction: 'desc' }];
	let sportOrder = $state<SortCriterion<SportColumn>[]>(initialSportOrder);
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

	let selectedYear = $state('all');
	const years = $derived(s?.by_year ?? []);
	const displayedYears = $derived(
		selectedYear === 'all' ? years : years.filter((year) => String(year.year) === selectedYear)
	);

	$effect(() => {
		const url = memberURL('/api/stats', data.member);
		const controller = new AbortController();
		const { signal } = controller;
		loading = true;
		s = null;
		selectedYear = 'all';
		void (async () => {
			try {
				const res = await fetch(url, { signal });
				if (!res.ok) return;
				const next: Stats = await res.json();
				if (!signal.aborted) s = next;
			} catch {
				// The error state below covers unavailable statistics.
			} finally {
				if (!signal.aborted) loading = false;
			}
		})();
		return () => controller.abort();
	});

	function humanize(v: string): string {
		return v.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
	}

	// max year distance for the bar widths
	const maxYearDist = $derived(s ? Math.max(1, ...s.by_year.map((y) => y.distance_m)) : 1);
</script>

<svelte:head><title>{m.nav_stats()}</title></svelte:head>

<div class="space-y-5 max-md:space-y-4">
	<header>
		<h1 class="page-title">{m.nav_stats()}</h1>
	</header>

	{#if loading}
		<p role="status" class="surface state-panel">{m.ride_loading()}</p>
	{:else if !s}
		<p role="alert" class="surface state-panel">{m.stats_failed()}</p>
	{:else if s.totals.count === 0}
		<p class="surface state-panel">{m.activities_empty()}</p>
	{:else}
		<section aria-labelledby="lifetime-totals">
			<h2 id="lifetime-totals" class="section-title mb-2">{m.stats_lifetime()}</h2>
			<MetricStrip metrics={activityMetrics(s.totals)} label={m.stats_lifetime()} />
		</section>

		<section aria-labelledby="stats-records">
			<header class="mb-2 flex items-center justify-between gap-3">
				<h2 id="stats-records" class="section-title">{m.member_records()}</h2>
				<a
					href={memberURL('/records', data.member)}
					class="inline-flex min-h-10 items-center gap-1 text-xs font-semibold text-accent-300 hover:underline"
				>
					{m.overview_view_all()}
					<Icon name="chevron" size={14} />
				</a>
			</header>
			<div class="grid grid-cols-2 gap-2 sm:gap-3">
				{#each [{ label: m.stats_longest_distance(), r: s.records.longest_distance, distanceRecord: true }, { label: m.stats_longest_moving(), r: s.records.longest_duration, distanceRecord: false }] as card (card.label)}
					{#snippet highlight()}
						<span class="record-icon" class:trophy={card.distanceRecord}>
							<Icon name={card.distanceRecord ? 'records' : 'clock'} size={18} />
						</span>
						<span class="grid min-w-0 gap-0.5">
							<span class="text-xs font-medium text-quiet">{card.label}</span>
							<strong class="metric-value text-base leading-tight tracking-tight sm:text-xl">
								{card.r
									? card.distanceRecord
										? formatDistance(card.r.distance_m)
										: formatDuration(card.r.moving_duration_s)
									: '—'}
							</strong>
							{#if card.r}
								<span class="truncate text-xs text-quiet" title={card.r.title}>
									{card.r.title || humanize(card.r.discipline || card.r.type)}
								</span>
								{#if card.distanceRecord && card.r.type === 'ride'}
									<span class="text-[0.6875rem] text-quiet tabular-nums">
										{m.metric_moving_time()}
										<span class="metric-value"
											>{formatDuration(card.r.moving_duration_s || null)}</span
										>
										{#if card.r.avg_speed_mps != null}
											<span class="metric-value">· {formatSpeed(card.r.avg_speed_mps)}</span>{/if}
									</span>
								{/if}
							{/if}
						</span>
					{/snippet}
					{#if card.r}
						<a
							href={memberURL(`/activities/${card.r.id}`, data.member)}
							class="surface record-highlight"
						>
							{@render highlight()}
						</a>
					{:else}
						<div class="surface record-highlight">{@render highlight()}</div>
					{/if}
				{/each}
			</div>
		</section>

		<!-- by year -->
		{#if s.by_year.length > 0}
			<section>
				<header class="mb-2 flex items-center justify-between gap-2">
					<h2 class="section-title">{m.stats_by_year()}</h2>
					<select class="field" aria-label={m.stats_year()} bind:value={selectedYear}>
						<option value="all">{m.stats_all_years()}</option>
						{#each years as year (year.year)}
							<option value={String(year.year)}>{year.year}</option>
						{/each}
					</select>
				</header>
				<div class="surface overflow-hidden">
					<div class="table-scroll">
						<table class="data-table w-full text-sm">
							<thead class="text-xs text-quiet">
								<tr>
									<th class="px-4 py-2 text-left font-medium">{m.stats_year()}</th>
									<th class="px-4 py-2 text-right font-medium" title={m.nav_activities()}>
										<span class="md:hidden">{m.metric_activity_count_short()}</span>
										<span class="hidden md:inline">{m.nav_activities()}</span>
									</th>
									<th class="px-4 py-2 text-right font-medium">{m.col_distance()}</th>
									<th class="px-4 py-2 text-right font-medium" title={m.col_moving()}>
										<span class="md:hidden">{m.detail_label_time()}</span>
										<span class="hidden md:inline">{m.col_moving()}</span>
									</th>
									<th class="px-4 py-2 text-right font-medium">{m.col_elevation()}</th>
									<th class="w-1/4 px-4 py-2 max-md:hidden"></th>
								</tr>
							</thead>
							<tbody>
								{#each displayedYears as y (y.year)}
									<tr
										class="border-t border-zinc-800/70"
										class:current-year={y.year === new Date().getFullYear()}
									>
										<th scope="row" class="px-4 py-2 text-left font-semibold">{y.year}</th>
										<td class="numeric px-4 py-2 text-zinc-300"
											>{y.count.toLocaleString(intlLocale())}</td
										>
										<td class="numeric px-4 py-2 text-zinc-300"
											>{formatDistance(y.distance_m, { compact: true })}</td
										>
										<td class="numeric px-4 py-2 text-zinc-300">{formatDuration(y.moving_s)}</td>
										<td class="numeric px-4 py-2 text-zinc-300"
											>{formatElevation(y.elevation_gain_m)}</td
										>
										<td class="px-4 py-2 max-md:hidden">
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
				<FilterSheet
					active={sportOrder.length !== 1 ||
						sportOrder[0]?.column !== 'count' ||
						sportOrder[0]?.direction !== 'desc'}
					reset={() => (sportOrder = initialSportOrder)}
				>
					{#snippet toolbar()}
						<h2 class="section-title">{m.stats_by_sport()}</h2>
					{/snippet}
					<SortControls
						columns={sportColumns}
						order={sportOrder}
						onchange={(next) => (sportOrder = next)}
					/>
				</FilterSheet>
				<div class="surface table-scroll mt-2">
					<table class="data-table w-full text-sm [--table-name-min:2.75rem]">
						<thead class="text-xs text-quiet">
							<tr>
								{#each sportColumns as column (column.key)}
									{@const index = sportOrder.findIndex(
										(criterion) => criterion.column === column.key
									)}
									<SortHeader
										label={column.label}
										shortLabel={column.shortLabel}
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
									<th scope="row" class="name-cell p-2 text-left font-medium sm:px-4">
										<a
											href={memberURL(`/activities?type=${sport.value}`, data.member)}
											class="table-name hover:text-accent-300"
											title={humanize(sport.value)}>{humanize(sport.value)}</a
										>
									</th>
									<td class="numeric p-2 sm:px-4">{sport.count.toLocaleString(intlLocale())}</td>
									<td class="numeric p-2 sm:px-4"
										>{formatDistance(sport.distance_m || null, { compact: true })}</td
									>
									<td class="numeric p-2 sm:px-4"
										>{sport.moving_s > 0 ? formatDuration(sport.moving_s) : '—'}</td
									>
									<td class="numeric p-2 sm:px-4">
										{formatSpeed(sport.speed_kmh == null ? null : sport.speed_kmh / 3.6)}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			</section>
		{/if}
	{/if}
</div>

<style>
	.record-highlight {
		display: flex;
		min-width: 0;
		align-items: flex-start;
		gap: clamp(0.35rem, 2vw, 0.75rem);
		padding: clamp(0.55rem, 2.5vw, 1rem);
	}

	a.record-highlight:hover {
		background: var(--surface-hover);
	}

	.record-icon {
		display: grid;
		width: 1.8rem;
		height: 1.8rem;
		flex: none;
		place-items: center;
		border-radius: 0.5rem;
		background: var(--surface-active);
		color: var(--accent-text);
	}

	.record-icon.trophy {
		background: var(--warning-surface);
		color: var(--warning-strong);
	}

	.current-year {
		background: var(--surface-active);
	}

	@media (width < 48rem) {
		.record-icon {
			width: 1.25rem;
			height: 1.25rem;
		}
		.record-icon :global(svg) {
			width: 0.875rem;
			height: 0.875rem;
		}
	}
</style>
