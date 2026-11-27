<script lang="ts">
	import HistoryBars from '#lib/components/HistoryBars.svelte';
	import { prefs } from '#lib/prefs.svelte.js';
	import {
		formatDistance,
		formatDuration,
		formatDateOnly,
		formatPace,
		formatSpeed,
		intlLocale
	} from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	const a = $derived(data.activity);
	const isRide = $derived(a.type === 'ride');

	type Row = {
		id: string;
		is_current: boolean;
		title: string;
		type: string;
		start_time: string;
		distance_m: number | null;
		moving_s: number;
		elapsed_s: number;
		avg_heart_rate: number | null;
		avg_power: number | null;
		pace_s_per_km: number | null;
		average_speed_kmh: number | null;
		overlap_pct: number;
	};
	type Payload = {
		target_segments: number;
		activities: Row[];
		summary: {
			count: number;
			best_speed_kmh: number | null;
			current_speed_kmh: number | null;
			current_rank: number;
			is_best: boolean;
		};
	};

	let payload = $state<Payload | null>(null);
	let loadError = $state<string | null>(null);

	async function load(id: string, signal: AbortSignal) {
		try {
			const res = await fetch(`/api/activities/${id}/similar`, { signal });
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
		void load(a.id, request.signal);
		return () => request.abort();
	});

	function performance(speed: number | null): string {
		const mps = speed == null ? null : speed / 3.6;
		return isRide ? formatSpeed(mps) : formatPace(mps);
	}

	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 1 }));
	const chart = $derived.by(() => {
		const rows = payload?.activities ?? [];
		const highest = rows.reduce((max, row) => Math.max(max, row.average_speed_kmh ?? 0), 0);
		return rows.map((row) => {
			const speed = row.average_speed_kmh;
			const value =
				speed != null && Number.isFinite(speed) && speed > 0
					? isRide
						? speed / (prefs.units === 'imperial' ? 1.609344 : 1)
						: (3600 / speed) * (prefs.units === 'imperial' ? 1.609344 : 1)
					: null;
			return {
				id: row.id,
				date: row.start_time,
				value,
				title: `${formatDateOnly(row.start_time)} · ${performance(speed)}${isRide ? ` · ${m.ride_elapsed()} ${formatDuration(row.elapsed_s)}` : ''}`,
				color: row.is_current
					? 'var(--accent-primary)'
					: speed === highest
						? 'var(--success-strong)'
						: 'var(--plot-axis)'
			};
		});
	});

	function formatTick(value: number) {
		if (isRide) return `${number.format(value)} ${prefs.units === 'imperial' ? 'mph' : 'km/h'}`;
		return prefs.units === 'imperial'
			? `${formatDuration(value)} /mi`
			: `${formatDuration(value)} /km`;
	}
</script>

<section class="space-y-8">
	<header>
		<a href={`/activities/${a.id}`} class="text-xs text-accent-400 hover:text-accent-300">
			← {a.title || 'Activity'}
		</a>
		<h1 class="mt-2 page-title">Similar routes</h1>
		<p class="mt-1 text-sm text-zinc-400">
			Activities of the same sport that share at least 80% of both GPS routes in the same direction.
		</p>
	</header>

	{#if loadError}
		<div class="rounded-lg border border-red-900/50 bg-red-950/30 p-4 text-sm text-red-300">
			{loadError}
		</div>
	{:else if payload && payload.summary.count > 1}
		<dl class="grid grid-cols-2 gap-4 sm:grid-cols-4">
			{#each [{ label: 'Times on this route', value: String(payload.summary.count) }, { label: 'This effort ranks', value: payload.summary.current_rank ? `#${payload.summary.current_rank}` : '—' }, { label: 'Your best', value: performance(payload.summary.best_speed_kmh) }, { label: 'This effort', value: performance(payload.summary.current_speed_kmh) }] as c (c.label)}
				<div class="surface p-4">
					<dt class="text-xs uppercase tracking-wide text-zinc-500">{c.label}</dt>
					<dd class="mt-1 text-xl font-semibold tabular-nums">{c.value}</dd>
				</div>
			{/each}
		</dl>

		{#if payload.summary.is_best}
			<div
				class="rounded-lg border border-emerald-800/50 bg-emerald-950/30 p-3 text-sm text-emerald-300"
			>
				🏆 This is the fastest effort on this route.
			</div>
		{/if}

		{#if chart.filter((point) => point.value !== null).length > 1}
			<section>
				<h2 class="mb-3 text-sm font-medium uppercase tracking-wide text-zinc-400">
					{isRide ? 'Average moving speed' : 'Average moving pace'} (oldest → newest)
				</h2>
				<HistoryBars
					label={isRide ? m.chart_moving_speed_trend() : m.chart_moving_pace_trend()}
					points={chart}
					{formatTick}
				/>
				<div class="mt-1 flex gap-4 text-[11px] text-zinc-500">
					<span
						><span class="inline-block h-2 w-2 rounded-sm bg-[var(--accent-primary)]"></span> this effort</span
					>
					<span
						><span class="inline-block h-2 w-2 rounded-sm bg-[var(--success-strong)]"></span> fastest</span
					>
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
							<th class="px-3 py-2 text-right font-medium">Distance</th>
							<th class="px-3 py-2 text-right font-medium">{m.col_moving()}</th>
							{#if isRide}
								<th class="px-3 py-2 text-right font-medium">{m.ride_elapsed()}</th>
							{/if}
							<th class="px-3 py-2 text-right font-medium">
								{isRide ? m.col_average_speed() : m.col_average_pace()}
							</th>
							<th class="px-3 py-2 text-right font-medium">HR</th>
							<th class="px-3 py-2 text-right font-medium">Power</th>
							<th class="px-3 py-2 text-right font-medium">Overlap</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each [...payload.activities].reverse() as r (r.id)}
							<tr class={r.is_current ? 'bg-accent-500/5' : ''}>
								<td class="px-3 py-2">
									<a href={`/activities/${r.id}`} class="text-accent-400 hover:text-accent-300">
										{formatDateOnly(r.start_time)}
									</a>
									{#if r.is_current}<span class="ml-1 text-[10px] text-accent-300">(this)</span
										>{/if}
								</td>
								<td class="numeric px-3 py-2">{formatDistance(r.distance_m)}</td>
								<td class="numeric px-3 py-2 font-medium">
									{formatDuration(r.moving_s)}
								</td>
								{#if isRide}
									<td class="numeric px-3 py-2">
										{formatDuration(r.elapsed_s)}
									</td>
								{/if}
								<td class="numeric px-3 py-2"
									>{performance(
										r.average_speed_kmh
									)}{#if r.average_speed_kmh != null && r.average_speed_kmh === payload.summary.best_speed_kmh}<span
											class="ml-1 text-emerald-400">★</span
										>{/if}</td
								>
								<td class="numeric px-3 py-2">{r.avg_heart_rate ?? '—'}</td>
								<td class="numeric px-3 py-2">{r.avg_power ?? '—'}</td>
								<td class="numeric px-3 py-2 text-zinc-400">
									{r.is_current
										? '—'
										: new Intl.NumberFormat(undefined, {
												style: 'percent',
												maximumFractionDigits: 1
											}).format(r.overlap_pct)}
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
			No other activity shares 80% of this GPS route in the same direction.
		</div>
	{:else}
		<p class="text-sm text-zinc-500">Loading…</p>
	{/if}
</section>
