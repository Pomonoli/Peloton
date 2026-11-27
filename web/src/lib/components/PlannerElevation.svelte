<script lang="ts">
	import { intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		routeDistance,
		routeElevation,
		type RecordedTrack,
		type RoutePoint
	} from '#lib/recorded-routes.js';
	import { elevationProfile, profilePoint, axisTicks, tickStep } from '#lib/waas/profile.js';
	import { finiteElevation, trackDistance } from '#lib/waas/recorded.js';
	import type { ElevationStats } from '#lib/waas/types.js';

	let {
		track,
		elevation,
		onpoint
	}: {
		track: RecordedTrack;
		elevation: ElevationStats | null;
		onpoint: (point: RoutePoint | null) => void;
	} = $props();

	const left = 54;
	const right = 18;
	const top = 13;
	const bottom = 130;
	let width = $state(0);
	let position = $state<number | null>(null);
	let pinned = $state(false);

	const length = $derived(trackDistance(track));
	const runs = $derived(elevationProfile(track));
	const partial = $derived(
		runs.reduce((sum, run) => sum + run.length, 0) <
			track.reduce((sum, line) => sum + line.length, 0)
	);
	const plotWidth = $derived(Math.max(1, width - left - right));
	const kilometerStep = $derived(
		tickStep(Math.max(0.001, length / 1000) / Math.max(2, Math.floor(plotWidth / 76)))
	);
	const xTicks = $derived(axisTicks(0, length / 1000, kilometerStep));
	const axis = $derived.by(() => {
		if (elevation?.lowest == null || elevation.highest == null) return null;
		const span = Math.max(60, (elevation.highest - elevation.lowest) * 1.14);
		const middle = (elevation.highest + elevation.lowest) / 2;
		const step = tickStep(span / 4);
		const minimum =
			Math.floor(
				(elevation.lowest >= 0 ? Math.max(0, middle - span / 2) : middle - span / 2) / step
			) * step;
		const maximum = Math.ceil(Math.max(minimum + span, middle + span / 2) / step) * step;
		return { minimum, maximum, ticks: axisTicks(minimum, maximum, step) };
	});
	const cursor = $derived(position == null ? null : profilePoint(track, runs, position));
	const cursorX = $derived(cursor ? x(cursor.point.distance_m) : 0);
	const cursorHeight = $derived(cursor ? finiteElevation(cursor.point) : null);
	const fmt = (value: number, digits = 0) =>
		new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: digits }).format(value);
	const gradientLabel = $derived(
		cursor?.gradient == null
			? '—'
			: new Intl.NumberFormat(intlLocale(), {
					maximumFractionDigits: 1,
					signDisplay: 'exceptZero',
					style: 'percent'
				}).format(cursor.gradient / 100)
	);

	function x(meters: number) {
		return left + (plotWidth * meters) / length;
	}

	function y(height: number) {
		return bottom - ((bottom - top) * (height - axis!.minimum)) / (axis!.maximum - axis!.minimum);
	}

	// Retain both extremes per pixel so short climbs remain visible without
	// rendering every recorded sample as a separate SVG vertex.
	function path(run: RoutePoint[]) {
		const points: string[] = [];
		let low = 0;
		let high = 0;
		let column = -1;
		function emit() {
			for (const i of low === high ? [low] : [Math.min(low, high), Math.max(low, high)]) {
				const p = run[i]!;
				points.push(`${x(p.distance_m).toFixed(1)},${y(p.elevation_m!).toFixed(1)}`);
			}
		}
		for (const [i, point] of run.entries()) {
			const px = Math.floor(x(point.distance_m));
			if (column !== px) {
				if (column >= 0) emit();
				column = px;
				low = high = i;
			}
			if (point.elevation_m! < run[low]!.elevation_m!) low = i;
			if (point.elevation_m! > run[high]!.elevation_m!) high = i;
		}
		emit();
		const line = 'M' + points.join('L');
		return { line, area: line + `V${bottom}H${x(run[0]!.distance_m)}Z` };
	}

	const dots = $derived(runs.filter((run) => run.length === 1));
	const paths = $derived(
		axis && length > 0 && width > 0 ? runs.filter((run) => run.length > 1).map(path) : []
	);

	$effect(() => {
		void track;
		position = null;
		pinned = false;
	});
	$effect(() => onpoint(cursor?.point ?? null));

	function hover(event: PointerEvent) {
		if (event.pointerType !== 'mouse' || event.buttons) return;
		const rect = (event.currentTarget as HTMLInputElement).getBoundingClientRect();
		position = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)) * length;
	}
</script>

<section
	class="surface min-w-0 px-3 pt-3 pb-2 sm:px-4 sm:pt-4"
	aria-label={m.waas_elevation_profile()}
