<script lang="ts">
	import { axisTicks, elevationScale, tickStep } from '#lib/chart-scale.js';
	import { formatDuration, intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import ChartAxes from './ChartAxes.svelte';

	type Point = { distM: number | null; eleM: number | null; t?: number; connected?: boolean };

	let {
		track = [],
		highlight = null,
		height = 96
	}: {
		track: Point[];
		highlight?: { startDist: number; endDist: number } | null;
		height?: number;
	} = $props();

	let width = $state(0);
	const top = 9;
	const right = $derived(width - 12);
	const bottom = $derived(top + height);
	const id = $props.id();
	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 2 }));

	const data = $derived.by(() => {
		const distances = track.map((point) => point.distM).filter(finite);
		const times = track.map((point) => point.t).filter(finite);
		const hasRange = (values: number[]) => values.length > 1 && values.at(-1)! > values[0]!;
		const distance = hasRange(distances);
		const time = !distance && hasRange(times);
		if (!distance && !time) return null;

		const runs: { x: number; y: number }[][] = [];
		let run: (typeof runs)[number] = [];
		let low = Infinity;
		let high = -Infinity;
		for (const point of track) {
			const x = distance ? point.distM : point.t;
			if (!finite(x) || !finite(point.eleM)) {
				run = [];
				continue;
			}
			if (point.connected === false) run = [];
			if (!run.length) runs.push(run);
			run.push({ x, y: point.eleM });
			low = Math.min(low, point.eleM);
			high = Math.max(high, point.eleM);
		}
		if (!runs.length) return null;
		const values = distance ? distances : times;
		return { runs, distance, start: values[0]!, end: values.at(-1)!, low, high };
	});

	function finite(value: number | null | undefined): value is number {
		return value != null && Number.isFinite(value);
	}

	const scale = $derived(data && elevationScale(data.low, data.high, height >= 90 ? 3 : 2));
	const yLabels = $derived(scale?.ticks.map((tick) => `${number.format(tick)} m`) ?? []);
	const left = $derived(Math.max(44, ...yLabels.map((label) => label.length * 6 + 8)));
	const y = (value: number) => bottom - ((value - scale!.min) / (scale!.max - scale!.min)) * height;
	const x = (value: number) =>
		left + ((value - data!.start) / (data!.end - data!.start)) * (right - left);
	const xTicks = $derived.by(() => {
		if (!data) return [];
		const span = data.end - data.start;
		const unit = data.distance && span >= 1000 ? 1000 : 1;
		const rawStep = span / Math.max(2, Math.floor((right - left) / 75));
		const step = tickStep(Math.max(data.distance ? 0.1 : 1, rawStep));
		const first = Math.ceil(data.start / step) * step;
		return axisTicks(first, data.end, step).map((value) => ({
			position: x(value),
			label: data.distance
				? `${number.format(value / unit)} ${unit === 1000 ? 'km' : 'm'}`
				: formatDuration(value)
		}));
	});
	const highlightBounds = $derived(
		highlight && data?.distance
			? {
					start: Math.max(left, x(highlight.startDist)),
					end: Math.min(right, x(highlight.endDist))
				}
			: null
	);
</script>

<div class="w-full min-w-0" bind:clientWidth={width}>
	{#if data && scale && right > left}
		<svg
			viewBox={`0 0 ${width} ${bottom + 23}`}
			class="block w-full"
			role="img"
			aria-label={m.waas_elevation_profile()}
		>
			<defs>
				<linearGradient {id} x2="0" y2="1">
					<stop offset="0%" stop-color="var(--plot-elevation)" stop-opacity="0.35" />
					<stop offset="100%" stop-color="var(--plot-elevation)" stop-opacity="0.04" />
				</linearGradient>
			</defs>
			<ChartAxes
				{left}
				{right}
				{top}
				{bottom}
				{width}
				{xTicks}
				yTicks={scale.ticks.map((tick) => ({
					position: y(tick),
					label: `${number.format(tick)} m`
				}))}
			/>
			{#if highlightBounds && highlightBounds.end > highlightBounds.start}
				<rect
					x={highlightBounds.start}
					y={top}
					width={highlightBounds.end - highlightBounds.start}
					{height}
					fill="var(--plot-power)"
					fill-opacity="0.22"
				/>
			{/if}
			{#each data.runs as run (run)}
				{@const line = run
					.map((point, i) => `${i ? 'L' : 'M'}${x(point.x).toFixed(1)},${y(point.y).toFixed(1)}`)
					.join(' ')}
				{#if run.length === 1}
					<circle cx={x(run[0]!.x)} cy={y(run[0]!.y)} r="2" fill="var(--plot-elevation)" />
				{:else}
					<path d={`${line} V${bottom} H${x(run[0]!.x)} Z`} fill={`url(#${id})`} />
					<path d={line} fill="none" stroke="var(--plot-elevation)" stroke-width="1.5" />
				{/if}
			{/each}
		</svg>
	{/if}
</div>
