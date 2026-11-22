<script lang="ts">
	import { SvelteURLSearchParams } from 'svelte/reactivity';
	import { formatDateOnly, intlLocale, formatSpeedKmh } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		alignEffortHistory,
		rivalryFrom,
		type HistoryEffort,
		type RivalryPeriod
	} from '#lib/segment-rivalries.js';
	import StreamChart from './StreamChart.svelte';

	let { id }: { id: string } = $props();

	interface Entry {
		rank: number;
		user_id: string;
		display_name: string;
		elapsed_s: number;
		effort_count: number;
		all_efforts_speed_kmh: number | null;
		start_time: string;
		activity_id: string;
	}

	const pageSize = 20;
	const historySize = 200;
	const colors = [
		'var(--plot-speed)',
		'var(--plot-power)',
		'var(--plot-cadence)',
		'var(--plot-heart-rate)'
	];
	let period = $state<RivalryPeriod>('all');
	let board = $state<Entry[]>([]);
	let loading = $state(true);
	let boardError = $state(false);
	let more = $state(false);
	let selected = $state<string[]>([]);
	let histories = $state<HistoryEffort[][]>([]);
	let historyError = $state(false);
	let historyLoading = $state(false);
	let boardRequest: AbortController | undefined;

	const from = $derived(rivalryFrom(period, new Date()));
	const compared = $derived(
		selected
			.map((uid) => board.find((entry) => entry.user_id === uid))
			.filter((entry): entry is Entry => !!entry)
	);
	const chart = $derived(alignEffortHistory(histories));
	const series = $derived(
		compared.map((entry, i) => ({
			values: chart.values[i] ?? [],
			label: entry.display_name,
			color: colors[i]!,
			unit: 's'
		}))
	);

	async function loadBoard(segmentId: string, cutoff: string | null, offset: number) {
		boardRequest?.abort();
		const request = new AbortController();
		boardRequest = request;
		loading = true;
		boardError = false;
		const params = new SvelteURLSearchParams({ limit: String(pageSize), offset: String(offset) });
		if (cutoff) params.set('from', cutoff);
		try {
			const response = await fetch(`/api/segments/${segmentId}/leaderboard?${params}`, {
				signal: request.signal
			});
			if (!response.ok) throw new Error();
			const { entries }: { entries: Entry[] } = await response.json();
			if (request.signal.aborted) return;
			board = offset ? [...board, ...entries] : entries;
			more = entries.length === pageSize;
			if (!offset) selected = entries.slice(0, 2).map((entry) => entry.user_id);
		} catch {
			if (!request.signal.aborted) boardError = true;
		} finally {
			if (!request.signal.aborted) loading = false;
		}
	}

	$effect(() => {
		const segmentId = id;
		const cutoff = from;
		board = [];
		selected = [];
		more = false;
		void loadBoard(segmentId, cutoff, 0);
		return () => boardRequest?.abort();
	});

	let historyRetry = $state(0);
	$effect(() => {
		const segmentId = id;
		const cutoff = from;
		const ids = [...selected];
		void historyRetry;
		histories = [];
		historyError = false;
		historyLoading = !!ids.length;
		if (!ids.length) return;
		const request = new AbortController();
		const rows = ids.map(async (uid) => {
			const params = new SvelteURLSearchParams({
				user_id: uid,
				limit: String(historySize),
				valid_only: 'true'
			});
			if (cutoff) params.set('from', cutoff);
			const response = await fetch(`/api/segments/${segmentId}/efforts?${params}`, {
				signal: request.signal
			});
			if (!response.ok) throw new Error();
			return (await response.json()).efforts as HistoryEffort[];
		});
		void Promise.all(rows)
			.then((efforts) => {
				if (!request.signal.aborted) histories = efforts;
			})
			.catch(() => {
				if (!request.signal.aborted) historyError = true;
			})
			.finally(() => {
				if (!request.signal.aborted) historyLoading = false;
			});
		return () => request.abort();
	});

	function seconds(value: number) {
		return new Intl.NumberFormat(intlLocale(), {
			style: 'unit',
			unit: 'second',
			maximumFractionDigits: 2
		}).format(value);
	}

	function toggle(uid: string) {
		if (selected.includes(uid)) selected = selected.filter((value) => value !== uid);
		else if (selected.length < colors.length) selected = [...selected, uid];
	}
</script>

