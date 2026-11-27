<script lang="ts">
	import { SvelteURLSearchParams } from 'svelte/reactivity';
	import { intlLocale } from '#lib/format.js';
	import { SORTS } from '#lib/activity-filter.svelte.js';
	import type { FeedActivity } from '#lib/activity-feed.js';
	import type { MemberView } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import ActivityList from './ActivityList.svelte';
	import MapResultsHeader from './MapResultsHeader.svelte';

	let {
		query,
		cell,
		selected,
		fallbackRider,
		onselect,
		onclear
	}: {
		query: string;
		cell: string;
		selected: string;
		fallbackRider?: MemberView | null;
		onselect: (id: string) => void;
		onclear: () => void;
	} = $props();

	type Rider = MemberView & { passages: number; activities: number };
	type Discovery = {
		activities: FeedActivity[];
		matched: number;
		passages: number;
		riders: Rider[];
		has_more: boolean;
	};

	const pageSize = 25;
	const number = $derived(new Intl.NumberFormat(intlLocale()));
	let result = $state.raw<Discovery>();
	let rider = $state('');
	let sort = $state('date_desc');
	let offset = $state(0);
	let retry = $state(0);
	let loading = $state(true);
	let failed = $state(false);

	function label(member: MemberView) {
		return (
			member.display_name.trim() ||
			member.username ||
			fallbackRider?.display_name.trim() ||
			fallbackRider?.username ||
			m.routes_rider()
		);
	}

	$effect(() => {
		void retry;
		const params = new SvelteURLSearchParams(query);
		params.set('cell', cell);
		params.set('sort', sort);
		params.set('limit', String(pageSize));
		params.set('offset', String(offset));
		if (rider) params.set('rider', rider);
		const ctrl = new AbortController();
		loading = true;
		failed = false;
		void fetch('/api/activities/heatmap/discover?' + params, { signal: ctrl.signal })
			.then(async (response) => {
				if (!response.ok) throw new Error(String(response.status));
				const data: Discovery = await response.json();
				if (!ctrl.signal.aborted) result = data;
			})
			.catch(() => {
				if (!ctrl.signal.aborted) failed = true;
			})
			.finally(() => {
				if (!ctrl.signal.aborted) loading = false;
			});
		return () => ctrl.abort();
	});
</script>

<section class="surface min-w-0 space-y-3 p-3 sm:p-4" aria-busy={loading}>
	<MapResultsHeader
		title={result
			? m.heatmap_discovery_passages({ count: number.format(result.passages) })
			: m.heatmap_discovery_title()}
		{onclear}
	/>
	{#if result?.riders.length}
		<div class="flex flex-wrap gap-1.5" role="group" aria-label={m.heatmap_discovery_riders()}>
			{#each result.riders as member (member.id)}
				<button
					type="button"
					aria-pressed={rider === member.id}
					title={m.heatmap_discovery_count({ count: number.format(member.activities) })}
					class="min-h-11 rounded border px-2.5 text-sm {rider === member.id
						? 'border-accent-500 bg-accent-500/10 text-accent-300'
						: 'border-zinc-700 text-zinc-200 hover:border-accent-500'}"
					onclick={() => {
						rider = rider === member.id ? '' : member.id;
						offset = 0;
						onselect('');
					}}
				>
					{label(member)} <span class="ml-1 tabular-nums">{number.format(member.passages)}</span>
				</button>
			{/each}
		</div>
	{/if}
	{#if failed}
		<p role="alert" class="text-sm">
			{m.heatmap_discovery_failed()}
			<button class="ml-1 min-h-10 text-accent-300 underline" onclick={() => retry++}
				>{m.routes_retry()}</button
			>
		</p>
	{:else if result}
		<div class="flex flex-wrap items-center justify-between gap-2">
			<p role="status" class="text-sm text-quiet">
				{loading
					? m.heatmap_discovery_loading()
					: result.matched === 1
						? m.heatmap_discovery_count_one()
						: m.heatmap_discovery_count({ count: number.format(result.matched) })}
			</p>
			{#if result.matched > 1}
				<label class="flex items-center gap-2 text-sm">
					{m.record_sort()}
					<select
						class="min-h-10 rounded border border-zinc-700 bg-panel px-2"
						bind:value={sort}
						onchange={() => (offset = 0)}
					>
						{#each SORTS as option (option.value)}
							<option value={option.value}>{option.label}</option>
						{/each}
					</select>
				</label>
			{/if}
		</div>
		{#if !loading}
			{#if result.activities.length}
				<ActivityList
					items={result.activities}
					{loading}
					{selected}
					riderName={(activity) => {
						const member = result?.riders.find((r) => r.id === activity.rider_id);
						return member && label(member);
					}}
					onselect={(activity) => onselect(selected === activity.id ? '' : activity.id)}
				/>
			{:else}
				<p class="text-sm text-quiet">{m.heatmap_discovery_empty()}</p>
			{/if}
		{/if}
		{#if offset > 0 || result.has_more}
			<nav class="flex flex-wrap gap-2" aria-label={m.heatmap_discovery_pages()}>
				<button
					class="min-h-10 surface px-3 text-sm disabled:opacity-50"
					disabled={loading || offset === 0}
					onclick={() => (offset -= pageSize)}
				>
					{m.heatmap_discovery_previous()}
				</button>
				<button
					class="min-h-10 surface px-3 text-sm disabled:opacity-50"
					disabled={loading || !result.has_more}
					onclick={() => (offset += pageSize)}
				>
					{m.heatmap_discovery_next()}
				</button>
			</nav>
		{/if}
	{:else}
		<p role="status" class="text-sm text-quiet">{m.heatmap_discovery_loading()}</p>
	{/if}
</section>
