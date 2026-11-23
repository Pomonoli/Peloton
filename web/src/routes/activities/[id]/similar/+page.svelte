<script lang="ts">
	import {
		formatDistance,
		formatDuration,
		formatDateOnly,
		formatPace,
		formatSpeed
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

	const chart = $derived.by(() => {
		if (!payload || payload.activities.length < 2) return null;
		const rows = payload.activities;
		const times = rows
			.map((r) => r.average_speed_kmh)
			.filter((v): v is number => v != null && v > 0);
		if (times.length < 2) return null;
		const highest = Math.max(...times);
		const max = isRide ? highest : 3600 / Math.min(...times);
		const W = 760;
		const H = 160;
		const pad = 8;
		const n = rows.length;
		const bw = (W - pad * 2) / n;
		return {
			W,
			H,
			pad,
			bw,
			max,
			bars: rows.map((r, i) => {
				const speed = r.average_speed_kmh;
				const value = speed && (isRide ? speed : 3600 / speed);
				const frac = value ? value / max : 0;
				const h = Math.max(2, frac * (H - pad * 2));
				return {
					x: pad + i * bw,
					y: H - pad - h,
					h,
					w: Math.max(1, bw - 2),
					row: r,
					isBest: speed === highest
				};
			})
		};
	});
</script>

<section class="space-y-8">
	<header>
		<a href={`/activities/${a.id}`} class="text-xs text-accent-400 hover:text-accent-300">
			← {a.title || 'Activity'}
		</a>
		<h1 class="mt-2 text-2xl font-semibold tracking-tight">Similar routes</h1>
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
				<div class="rounded-lg border border-zinc-800 bg-panel p-4">
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

		{#if chart}
			<section>
				<h2 class="mb-3 text-sm font-medium uppercase tracking-wide text-zinc-400">
					{isRide ? 'Average moving speed' : 'Average moving pace'} (oldest → newest)
				</h2>
				<svg
					viewBox={`0 0 ${chart.W} ${chart.H}`}
					class="w-full"
					role="img"
					aria-label={isRide ? 'Average moving speed trend' : 'Average moving pace trend'}
				>
					{#each chart.bars as b (b.row.id)}
						<rect
							x={b.x}
							y={b.y}
							width={b.w}
							height={b.h}
							rx="1.5"
							class={b.row.is_current
								? 'fill-accent-400'
								: b.isBest
									? 'fill-emerald-500'
									: 'fill-zinc-600'}
						>
							<title>
								{formatDateOnly(b.row.start_time)} · {performance(b.row.average_speed_kmh)}
								{#if isRide}
									· {m.ride_elapsed()} {formatDuration(b.row.elapsed_s)}
								{/if}
							</title>
						</rect>
					{/each}
				</svg>
				<div class="mt-1 flex gap-4 text-[11px] text-zinc-500">
					<span
						><span class="inline-block h-2 w-2 rounded-sm bg-accent-400"></span> this effort</span
					>
					<span><span class="inline-block h-2 w-2 rounded-sm bg-emerald-500"></span> fastest</span>
				</div>
			</section>
		{/if}

		<section>
			<h2 class="mb-3 text-sm font-medium uppercase tracking-wide text-zinc-400">All efforts</h2>
			<div class="overflow-x-auto rounded-lg border border-zinc-800">
				<table class="w-full text-sm">
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
								<td class="px-3 py-2 text-right tabular-nums">{formatDistance(r.distance_m)}</td>
								<td class="px-3 py-2 text-right font-medium tabular-nums">
									{formatDuration(r.moving_s)}
								</td>
								{#if isRide}
									<td class="px-3 py-2 text-right whitespace-nowrap tabular-nums">
										{formatDuration(r.elapsed_s)}
									</td>
								{/if}
								<td class="px-3 py-2 text-right tabular-nums"
									>{performance(
										r.average_speed_kmh
									)}{#if r.average_speed_kmh != null && r.average_speed_kmh === payload.summary.best_speed_kmh}<span
											class="ml-1 text-emerald-400">★</span
										>{/if}</td
								>
								<td class="px-3 py-2 text-right tabular-nums">{r.avg_heart_rate ?? '—'}</td>
								<td class="px-3 py-2 text-right tabular-nums">{r.avg_power ?? '—'}</td>
								<td class="px-3 py-2 text-right tabular-nums text-zinc-400">
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
