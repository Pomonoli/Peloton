<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { memberURL } from '#lib/member.js';

	type Analysis = {
		dates: string[];
		ctl: number[];
		atl: number[];
		tsb: number[];
		tss: number[];
		coverage?: {
			imported: number;
			sensor_estimate: number;
			duration_estimate: number;
			missing: number;
		};
		current: { ctl: number; atl: number; tsb: number };
	};

	let { data } = $props();

	let days = $state(180);
	let a = $state<Analysis | null>(null);
	let loading = $state(true);

	$effect(() => {
		const url = memberURL(`/api/analysis?days=${days}`, data.member);
		if (!data.user) return;
		const request = new AbortController();
		loading = true;
		a = null;
		void fetch(url, { signal: request.signal })
			.then(async (response) => {
				if (!response.ok) throw new Error();
				const result: Analysis = await response.json();
				if (!request.signal.aborted) a = result;
			})
			.catch(() => {})
			.finally(() => {
				if (!request.signal.aborted) loading = false;
			});
		return () => request.abort();
	});

	// ---- inline SVG chart geometry --------------------------------------
	let chartWidth = $state(880);
	const W = $derived(Math.max(240, chartWidth));
	const H = $derived(W < 500 ? 200 : 280);
	const PAD = { t: 16, r: 16, b: 28, l: 40 };
	const plotW = $derived(W - PAD.l - PAD.r);
	const plotH = $derived(H - PAD.t - PAD.b);

	type Series = { key: 'ctl' | 'atl' | 'tsb'; label: string; description: string; color: string };
	const series: Series[] = [
		{
			key: 'ctl',
			label: 'Fitness (CTL)',
			description: m.analysis_fitness_description(),
			color: 'var(--plot-speed)'
		},
		{
			key: 'atl',
			label: 'Fatigue (ATL)',
			description: m.analysis_fatigue_description(),
			color: 'var(--plot-fatigue)'
		},
		{
			key: 'tsb',
			label: 'Form (TSB)',
			description: m.analysis_form_description(),
			color: 'var(--plot-form)'
		}
	];

	const bounds = $derived.by(() => {
		if (!a || a.dates.length === 0) return { min: 0, max: 1 };
		let min = Infinity;
		let max = -Infinity;
		for (const s of series) {
			for (const v of a[s.key]) {
				if (v < min) min = v;
				if (v > max) max = v;
			}
		}
		if (!isFinite(min)) return { min: 0, max: 1 };
		// pad a touch so lines aren't glued to edges
		const span = max - min || 1;
		return { min: min - span * 0.08, max: max + span * 0.08 };
	});

	function x(i: number, n: number): number {
		if (n <= 1) return PAD.l;
		return PAD.l + (i / (n - 1)) * plotW;
	}
	function y(v: number): number {
		const { min, max } = bounds;
		return PAD.t + plotH - ((v - min) / (max - min || 1)) * plotH;
	}
	function path(vals: number[]): string {
		const n = vals.length;
		return vals
			.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i, n).toFixed(1)},${y(v).toFixed(1)}`)
			.join(' ');
	}

	// y=0 gridline (form crosses zero — meaningful for TSB)
	const zeroY = $derived(bounds.min <= 0 && bounds.max >= 0 ? y(0) : null);

	function fmt(v: number): string {
		return v >= 0 ? `+${v.toFixed(1)}` : v.toFixed(1);
	}

	// hover crosshair
	let hover = $state<number | null>(null);
	function onMove(e: MouseEvent, n: number) {
		const svg = e.currentTarget as SVGSVGElement;
		const rect = svg.getBoundingClientRect();
		const px = ((e.clientX - rect.left) / rect.width) * W;
		const i = Math.round(((px - PAD.l) / plotW) * (n - 1));
		hover = Math.max(0, Math.min(n - 1, i));
	}

	const ranges = [
		{ label: '3M', d: 90 },
		{ label: '6M', d: 180 },
		{ label: '1Y', d: 365 },
		{ label: 'All', d: 3650 }
	];
</script>

<div class="space-y-4 max-md:space-y-3">
	<header>
		<h1 class="text-3xl font-semibold tracking-tight max-md:text-2xl">Analysis</h1>
	</header>

	{#if !data.user}
		<a
			href="/login"
			class="inline-block rounded border border-accent-500 bg-accent-500/20 px-4 py-2 text-sm text-accent-300 hover:bg-accent-500/30"
		>
			Sign in
		</a>
	{:else}
		<!-- range picker -->
		<div
			role="group"
			aria-label="Training load period"
			class="grid max-w-sm grid-cols-4 gap-0.5 rounded-xl bg-zinc-800/65 p-0.5"
		>
			{#each ranges as r (r.d)}
				<button
					type="button"
					aria-pressed={days === r.d}
					onclick={() => (days = r.d)}
					class="min-h-10 rounded-[10px] px-2 text-xs font-semibold transition-colors"
					class:bg-panel={days === r.d}
					class:text-accent-300={days === r.d}
					class:text-zinc-400={days !== r.d}
					class:hover:text-zinc-100={days !== r.d}
				>
					{r.label}
				</button>
			{/each}
		</div>

		<!-- current snapshot -->
		{#if a}
			<div
				class="grid grid-cols-3 gap-px overflow-hidden rounded-xl border border-zinc-800 bg-zinc-800"
			>
				{#each series as s (s.key)}
					<div class="min-w-0 bg-zinc-900 p-2 sm:p-4">
						<div class="flex items-center gap-1 text-xs font-semibold text-quiet sm:gap-2">
							<span class="inline-block size-2 shrink-0 rounded-full" style:background={s.color}
							></span>
							{s.label}
						</div>
						<div class="mt-1 text-lg font-bold tabular-nums sm:text-2xl text-zinc-100">
							{a.dates.length
								? s.key === 'tsb'
									? fmt(a.current[s.key])
									: a.current[s.key].toFixed(1)
								: '—'}
						</div>
						<p class="mt-1 text-xs text-quiet">{s.description}</p>
					</div>
				{/each}
			</div>
		{/if}

		{#if a?.coverage}
			<dl class="flex flex-wrap gap-x-5 gap-y-1 text-xs">
				{#each [{ label: m.analysis_stress_imported(), count: a.coverage.imported }, { label: m.analysis_stress_sensor(), count: a.coverage.sensor_estimate }, { label: m.analysis_stress_duration(), count: a.coverage.duration_estimate }, { label: m.analysis_stress_missing(), count: a.coverage.missing }] as item (item.label)}
					<div class="flex gap-1">
						<dt class="text-quiet">{item.label}</dt>
						<dd class="font-semibold tabular-nums">{item.count}</dd>
					</div>
				{/each}
			</dl>
		{:else if a?.dates.length}
			<p class="text-xs text-quiet">{m.analysis_training_estimated()}</p>
		{/if}

		<!-- chart -->
		<div class="min-w-0 rounded-xl border border-zinc-800 bg-panel p-2 sm:p-4">
			{#if loading}
				<div
					class="flex min-h-32 items-center justify-center text-center sm:min-h-56 text-sm text-zinc-500"
				>
					Loading…
				</div>
			{:else if !a}
				<p role="alert" class="text-sm text-red-300">{m.analysis_failed()}</p>
			{:else if a.dates.length === 0}
				<div
					class="flex min-h-32 items-center justify-center text-center sm:min-h-56 text-sm text-zinc-500"
				>
					No training-load data yet. Import some activities first.
				</div>
			{:else}
				{@const n = a.dates.length}
				<div bind:clientWidth={chartWidth} class="min-w-0">
					<svg
						viewBox={`0 0 ${W} ${H}`}
						class="w-full"
						role="img"
						aria-label="Training load trend"
						onpointermove={(e) => onMove(e, n)}
						onpointerdown={(e) => onMove(e, n)}
						onpointerleave={(e) => {
							if (e.pointerType === 'mouse') hover = null;
						}}
					>
						<!-- y axis gridlines -->
						{#each [0, 0.25, 0.5, 0.75, 1] as t (t)}
							{@const gy = PAD.t + plotH * t}
							{@const val = bounds.max - (bounds.max - bounds.min) * t}
							<line
								x1={PAD.l}
								y1={gy}
								x2={W - PAD.r}
								y2={gy}
								stroke="var(--plot-grid)"
								stroke-width="1"
							/>
							<text x={PAD.l - 6} y={gy + 3} text-anchor="end" class="fill-quiet text-[10px]">
								{val.toFixed(0)}
							</text>
						{/each}

						<!-- zero line for TSB -->
						{#if zeroY !== null}
							<line
								x1={PAD.l}
								y1={zeroY}
								x2={W - PAD.r}
								y2={zeroY}
								stroke="var(--plot-grid)"
								stroke-width="1"
								stroke-dasharray="3 3"
							/>
						{/if}

						<!-- series -->
						{#each series as s (s.key)}
							<path d={path(a[s.key])} fill="none" stroke={s.color} stroke-width="1.8" />
						{/each}

						<!-- hover crosshair -->
						{#if hover !== null}
							{@const hx = x(hover, n)}
							<line
								x1={hx}
								y1={PAD.t}
								x2={hx}
								y2={PAD.t + plotH}
								stroke="var(--plot-cursor)"
								stroke-width="1"
							/>
							{#each series as s (s.key)}
								{@const v = a[s.key][hover]}
								{#if v !== undefined}
									<circle cx={hx} cy={y(v)} r="3" fill={s.color} />
								{/if}
							{/each}
						{/if}
					</svg>
				</div>

				<!-- legend / hover readout -->
				<div class="mt-2 flex flex-wrap items-center justify-between gap-3 text-xs">
					<div class="flex flex-wrap gap-4">
						{#each series as s (s.key)}
							{@const v = hover === null ? undefined : a[s.key][hover]}
							<span class="flex items-center gap-1.5 text-zinc-400">
								<span class="inline-block h-2 w-2 rounded-full" style:background={s.color}></span>
								{s.label}
								{#if v !== undefined}
									<span class="tabular-nums text-zinc-200">
										{s.key === 'tsb' ? fmt(v) : v.toFixed(1)}
									</span>
								{/if}
							</span>
						{/each}
					</div>
					{#if hover !== null}
						<span class="tabular-nums text-zinc-500">{a.dates[hover]}</span>
					{/if}
				</div>
			{/if}
		</div>
	{/if}
</div>
