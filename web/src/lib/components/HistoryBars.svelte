<script lang="ts">
	import { axisTicks, tickStep } from '#lib/chart-scale.js';
	import { intlLocale } from '#lib/format.js';
	import ChartAxes from './ChartAxes.svelte';

	let {
		label,
		points,
		formatTick
	}: {
		label: string;
		points: { id: string; date: string; value: number | null; title: string; color: string }[];
		formatTick: (value: number) => string;
	} = $props();

	let width = $state(0);
	const height = 182;
	const top = 9;
	const bottom = height - 24;
	const right = $derived(width - 10);
	const numberOfBars = $derived(points.length);
	const maximum = $derived(points.reduce((max, point) => Math.max(max, point.value ?? 0), 0));
	const step = $derived(tickStep(maximum > 0 ? maximum / 4 : 1));
	const ceiling = $derived(Math.ceil(maximum / step) * step || step);
	const yTicks = $derived(
		axisTicks(0, ceiling, step).map((value) => ({
			position: y(value),
			label: formatTick(value)
		}))
	);
	const left = $derived(9 + Math.max(...yTicks.map((tick) => tick.label.length)) * 6);
	const barWidth = $derived((right - left) / numberOfBars);
	const dateFormat = $derived(
		new Intl.DateTimeFormat(intlLocale(), {
			month: 'short',
			day: 'numeric',
			year: points[0]?.date.slice(0, 4) !== points.at(-1)?.date.slice(0, 4) ? '2-digit' : undefined
		})
	);

	function y(value: number) {
		return bottom - (value / ceiling) * (bottom - top);
	}

	const xTicks = $derived.by(() => {
		const limit = Math.min(numberOfBars, Math.max(2, Math.floor((right - left) / 82)));
		const ticks: { position: number; label: string }[] = [];
		for (let i = 0; i < limit; i++) {
			const index = Math.round((i * (numberOfBars - 1)) / Math.max(1, limit - 1));
			const date = new Date(points[index]!.date);
			if (!Number.isFinite(date.getTime())) continue;
			const label = dateFormat.format(date);
			if (ticks.at(-1)?.label === label) continue;
			ticks.push({ position: left + barWidth * (index + 0.5), label });
		}
		return ticks;
	});
</script>

<div class="w-full min-w-0" bind:clientWidth={width}>
	{#if right > left && numberOfBars}
		<svg viewBox={`0 0 ${width} ${height}`} class="block w-full" role="img" aria-label={label}>
			<ChartAxes {left} {right} {top} {bottom} {width} {xTicks} {yTicks} />
			{#each points as point, index (point.id)}
				{#if point.value !== null}
					<rect
						x={left + index * barWidth + barWidth * 0.1}
						y={y(point.value)}
						width={barWidth * 0.8}
						height={bottom - y(point.value)}
						rx="1.5"
						fill={point.color}
					>
						<title>{point.title}</title>
					</rect>
				{/if}
			{/each}
		</svg>
	{/if}
</div>
