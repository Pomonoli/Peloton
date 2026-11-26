<script lang="ts">
	import {
		ActivityFilter,
		DATE_PRESETS,
		SORTS,
		humanize,
		type ActivityFacets
	} from '#lib/activity-filter.svelte.js';
	import { onMount, type Snippet } from 'svelte';
	import { clients } from '#lib/connect.js';
	import { m } from '#lib/paraglide/messages.js';
	import { gearToView, type GearView } from '#lib/gear.js';

	// The standard filter bar. Facet chips only render for values the user
	// actually has (facets come from the server, respecting every filter but
	// their own dimension). Pages own the ActivityFilter and react to its keys.
	let {
		filter,
		facets,
		showSort = true,
		controls,
		member
	}: {
		filter: ActivityFilter;
		facets: ActivityFacets | null;
		showSort?: boolean;
		controls?: Snippet;
		member?: string | undefined;
	} = $props();

	// Another filter can shrink a facet row mid-interaction. Keep a row visible
	// once it has appeared, or the bar jumps around while filtering.
	let showSports = $state(false);
	let showDisciplines = $state(false);
	// Gear chips: the user's active gear (retired too when it is the current
	// filter), loaded once; no chips when they have none.
	let gear = $state<GearView[]>([]);
	onMount(async () => {
		try {
			gear = (
				await clients.gear.listGear({ includeRetired: true, ...(member ? { member } : {}) })
			).gear.map(gearToView);
		} catch {
			/* filter bar works without gear */
		}
	});
	const gearChips = $derived(gear.filter((g) => !g.retired || g.id === filter.gearId));
	$effect(() => {
		if (facets && facets.types.length > 1) showSports = true;
		if (facets && facets.disciplines.length > 0) showDisciplines = true;
	});

	const chip = 'rounded-full px-3 py-1 text-xs max-md:min-h-10';
	const chipOn = 'bg-accent-500 text-on-accent';
	const chipOff = 'bg-zinc-800 text-zinc-300 hover:bg-zinc-700';
	const field =
		'min-w-0 rounded-lg border border-zinc-700 bg-zinc-900 px-2 py-1 text-xs text-zinc-300 focus:border-accent-400 focus:outline-none max-md:min-h-10 max-md:text-base';
</script>