<section class="space-y-3">
	<div class="flex flex-wrap items-end justify-between gap-2">
		<div>
			<h2 class="font-medium">{m.segment_local_kom()}</h2>
			<p class="text-sm text-quiet">{m.segment_rivalry_description()}</p>
		</div>
		<label class="flex items-center gap-2 text-sm">
			{m.segment_rivalry_period()}
			<select bind:value={period} class="min-h-10 rounded-md border border-zinc-700 bg-panel px-2">
				<option value="all">{m.segment_rivalry_all()}</option>
				<option value="year">{m.segment_rivalry_year()}</option>
				<option value="recent">{m.segment_rivalry_recent()}</option>
			</select>
		</label>
	</div>

	{#if board.length}
		<p class="text-sm text-accent-300">{m.segment_winner({ name: board[0]!.display_name })}</p>
		<div class="overflow-x-auto rounded-xl border border-zinc-800">
			<table class="w-full text-sm whitespace-nowrap">
				<thead class="bg-panel text-xs text-quiet">
					<tr>
						<th scope="col" class="px-3 py-2 text-left">{m.segment_rivalry_compare()}</th>
						<th scope="col" class="px-3 py-2 text-left">{m.segment_leaderboard_rank()}</th>
						<th scope="col" class="px-3 py-2 text-left">{m.segment_leaderboard_rider()}</th>
						<th scope="col" class="px-3 py-2 text-right"
							>{period === 'all'
								? m.segment_rivalry_personal_best()
								: m.segment_rivalry_period_best()}</th
						>
						<th scope="col" class="px-3 py-2 text-right">{m.segment_rivalry_gap()}</th>
						<th scope="col" class="px-3 py-2 text-right">{m.segment_avg_speed()}</th>
						<th scope="col" class="px-3 py-2 text-right">{m.segment_rivalry_efforts()}</th>
						<th scope="col" class="px-3 py-2 text-right">{m.segment_leaderboard_date()}</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each board as entry (entry.user_id)}
						{@const gap = entry.elapsed_s - board[0]!.elapsed_s}
						<tr>
							<td class="px-3 py-2">
								<label class="inline-flex min-h-8 min-w-10 items-center justify-center">
									<input
										type="checkbox"
										checked={selected.includes(entry.user_id)}
										disabled={selected.length >= colors.length && !selected.includes(entry.user_id)}
										aria-label={m.segment_rivalry_compare_rider({ name: entry.display_name })}
										onchange={() => toggle(entry.user_id)}
										class="accent-accent-500"
									/>
								</label>
							</td>
							<td class="px-3 py-2 tabular-nums">#{entry.rank}</td>
							<td class="px-3 py-2" class:font-semibold={gap === 0}>{entry.display_name}</td>
							<td class="px-3 py-2 text-right tabular-nums">{seconds(entry.elapsed_s)}</td>
							<td class="px-3 py-2 text-right tabular-nums"
								>{gap === 0 ? m.segment_rivalry_fastest() : `+${seconds(gap)}`}</td
							>
							<td class="px-3 py-2 text-right tabular-nums"
								>{formatSpeedKmh(entry.all_efforts_speed_kmh)}</td
							>
							<td class="px-3 py-2 text-right tabular-nums"
								>{new Intl.NumberFormat(intlLocale()).format(entry.effort_count)}</td
							>
							<td class="px-3 py-2 text-right">
								<a href={`/activities/${entry.activity_id}`} class="text-accent-300 hover:underline"
									>{formatDateOnly(entry.start_time)}</a
								>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else if !loading && !boardError}
		<p class="text-sm text-quiet">{m.segment_leaderboard_empty()}</p>
	{/if}
	{#if boardError}<p role="status" class="text-sm text-quiet">
			{m.segment_leaderboard_failed()}
		</p>{/if}
	{#if more || boardError}
		<button
			type="button"
			disabled={loading}
			class="min-h-10 text-sm text-accent-300 hover:underline disabled:opacity-50"
			onclick={() => loadBoard(id, from, board.length)}
		>
			{boardError ? m.community_retry() : m.segment_leaderboard_next()}
		</button>
	{/if}

	{#if board.length}
		<div class="space-y-2 rounded-xl border border-zinc-800 bg-panel p-3">
			<h3 class="text-sm font-medium">{m.segment_rivalry_history()}</h3>
			{#if historyError}
				<p role="status" class="text-sm text-quiet">{m.segment_rivalry_history_failed()}</p>
				<button
					type="button"
					onclick={() => (historyRetry += 1)}
					class="min-h-10 text-sm text-accent-300 hover:underline">{m.community_retry()}</button
				>
			{:else if historyLoading || loading}
				<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
			{:else if chart.offsets.length}
				{#if compared.some((entry) => entry.effort_count > historySize)}
					<p class="text-xs text-quiet">
						{m.segment_rivalry_history_limit({ count: historySize })}
					</p>
				{/if}
				<StreamChart offsets={chart.offsets} {series} height={210} calendar />
			{:else}
				<p class="text-sm text-quiet">{m.segment_rivalry_history_select()}</p>
			{/if}
		</div>
	{:else if loading}
		<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
	{/if}
</section>
