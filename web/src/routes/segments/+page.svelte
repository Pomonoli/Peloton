<script lang="ts">
	import { onMount } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { formatMetricDistance, formatDuration, formatSpeedKmh, intlLocale } from '#lib/format.js';

	type Segment = {
		id: string;
		name: string;
		distance_m: number;
		best_elapsed_s: number;
		average_speed_kmh: number | null;
		local_kom_holder: string;
		member_count: number;
	};

	let segments = $state<Segment[]>([]);
	let mine = $state(false);
	let hasMore = $state(false);
	let loading = $state(true);
	let failed = $state(false);

	async function load(offset: number) {
		loading = true;
		failed = false;
		try {
			const query = `limit=50&offset=${offset}${mine ? '&mine=true' : ''}`;
			const res = await fetch(`/api/segments/catalog?${query}`);
			if (!res.ok) throw new Error();
			const body = await res.json();
			segments = offset === 0 ? body.segments : [...segments, ...body.segments];
			hasMore = body.has_more;
		} catch {
			failed = true;
		} finally {
			loading = false;
		}
	}

	function setFilter(onlyMine: boolean) {
		if (mine === onlyMine) return;
		mine = onlyMine;
		segments = [];
		hasMore = false;
		void load(0);
	}

	onMount(() => load(0));
</script>

<div class="space-y-6">
	<header>
		<h1 class="text-2xl font-semibold tracking-tight">{m.nav_segments()}</h1>
		<p class="mt-1 text-sm text-zinc-400">{m.segment_catalog_coverage()}</p>
	</header>

	<div class="flex gap-2" role="group" aria-label={m.segment_filter_label()}>
		{#each [{ mine: false, label: m.segment_filter_all() }, { mine: true, label: m.segment_filter_mine() }] as filter (filter.label)}
			<button
				type="button"
				aria-pressed={mine === filter.mine}
				disabled={loading}
				onclick={() => setFilter(filter.mine)}
				class="rounded border px-3 py-1.5 text-sm {mine === filter.mine
					? 'border-accent-500 bg-accent-500/15 text-accent-200'
					: 'border-zinc-700 text-zinc-400 hover:border-zinc-600'}"
			>
				{filter.label}
			</button>
		{/each}
	</div>

	{#if segments.length > 0}
		<div class="overflow-x-auto rounded-lg border border-zinc-800">
			<table class="w-full text-sm">
				<thead class="bg-panel text-xs uppercase tracking-wide text-zinc-500">
					<tr>
						<th scope="col" class="px-4 py-2 text-left">{m.segment_column_name()}</th>
						<th scope="col" class="px-4 py-2 text-right">{m.detail_stat_distance()}</th>
						<th scope="col" class="px-4 py-2 text-right">{m.segment_leaderboard_time()}</th>
						<th scope="col" class="px-4 py-2 text-right">{m.segment_avg_speed()}</th>
						<th scope="col" class="px-4 py-2 text-left">{m.segment_local_kom()}</th>
						<th scope="col" class="px-4 py-2 text-right">{m.segment_column_members()}</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each segments as segment (segment.id)}
						<tr class="hover:bg-panel">
							<th scope="row" class="px-4 py-3 text-left font-medium">
								<a href={`/segment/${segment.id}`} class="text-accent-400 hover:underline">
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
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else if !loading && !failed}
		<p
			class="rounded-lg border border-dashed border-zinc-700 p-8 text-center text-sm text-zinc-400"
		>
			{mine ? m.segment_catalog_empty_mine() : m.segment_catalog_empty()}
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
</div>