{#if facets || controls}
	<div class="space-y-3 surface p-4 max-md:rounded-none max-md:border-0 max-md:p-0">
		{#if controls}
			{@render controls()}
		{/if}
		{#if facets && showSports}
			<div class="flex flex-wrap items-center gap-2">
				<span class="mr-1 text-xs font-medium uppercase tracking-wide text-zinc-500">Sport</span>
				<button
					type="button"
					onclick={() => {
						filter.type = '';
						filter.discipline = '';
					}}
					class="{chip} {filter.type === '' ? chipOn : chipOff}"
				>
					{m.filter_all()}
				</button>
				{#each facets.types as f (f.value)}
					<button
						type="button"
						onclick={() => filter.setType(f.value)}
						class="{chip} {filter.type === f.value ? chipOn : chipOff}"
					>
						{humanize(f.value)} <span class="opacity-60">{f.count}</span>
					</button>
				{/each}
			</div>
		{/if}

		{#if facets && showDisciplines}
			<div class="flex flex-wrap items-center gap-2">
				<span class="mr-1 text-xs font-medium uppercase tracking-wide text-zinc-500"
					>Discipline</span
				>
				<button
					type="button"
					onclick={() => (filter.discipline = '')}
					class="{chip} {filter.discipline === '' ? chipOn : chipOff}"
				>
					{m.filter_all()}
				</button>
				{#each facets.disciplines as f (f.value)}
					<button
						type="button"
						onclick={() => (filter.discipline = filter.discipline === f.value ? '' : f.value)}
						class="{chip} {filter.discipline === f.value ? chipOn : chipOff}"
					>
						{humanize(f.value)} <span class="opacity-60">{f.count}</span>
					</button>
				{/each}
			</div>
		{/if}

		<!-- Date range: year selector + quick presets + custom bounds. -->
		<div class="flex flex-wrap items-center gap-2">
			<span class="mr-1 text-xs font-medium uppercase tracking-wide text-zinc-500">Date</span>
			{#if (facets?.years ?? []).length > 0}
				<select
					bind:value={filter.year}
					onchange={() => filter.applyYear()}
					aria-label="Year"
					class="{field} {filter.year ? 'border-accent-500 text-accent-300' : ''}"
				>
					<option value="">All years</option>
					{#each facets?.years ?? [] as y (y.value)}
						<option value={y.value}>{y.value} ({y.count})</option>
					{/each}
				</select>
			{/if}
			{#each DATE_PRESETS as p (p.days)}
				<button
					type="button"
					onclick={() => filter.applyPreset(p.days)}
					class="{chip} {filter.preset === p.days ? chipOn : chipOff}"
				>
					{p.label}
				</button>
			{/each}
			<input
				type="date"
				bind:value={filter.from}
				onchange={() => filter.dateEdited()}
				aria-label="From date"
				class={field}
			/>
			<span class="text-xs text-quiet">–</span>
			<input
				type="date"
				bind:value={filter.to}
				onchange={() => filter.dateEdited()}
				aria-label="To date"
				class={field}
			/>
		</div>

		{#if gearChips.length > 0}
			<div class="flex flex-wrap items-center gap-2">
				<span class="mr-1 text-xs font-medium uppercase tracking-wide text-zinc-500"
					>{m.gear_filter_label()}</span
				>
				<button
					type="button"
					onclick={() => (filter.gearId = '')}
					class="{chip} {filter.gearId === '' ? chipOn : chipOff}"
				>
					{m.filter_all()}
				</button>
				{#each gearChips as g (g.id)}
					<button
						type="button"
						onclick={() => (filter.gearId = filter.gearId === g.id ? '' : g.id)}
						class="{chip} {filter.gearId === g.id ? chipOn : chipOff}"
					>
						{g.name}
					</button>
				{/each}
			</div>
		{/if}

		<div class="flex">
			<button
				type="button"
				onclick={() => (filter.moreFilters = !filter.moreFilters)}
				class="ml-auto min-h-10 text-xs {filter.rangesActive
					? 'text-accent-300'
					: 'text-zinc-500 hover:text-zinc-300'}"
			>
				{filter.moreFilters ? 'Hide ranges ▴' : `Ranges${filter.rangesActive ? ' •' : ''} ▾`}
			</button>
		</div>

		<!-- Numeric ranges (display units → SI in the query). -->
		{#if filter.moreFilters}
			<div class="grid gap-x-6 gap-y-2 border-t border-zinc-800 pt-3 sm:grid-cols-2 lg:grid-cols-3">
				{#each filter.rangeSpecs as r (r.key)}
					<div class="flex items-center gap-2">
						<span class="w-28 shrink-0 text-xs text-zinc-500 max-md:w-21">{r.label}</span>
						<input
							type="number"
							min="0"
							placeholder="min"
							bind:value={filter.ranges[r.key].min}
							aria-label={`${r.label} minimum (${r.unit})`}
							class="w-20 max-md:flex-1 {field}"
						/>
						<span class="text-xs text-quiet">–</span>
						<input
							type="number"
							min="0"
							placeholder="max"
							bind:value={filter.ranges[r.key].max}
							aria-label={`${r.label} maximum (${r.unit})`}
							class="w-20 max-md:flex-1 {field}"
						/>
						<span class="text-xs text-quiet">{r.unit}</span>
					</div>
				{/each}
			</div>
		{/if}

		{#if showSort || filter.active}
			<div
				class="flex items-center gap-2 border-t border-zinc-800 pt-3"
				class:max-md:hidden={!showSort}
			>
				{#if showSort}
					<label for="sort" class="text-xs font-medium uppercase tracking-wide text-zinc-500"
						>Sort</label
					>
					<select id="sort" bind:value={filter.sort} class={field}>
						{#each SORTS as s (s.value)}
							<option value={s.value}>{s.label}</option>
						{/each}
					</select>
				{/if}
				{#if filter.active}
					<button
						type="button"
						onclick={() => filter.clear()}
						class="text-xs text-zinc-500 hover:text-zinc-300 max-md:hidden"
					>
						Clear filters
					</button>
				{/if}
			</div>
		{/if}
	</div>
{/if}
