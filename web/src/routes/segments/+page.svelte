<script lang="ts">
	import SortHeader from '#lib/components/SortHeader.svelte';
	import { promoteSort } from '#lib/stacked-sort.js';
	import { untrack, onMount } from 'svelte';
	import { memberURL } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import { formatMetricDistance, formatDuration, formatSpeedKmh, intlLocale } from '#lib/format.js';

	import {
		fetchSegmentCatalog,
		defaultCatalogSort,
		type CatalogSort,
		type SortColumn,
		type CatalogSegment,
		type MapCamera
	} from '#lib/segment-catalog.js';

	let { data } = $props();
	const columns = $derived([
		{ key: 'name', label: m.segment_column_name(), align: 'text-left' },
		{ key: 'distance_m', label: m.detail_stat_distance(), align: 'text-right' },
		{ key: 'best_elapsed_s', label: m.segment_leaderboard_time(), align: 'text-right' },
		{ key: 'average_speed_kmh', label: m.segment_avg_speed(), align: 'text-right' },
		{ key: 'local_kom_holder', label: m.segment_local_kom(), align: 'text-left' },
		{ key: 'member_count', label: m.segment_column_members(), align: 'text-right' },
		{
			key: 'my_attempts',
			label: data.member ? m.segment_attempts() : m.segment_my_attempts(),
			align: 'text-right'
		}
	] satisfies { key: SortColumn; label: string; align: string }[]);

	let segments = $state<CatalogSegment[]>([]);
	let mine = $state(untrack(() => !!data.member));
	let selectedOrder = $state<CatalogSort[] | null>(null);
	const order = $derived(selectedOrder ?? defaultCatalogSort(mine));
	let sorting = $state(false);
	let hasMore = $state(false);
	let loading = $state(true);
	let failed = $state(false);
	let search = $state('');
	let searchInput: HTMLInputElement;
	let view = $state<'list' | 'map'>('list');
	let mapCenter = $state<[number, number]>();
	let camera = $state<MapCamera>();
	let request: AbortController | undefined;
	let searchTimer: ReturnType<typeof setTimeout> | undefined;
	const name = $derived(search.trim());

	async function load(offset: number) {
		request?.abort();
		const controller = new AbortController();
		request = controller;
		loading = true;
		failed = false;
		try {
			const body = await fetchSegmentCatalog(
				{
					limit: '50',
					offset: String(offset),
					...(mine ? { mine: 'true' } : {}),
					...(data.member ? { member: data.member.username } : {}),
					...(name ? { q: name } : {})
				},
				order,
				controller.signal
			);
			if (controller.signal.aborted) return;
			segments = offset === 0 ? body.segments : [...segments, ...body.segments];
			if (!offset) mapCenter = segments.find((segment) => segment.start)?.start ?? undefined;
			hasMore = body.has_more;
			sorting = false;
		} catch {
			if (!controller.signal.aborted) failed = true;
		} finally {
			if (!controller.signal.aborted) loading = false;
		}
	}

	function setFilter(onlyMine: boolean) {
		if (mine === onlyMine) return;
		mine = onlyMine;
		if (view === 'list') {
			sorting = false;
			reload();
		} else {
			cancelLoad();
		}
	}

	function setOrder(next: CatalogSort[]) {
		selectedOrder = next;
		if (view === 'list') {
			// Keep the headers mounted so the active button retains focus.
			sorting = true;
			reload();
		}
	}

	function cancelLoad() {
		clearTimeout(searchTimer);
		request?.abort();
	}

	function reload(delay = 0) {
		cancelLoad();
		segments = [];
		hasMore = false;
		failed = false;
		loading = true;
		if (delay) searchTimer = setTimeout(() => void load(0), delay);
		else void load(0);
	}

	function setSearch(text: string) {
		const changed = text.trim() !== name;
		search = text;
		if (!changed) return;
		if (view === 'list') reload(text.trim() ? 150 : 0);
		else cancelLoad();
	}

	function setView(next: typeof view) {
		if (view === next) return;
		view = next;
		if (next === 'list') {
			sorting = false;
			reload();
		}
	}

	onMount(() => {
		void load(0);
		return cancelLoad;
	});
</script>

