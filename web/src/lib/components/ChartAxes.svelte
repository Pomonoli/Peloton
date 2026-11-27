<script lang="ts">
	type Tick = { position: number; label: string };

	let {
		left,
		right,
		top,
		bottom,
		width,
		xTicks,
		yTicks
	}: {
		left: number;
		right: number;
		top: number;
		bottom: number;
		width: number;
		xTicks: Tick[];
		yTicks: Tick[];
	} = $props();
</script>

<g aria-hidden="true">
	{#each yTicks as tick (tick.position)}
		<line x1={left} x2={right} y1={tick.position} y2={tick.position} />
		<text x={left - 6} y={tick.position} text-anchor="end" dominant-baseline="middle">
			{tick.label}
		</text>
	{/each}
	<path d={`M${left} ${top} V${bottom} H${right}`} />
	{#each xTicks as tick (tick.position)}
		<line x1={tick.position} x2={tick.position} y1={bottom} y2={bottom + 4} />
		<text
			x={Math.min(tick.position, width - 2)}
			y={bottom + 17}
			text-anchor={tick.position > width - 35 ? 'end' : 'middle'}
		>
			{tick.label}
		</text>
	{/each}
</g>

<style>
	text {
		fill: var(--plot-axis);
		font: var(--plot-font);
	}
	line {
		stroke: var(--plot-grid);
	}
	path {
		fill: none;
		stroke: var(--plot-cursor);
	}
</style>