>
	<div class="flex min-h-6 flex-wrap items-center justify-between gap-x-3 gap-y-1">
		<h2 class="section-title">{m.waas_elevation_profile()}</h2>
		{#if partial && axis}<span class="text-xs text-zinc-400">{m.planner_partial_elevation()}</span
			>{/if}
	</div>
	{#if axis && length > 0}
		<div class="relative mt-2" bind:clientWidth={width}>
			{#if width > left + right}
				<svg viewBox="0 0 {width} 154" class="block w-full overflow-visible" aria-hidden="true">
					{#each axis.ticks as tick (tick)}
						<line x1={left} x2={width - right} y1={y(tick)} y2={y(tick)} class="gridline" />
						<text x={left - 7} y={y(tick)} text-anchor="end" dominant-baseline="middle"
							>{fmt(tick)} m</text
						>
					{/each}
					{#each xTicks as tick (tick)}
						<line x1={x(tick * 1000)} x2={x(tick * 1000)} y1={top} y2={bottom} class="gridline" />
						<text x={Math.min(width - 20, x(tick * 1000))} y="148" text-anchor="middle"
							>{fmt(tick, 3)} km</text
						>
					{/each}
					{#each paths as { line, area }, i (i)}
						<path d={area} class="area" />
						<path d={line} class="profile-line" />
					{/each}
					{#each dots as run (run)}
						<circle
							cx={x(run[0]!.distance_m)}
							cy={y(run[0]!.elevation_m!)}
							r="2.5"
							class="profile-dot"
						/>
					{/each}
					{#if cursor}
						<line x1={cursorX} x2={cursorX} y1={top} y2={bottom} class="cursor-line" />
						{#if cursorHeight != null}
							<circle cx={cursorX} cy={y(cursorHeight)} r="4.5" class="cursor-dot" />
						{/if}
					{/if}
				</svg>
				<input
					class="profile-input"
					style:left={left + 'px'}
					style:width={plotWidth + 'px'}
					type="range"
					min="0"
					max={length}
					step="any"
					value={position ?? 0}
					aria-label={m.planner_profile_cursor()}
					aria-valuetext={cursor
						? [
								routeDistance(cursor.point.distance_m),
								routeElevation(cursorHeight),
								gradientLabel
							].join(', ')
						: undefined}
					onpointermove={hover}
					onpointerleave={() => {
						if (!pinned) position = null;
					}}
					onpointerdown={() => {
						pinned = true;
					}}
					oninput={(event) => {
						position = event.currentTarget.valueAsNumber;
					}}
					onfocus={() => {
						position ??= 0;
					}}
					onblur={() => {
						if (!pinned) position = null;
					}}
					onkeydown={(event) => {
						if (event.key === 'Escape') {
							pinned = false;
							position = null;
						}
					}}
				/>
				{#if cursor}
					<div
						class="profile-tooltip"
						data-profile-tooltip
						style:--cursor-x={`${cursorX}px`}
						aria-hidden="true"
					>
						<strong class="metric-value">{routeDistance(cursor.point.distance_m)}</strong>
						<span class="metric-value">{routeElevation(cursorHeight)}</span>
						<span title={m.planner_gradient()}>{gradientLabel}</span>
					</div>
				{/if}
			{/if}
		</div>
	{:else}
		<p class="py-5 text-sm text-zinc-400">{m.planner_no_elevation()}</p>
	{/if}
</section>

<style>
	text {
		fill: var(--plot-axis);
		font: var(--plot-font);
	}
	.gridline {
		stroke: var(--plot-grid);
		stroke-width: 1;
	}
	.area {
		fill: var(--plot-elevation);
		fill-opacity: 0.13;
	}
	.profile-line {
		fill: none;
		stroke: var(--plot-elevation);
		stroke-width: 1.75;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	.profile-dot {
		fill: var(--plot-elevation);
	}
	.cursor-line {
		stroke: var(--plot-cursor);
		stroke-dasharray: 3 3;
	}
	.cursor-dot {
		stroke: var(--surface-card);
		stroke-width: 2;
		fill: var(--plot-elevation);
	}
	.profile-input {
		position: absolute;
		top: 8px;
		height: 125px;
		appearance: none;
		background: transparent;
		cursor: crosshair;
		touch-action: pan-y;
	}
	.profile-input::-webkit-slider-thumb {
		appearance: none;
		width: 1px;
		height: 24px;
		background: transparent;
	}
	.profile-input::-moz-range-thumb {
		width: 1px;
		height: 24px;
		border: 0;
		background: transparent;
	}
	.profile-tooltip {
		--tooltip-width: min(100%, 10rem);
		position: absolute;
		top: 1px;
		left: clamp(0px, var(--cursor-x) - 5rem, 100% - var(--tooltip-width));
		z-index: 1;
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: 0.15rem 0.4rem;
		width: var(--tooltip-width);
		padding: 0.33rem 0.48rem;
		border: 1px solid var(--line-subtle);
		border-radius: 0.45rem;
		background: var(--surface-card);
		box-shadow: var(--panel-shadow);
		color: var(--text-primary);
		font-size: 0.73rem;
		font-variant-numeric: tabular-nums;
		pointer-events: none;
	}
</style>
