<script lang="ts">
	import HistoryBars from '#lib/components/HistoryBars.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import MetricReadout from '#lib/components/MetricReadout.svelte';
	import { memberURL } from '#lib/member.js';

	import { formatDateOnly, formatDuration, intlLocale } from '#lib/format.js';
	import { isCyclingDistanceRecord, recordValue, recordLabel } from '#lib/records.js';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	type Item = {
		activity_id: string;
		title: string;
		start_time: string;
		achieved_value: number;
		is_best: boolean;
	};
	type Payload = {
		best: number;
		count: number;
		items: Item[];
	};

	let payload = $state<Payload | null>(null);
	let loadError = $state<string | null>(null);

	async function load(signal: AbortSignal) {
		try {
			const params = new URLSearchParams({
				type: data.type,
				metric: data.metric,
				window_kind: data.window,
				window_value: String(data.value),
				...(data.excludeDownhills ? { exclude_downhills: 'true' } : {})
			});
			const res = await fetch(memberURL(`/api/best-efforts/history?${params}`, data.member), {
				signal
			});
			if (!res.ok) throw new Error((await res.text()).trim());
			const result = await res.json();
			if (!signal.aborted) payload = result;
		} catch (e) {
			if (!signal.aborted) loadError = (e as Error).message;
		}
	}

	$effect(() => {
		const request = new AbortController();
		payload = null;
		loadError = null;
		void load(request.signal);
		return () => request.abort();
	});

	const bucket = $derived({
		activity_type: data.type,
		metric: data.metric,
		window_kind: data.window,
		window_value: data.value
	});

	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 1 }));
	const cyclingDistance = $derived(isCyclingDistanceRecord(bucket));
	const chart = $derived(
		payload?.items.flatMap((item) => {
			const raw = item.achieved_value;
			if (!Number.isFinite(raw) || raw <= 0) return [];
			return [
				{
					id: item.activity_id,
					date: item.start_time,
					value: cyclingDistance
						? (raw * bucket.window_value) / 1000
						: bucket.metric === 'speed'
							? raw * 3.6
							: bucket.metric === 'vam'
								? (raw * bucket.window_value) / 3600
								: raw,
					title: `${formatDateOnly(item.start_time)} · ${recordValue(bucket, raw)}`,
					color: item.is_best ? 'var(--success-strong)' : 'var(--plot-axis)'
				}
			];
		}) ?? []
	);

	function formatTick(value: number) {
		if (cyclingDistance) return formatDuration(value);
		if (bucket.metric === 'speed') return `${number.format(value)} km/h`;
		if (bucket.metric === 'vam') return `${number.format(value)} m`;
		return recordValue(bucket, value);
	}
</script>

<section class="space-y-8">
	<header>
		<a href="/" class="text-xs text-accent-400 hover:text-accent-300">← Overview</a>
		<h1 class="mt-2 page-title">
			Best effort · {recordLabel(bucket, false)}
		</h1>
		<p class="mt-1 text-sm text-zinc-400">
			Your progression for this effort across all activities.
		</p>
	</header>

	{#if loadError}
		<div class="rounded-lg border border-red-900/50 bg-red-950/30 p-4 text-sm text-red-300">
			{loadError}
		</div>
	{:else if payload && payload.count > 0}
		<dl class="grid grid-cols-2 gap-4 sm:grid-cols-3">
			<div class="surface p-4">
				<dt class="text-xs uppercase tracking-wide text-zinc-500">All-time best</dt>
				<dd class="mt-1 text-2xl font-semibold tabular-nums text-emerald-300">
					{recordValue(bucket, payload.best)}
				</dd>
			</div>
			<div class="surface p-4">
				<dt class="text-xs uppercase tracking-wide text-zinc-500">Activities</dt>
				<dd class="mt-1 text-2xl font-semibold tabular-nums">{payload.count}</dd>
			</div>
		</dl>

		{#if chart.length > 1}
			<section>
				<h2 class="mb-3 text-sm font-medium uppercase tracking-wide text-zinc-400">
					Over time (oldest → newest)
				</h2>
				<HistoryBars label={m.chart_best_effort_trend()} points={chart} {formatTick} />
				<div class="mt-1 text-[11px] text-zinc-500">
					<span class="inline-block h-2 w-2 rounded-sm bg-[var(--success-strong)]"></span> all-time best
				</div>
			</section>
		{/if}

		<section>
			<h2 class="mb-3 text-sm font-medium uppercase tracking-wide text-zinc-400">All efforts</h2>
			<div class="table-scroll rounded-lg border border-zinc-800">
				<table class="data-table w-full text-sm">
					<thead class="bg-panel text-xs uppercase tracking-wide text-zinc-500">
						<tr>
							<th class="px-3 py-2 text-left font-medium">Date</th>
							<th class="px-3 py-2 text-left font-medium">Activity</th>
							<th class="px-3 py-2 text-right font-medium">Value</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each [...payload.items].reverse() as it (it.activity_id)}
							<tr class={it.is_best ? 'bg-emerald-500/5' : ''}>
								<td class="px-3 py-2 tabular-nums">{formatDateOnly(it.start_time)}</td>
								<td class="name-cell px-3 py-2">
									<a
										href={`/activities/${it.activity_id}`}
										class="table-name text-accent-400 hover:text-accent-300"
										title={it.title}
									>
										{it.title || 'Activity'}
									</a>
								</td>
								<td class="px-3 py-2 text-right font-medium tabular-nums">
									<MetricReadout value={recordValue(bucket, it.achieved_value)} />
									{#if it.is_best}<span class="ml-1 text-emerald-400">★</span>{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{:else if payload}
		<div
			class="rounded-lg border border-dashed border-zinc-700 bg-zinc-900/30 p-8 text-center text-sm text-zinc-400"
		>
			No efforts recorded for this bucket.
		</div>
	{:else}
		<p class="text-sm text-zinc-500">Loading…</p>
	{/if}
</section>