<div class="space-y-6">
	<header>
		<h1 class="text-2xl font-semibold tracking-tight">{m.nav_segments()}</h1>
		<p class="mt-1 text-sm text-zinc-400">{m.segment_catalog_coverage()}</p>
	</header>

	<div class="flex gap-2" role="group" aria-label={m.segment_filter_label()}>
		{#each [{ mine: false, label: m.segment_filter_all() }, { mine: true, label: data.member ? m.member_segments() : m.segment_filter_mine() }] as filter (filter.label)}
			<button
				type="button"
				aria-pressed={mine === filter.mine}
				onclick={() => setFilter(filter.mine)}
				class="rounded border px-3 py-1.5 text-sm {mine === filter.mine
					? 'border-accent-500 bg-accent-500/15 text-accent-200'
					: 'border-zinc-700 text-zinc-400 hover:border-zinc-600'}"
			>
				{filter.label}
			</button>
		{/each}
	</div>

	<div class="flex flex-wrap items-center justify-between gap-3">
		<div
			class="flex min-w-0 flex-1 items-stretch rounded-md border border-zinc-700 bg-panel focus-within:border-accent-500 max-sm:basis-full"
		>
			<label class="sr-only" for="segment-search">{m.segment_search_placeholder()}</label>
			<input
				type="search"
				id="segment-search"
				bind:this={searchInput}
				value={search}
				oninput={(event) => setSearch(event.currentTarget.value)}
				placeholder={m.segment_search_placeholder()}
				class="min-h-11 min-w-0 flex-1 bg-transparent px-3 text-sm text-zinc-200 outline-none"
			/>
			{#if search}
				<button
					type="button"
					aria-label={m.segment_search_clear()}
					class="px-3 text-sm text-accent-300 hover:underline"
					onclick={() => {
						setSearch('');
						searchInput.focus();
					}}>{m.segment_search_clear_label()}</button
				>
			{/if}
		</div>
		<div
			class="inline-flex rounded-md border border-zinc-700 bg-panel p-0.5"
			role="group"
			aria-label={m.segment_view_label()}
		>
			{#each [{ id: 'list', label: m.segment_view_list() }, { id: 'map', label: m.segment_view_map() }] as option (option.id)}
				<button
					type="button"
					aria-pressed={view === option.id}
					onclick={() => setView(option.id as typeof view)}
					class="min-h-10 rounded px-4 text-sm {view === option.id
						? 'bg-accent-500/15 text-accent-200'
						: 'text-zinc-400 hover:text-zinc-200'}"
				>
					{option.label}
				</button>
			{/each}
		</div>
	</div>

	{#if view === 'map'}
		{#await import('#lib/components/SegmentCatalogMap.svelte')}
			<p role="status" class="text-sm text-quiet">{m.community_loading()}</p>
		{:then { default: SegmentCatalogMap }}
			<SegmentCatalogMap
				{name}
				{mine}
				{order}
				member={data.member}
				center={mapCenter}
				{camera}
				onCamera={(value) => (camera = value)}
			/>
		{:catch}
			<p role="status" class="text-sm text-quiet">{m.segment_map_unavailable()}</p>
		{/await}
	{:else}
		{#if segments.length > 0 || sorting}
			<div class="overflow-x-auto rounded-lg border border-zinc-800">
				<table class="w-full text-sm" aria-busy={loading}>
					<thead class="bg-panel text-xs uppercase tracking-wide text-zinc-500">
						<tr>
							{#each columns as column (column.key)}
								{@const index = order.findIndex((item) => item.column === column.key)}
								{@const active = order[index]}
								<SortHeader
									label={column.label}
									align={column.align}
									{index}
									direction={active?.direction}
									onclick={() => setOrder(promoteSort(order, column.key))}
								/>
							{/each}
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each segments as segment (segment.id)}
							<tr class="hover:bg-panel">
								<th scope="row" class="px-4 py-3 text-left font-medium">
									<a
										href={memberURL(`/segment/${segment.id}`, data.member)}
										class="text-accent-400 hover:underline"
									>
										{segment.name}
									</a>
								</th>
								<td class="px-4 py-3 text-right whitespace-nowrap tabular-nums">
									{formatMetricDistance(segment.distance_m)}
								</td>
								<td class="px-4 py-3 text-right tabular-nums"
									>{formatDuration(segment.best_elapsed_s)}</td
								>
								<td class="px-4 py-3 text-right whitespace-nowrap tabular-nums">
									{formatSpeedKmh(segment.average_speed_kmh)}
								</td>
								<td class="px-4 py-3">{segment.local_kom_holder}</td>
								<td class="px-4 py-3 text-right tabular-nums">
									{segment.member_count.toLocaleString(intlLocale())}
								</td>
								<td class="px-4 py-3 text-right tabular-nums"
									>{segment.my_attempts.toLocaleString(intlLocale())}</td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
		{#if segments.length === 0 && !loading && !failed}
			<p
				class="rounded-lg border border-dashed border-zinc-700 p-8 text-center text-sm text-zinc-400"
			>
				{name
					? m.segment_search_empty()
					: mine
						? m.segment_catalog_empty_mine()
						: m.segment_catalog_empty()}
			</p>
		{/if}

		{#if failed}
			<p role="status" class="text-sm text-red-300">{m.segment_catalog_failed()}</p>
		{/if}
		{#if hasMore || failed}
			<button
				type="button"
				disabled={loading}
				onclick={() => load(segments.length)}
				class="rounded border border-zinc-700 px-4 py-2 text-sm text-zinc-300 hover:border-accent-500 hover:text-accent-300 disabled:opacity-50"
			>
				{failed ? m.community_retry() : m.community_load_more()}
			</button>
		{/if}
		{#if loading}
			<p role="status" class="text-sm text-quiet">{m.community_loading()}</p>
		{/if}
	{/if}
</div>

<style>
	input[type='search']::-webkit-search-cancel-button {
		display: none;
	}
</style>
