<script lang="ts">
	import { SvelteURLSearchParams } from 'svelte/reactivity';
	import {
		formatDateOnly,
		formatDistanceEffort,
		formatDuration,
		formatSpeedKmh,
		intlLocale
	} from '#lib/format.js';
	import { memberURL, type MemberView } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		alignEffortHistory,
		rivalryFrom,
		segmentSpeedRange,
		type HistoryEffort,
		type RivalryPeriod
	} from '#lib/segment-rivalries.js';
	import StreamChart from './StreamChart.svelte';

	let { id, distanceM, member }: { id: string; distanceM: number; member: MemberView | null } =
		$props();

	interface Entry {
		rank: number;
		user_id: string;
		display_name: string;
		elapsed_s: number;
		average_speed_kmh: number | null;
		effort_count: number;
		start_time: string;
		activity_id: string;
	}

	const pageSize = 20;
	const boardFetchSize = 200;
	const historySize = 200;
	let period = $state<RivalryPeriod>('all');
	let board = $state.raw<Entry[]>([]);
	let visibleCount = $state(pageSize);
	let loading = $state(true);
	let boardError = $state(false);
	let boardRetry = $state(0);
	let historyRetry = $state(0);
	let histories = $state.raw<HistoryEffort[][]>([]);
	let historyError = $state(false);
	let historyLoading = $state(false);
	let hidden = $state.raw<string[]>([]);
	let hovered = $state<number | null>(null);

	const from = $derived(rivalryFrom(period, new Date()));
	const boardURL = $derived(
		memberURL(`/api/segments/${id}/leaderboard?limit=${boardFetchSize}`, member)
	);
	const chart = $derived(alignEffortHistory(histories, distanceM));
	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 2 }));
	// eslint-disable-next-line svelte/prefer-svelte-reactivity -- stable color allocation is independent of effects
	const colorsByRider = new Map<string, string>();
	function riderColor(uid: string) {
		const existing = colorsByRider.get(uid);
		if (existing) return existing;
		const hue = (colorsByRider.size * 137.508 + 205) % 360;
		const color = `hsl(${hue} 72% 52%)`;
		colorsByRider.set(uid, color);
		return color;
	}
	const riders = $derived(
		board.map((entry, index) => ({
			entry,
			color: riderColor(entry.user_id),
			values: chart.values[index] ?? [],
			times: chart.times[index] ?? []
		}))
	);
	const visible = $derived(riders.filter(({ entry }) => !hidden.includes(entry.user_id)));
	const series = $derived(
		visible.map(({ entry, color, values }) => ({ label: entry.display_name, color, values }))
	);
	const hoverIndex = $derived(hovered === null ? -1 : chart.offsets.indexOf(hovered));

	$effect(() => {
		const url = boardURL;
		const cutoff = from;
		void boardRetry;
		const controller = new AbortController();
		loading = true;
		boardError = false;
		visibleCount = pageSize;
		board = [];
		hidden = [];
		hovered = null;
		void (async () => {
			try {
				const entries: Entry[] = [];
				for (let offset = 0; ; offset += boardFetchSize) {
					const params = new SvelteURLSearchParams({ offset: String(offset) });
					if (cutoff) params.set('from', cutoff);
					const response = await fetch(`${url}&${params}`, { signal: controller.signal });
					if (!response.ok) throw new Error();
					const page: { entries: Entry[] } = await response.json();
					if (controller.signal.aborted) return;
					entries.push(...page.entries);
					if (page.entries.length < boardFetchSize) break;
				}
				board = entries;
			} catch {
				if (!controller.signal.aborted) boardError = true;
			} finally {
				if (!controller.signal.aborted) loading = false;
			}
		})();
		return () => controller.abort();
	});

	let cachedScope = '';
	// eslint-disable-next-line svelte/prefer-svelte-reactivity -- results are published together after the workers settle
	const cache = new Map<string, HistoryEffort[]>();
	$effect(() => {
		const entries = board;
		const segmentId = id;
		const cutoff = from;
		void historyRetry;
		const scope = `${segmentId}/${cutoff ?? ''}`;
		if (scope !== cachedScope) cache.clear();
		cachedScope = scope;
		histories = [];
		historyError = false;
		historyLoading = entries.length > 0;
		if (!entries.length) return;
		const controller = new AbortController();
		let next = 0;
		async function worker() {
			while (!controller.signal.aborted && next < entries.length) {
				const uid = entries[next++]!.user_id;
				if (cache.has(uid)) continue;
				const params = new SvelteURLSearchParams({
					user_id: uid,
					limit: String(historySize),
					valid_only: 'true'
				});
				if (cutoff) params.set('from', cutoff);
				try {
					const response = await fetch(`/api/segments/${segmentId}/efforts?${params}`, {
						signal: controller.signal
					});
					if (!response.ok) throw new Error();
					const { efforts } = (await response.json()) as { efforts: HistoryEffort[] };
					if (!controller.signal.aborted) cache.set(uid, efforts);
				} catch {
					if (!controller.signal.aborted) historyError = true;
				}
			}
		}
		void Promise.all(Array.from({ length: Math.min(4, entries.length) }, worker)).then(() => {
			if (!controller.signal.aborted) {
				histories = entries.map(({ user_id }) => cache.get(user_id) ?? []);
				historyLoading = false;
			}
		});
		return () => controller.abort();
	});

	function toggle(uid: string) {
		hidden = hidden.includes(uid) ? hidden.filter((id) => id !== uid) : [...hidden, uid];
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
		<div class="overflow-x-auto rounded-xl border border-zinc-800">
			<table class="w-full text-xs sm:text-sm">
				<thead class="bg-panel text-xs text-quiet">
					<tr>
						<th scope="col" class="px-1.5 py-2 text-left sm:px-3">{m.segment_leaderboard_rank()}</th
						>
						<th scope="col" class="px-1.5 py-2 text-left sm:px-3"
							>{m.segment_leaderboard_rider()}</th
						>
						<th scope="col" class="px-1.5 py-2 text-right sm:px-3">{m.detail_label_time()}</th>
						<th scope="col" class="px-1.5 py-2 text-right sm:px-3">{m.segment_speed_column()}</th>
						<th scope="col" class="px-1.5 py-2 text-right sm:px-3">{m.segment_attempts()}</th>
						<th scope="col" class="px-1.5 py-2 text-right sm:px-3"
							>{m.segment_leaderboard_date()}</th
						>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each board.slice(0, visibleCount) as entry (entry.user_id)}
						<tr>
							<td class="px-1.5 py-2 tabular-nums sm:px-3">#{entry.rank}</td>
							<td
								class="px-1.5 py-2 sm:px-3"
								class:font-semibold={entry.elapsed_s === board[0]?.elapsed_s}
								>{entry.display_name}</td
							>
							<td class="px-1.5 py-2 text-right tabular-nums sm:px-3"
								>{formatDuration(entry.elapsed_s)}</td
							>
							<td class="px-1.5 py-2 text-right tabular-nums sm:px-3"
								>{formatSpeedKmh(entry.average_speed_kmh)}</td
							>
							<td class="px-1.5 py-2 text-right tabular-nums sm:px-3"
								>{number.format(entry.effort_count)}</td
							>
							<td class="px-1.5 py-2 text-right sm:px-3">
								<a
									href={memberURL(`/activities/${entry.activity_id}`, member)}
									class="text-accent-300 hover:underline">{formatDateOnly(entry.start_time)}</a
								>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
		{#if visibleCount < board.length}
			<button
				type="button"
				class="min-h-10 text-sm text-accent-300 hover:underline"
				onclick={() => (visibleCount += pageSize)}>{m.segment_leaderboard_next()}</button
			>
		{/if}
	{:else if loading}
		<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
	{:else if boardError}
		<p role="status" class="text-sm text-quiet">{m.segment_leaderboard_failed()}</p>
		<button
			type="button"
			class="min-h-10 text-sm text-accent-300 hover:underline"
			onclick={() => (boardRetry += 1)}>{m.community_retry()}</button
		>
	{:else}
		<p class="text-sm text-quiet">{m.segment_leaderboard_empty()}</p>
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
			{/if}
			{#if historyLoading}
				<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
			{:else if chart.offsets.length}
				{#if board.some((entry) => entry.effort_count > historySize)}
					<p class="text-xs text-quiet">
						{m.segment_rivalry_history_limit({ count: historySize })}
					</p>
				{/if}
				{#if series.length}
					<StreamChart
						offsets={chart.offsets}
						{series}
						height={220}
						calendar
						legend={false}
						yAxis={{
							label: m.segment_speed_axis(),
							format: (value) => number.format(value),
							range: segmentSpeedRange
						}}
						onHover={(time) => (hovered = time)}
					/>
				{/if}
				{#if hovered !== null && visible.some(({ times }) => times[hoverIndex] != null)}
					<p class="text-xs text-quiet">{formatDateOnly(new Date(hovered * 1000).toISOString())}</p>
				{/if}
				<div
					role="group"
					aria-label={m.segment_history_legend()}
					class="flex max-h-36 flex-wrap gap-x-2 overflow-y-auto"
				>
					{#each riders as { entry, color, times } (entry.user_id)}
						<button
							type="button"
							aria-pressed={!hidden.includes(entry.user_id)}
							onclick={() => toggle(entry.user_id)}
							class="flex min-h-9 items-center gap-1.5 rounded-lg px-2 text-xs aria-pressed:bg-zinc-800"
						>
							<span class="size-2 shrink-0 rounded-full" style:background={color}></span>
							{entry.display_name}
							{#if !hidden.includes(entry.user_id) && times[hoverIndex] != null}<span
									>{formatDistanceEffort(distanceM, times[hoverIndex])}</span
								>{/if}
						</button>
					{/each}
				</div>
			{:else if !historyError}
				<p class="text-sm text-quiet">{m.segment_leaderboard_empty()}</p>
			{/if}
		</div>
	{/if}
</section>
