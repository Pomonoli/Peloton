<script lang="ts">
	import { intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		DAY,
		FORM_ZONES,
		METRICS,
		calendarDate,
		metricNumber,
		yScale,
		type Analysis,
		type StressPeriod
	} from './analysis.js';

	let {
		analysis,
		periods,
		hidden = $bindable([])
	}: { analysis: Analysis; periods?: StressPeriod[]; hidden?: string[] } = $props();
	let width = $state(600);
	let active = $state<number | null>(null);
	const id = $props.id();

	const isStress = $derived(periods !== undefined);
	const visible = $derived(METRICS.filter(({ key }) => !hidden.includes(key)));
	const dates = $derived(analysis.dates.map((day) => Date.parse(day)));
	const start = $derived(Date.parse(analysis.start));
	const end = $derived(Date.parse(analysis.end));
	const count = $derived(periods?.length ?? (end - start) / DAY + 1);
	const size = $derived(periods?.length ?? dates.length);
	const offset = $derived(isStress ? 0 : ((dates[0] ?? start) - start) / DAY);
	const current = $derived(Math.min(active ?? size - 1, size - 1));

	const left = $derived(isStress ? 47 : 37);
	const right = $derived(width - 12);
	const height = $derived(isStress ? 208 : width >= 520 ? 216 : 192);
	const top = 12;
	const bottom = $derived(height - 27);
	const plotWidth = $derived(Math.max(1, right - left));
	const scale = $derived.by(() => {
		let min = 0;
		let max = 0;
		if (periods) {
			for (const period of periods) max = Math.max(max, period.tss ?? 0);
		} else {
			for (const { key } of visible) {
				for (const value of analysis[key]) {
					min = Math.min(min, value);
					max = Math.max(max, value);
				}
			}
		}
		return yScale(min, max || (min === 0 ? 4 : 0));
	});

	function x(index: number) {
		if (count <= 1) return left + plotWidth / 2;
		const position = isStress ? (index + 0.5) / count : index / (count - 1);
		return left + position * plotWidth;
	}

	function y(value: number) {
		return bottom - ((value - scale.min) / (scale.max - scale.min)) * (bottom - top);
	}

	const paths = $derived(
		isStress
			? []
			: visible.map(({ key, color }) => ({
					key,
					color,
					path: analysis[key]
						.map(
							(value, i) =>
								`${i === 0 ? 'M' : 'L'}${x(i + offset).toFixed(2)},${y(value).toFixed(2)}`
						)
						.join(' ')
				}))
	);

	const ticks = $derived.by(() => {
		const limit = Math.min(9, Math.max(2, Math.floor(plotWidth / 78) + 1), count);
		const span = end - start;
		const options: Intl.DateTimeFormatOptions =
			span <= 75 * DAY
				? { month: 'short', day: 'numeric' }
				: { month: 'short', year: span > 400 * DAY ? '2-digit' : 'numeric' };
		const formatter = new Intl.DateTimeFormat(intlLocale(), { ...options, timeZone: 'UTC' });
		const result: { label: string; x: number }[] = [];
		for (let i = 0; i < limit; i++) {
			const index = Math.round((i * (count - 1)) / Math.max(1, limit - 1));
			const label = formatter.format(periods?.[index]?.date ?? start + index * DAY);
			const position = Math.max(left + 9, Math.min(right - 24, x(index)));
			const previous = result.at(-1);
			if (previous && (previous.label === label || position - previous.x < 60)) continue;
			result.push({ label, x: position });
		}
		return result;
	});

	const barWidth = $derived(Math.min(34, (plotWidth / count) * 0.72));

	function toggle(key: string) {
		hidden = hidden.includes(key) ? hidden.filter((item) => item !== key) : [...hidden, key];
	}

	function point(event: PointerEvent) {
		const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
		const position = (event.clientX - rect.left - left) / plotWidth;
		const index = isStress ? Math.floor(position * count) : Math.round(position * (count - 1));
		active = Math.max(0, Math.min(size - 1, index - offset));
	}

	function move(event: KeyboardEvent) {
		const targets: Record<string, number> = {
			ArrowLeft: current - 1,
			ArrowDown: current - 1,
			ArrowRight: current + 1,
			ArrowUp: current + 1,
			Home: 0,
			End: size - 1
		};
		const target = targets[event.key];
		if (target === undefined && event.key !== 'Escape') return;
		event.preventDefault();
		active = target === undefined ? null : Math.max(0, Math.min(size - 1, target));
	}

	const title = $derived.by(() => {
		if (current < 0) return '';
		const period = periods?.[current];
		const date = dates[current];
		if (!period) return date === undefined ? '' : calendarDate(date);
		return period.from === period.to
			? calendarDate(period.from)
			: m.analysis_date_range({ start: calendarDate(period.from), end: calendarDate(period.to) });
	});

	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 1 }));
	const readout = $derived.by(() => {
		if (current < 0) return [];
		const period = periods?.[current];
		if (!period)
			return METRICS.flatMap(({ key, label, color }) => {
				const value = analysis[key][current];
				return value === undefined
					? []
					: [{ label: label(), value: metricNumber(value, key === 'tsb'), color }];
			});
		return [
			{
				label: m.analysis_tss_total(),
				value:
					period.tss === null && period.total > 0
						? m.analysis_tss_unavailable()
						: number.format(period.tss ?? 0),
				color: ''
			},
			{ label: m.analysis_tss_count(), value: number.format(period.total), color: '' },
			{ label: m.analysis_tss_scored(), value: number.format(period.scored), color: '' },
			{
				label: m.analysis_stress_missing(),
				value: number.format(period.total - period.scored),
				color: ''
			}
		];
	});
	const announcement = $derived(
		m.analysis_chart_point({
			date: title,
			values: readout.map(({ label, value }) => `${label} ${value}`).join(', ')
		})
	);
</script>

{#if !isStress}
	<div class="legend" role="group" aria-label={m.analysis_series()}>
		{#each METRICS as { key, color, label } (key)}
			<button type="button" aria-pressed={!hidden.includes(key)} onclick={() => toggle(key)}>
				<span class="legend-dot" style:--series-color={color} aria-hidden="true"></span>
				{label()}
			</button>
		{/each}
	</div>
{/if}

{#if size === 0 || (!isStress && visible.length === 0)}
	<p class="empty">{size === 0 ? m.analysis_trends_empty() : m.analysis_chart_no_series()}</p>
{:else}
	<span {id} class="sr-only">{m.analysis_chart_keyboard()}</span>
	<div
		class="chart"
		bind:clientWidth={width}
		role="slider"
		tabindex="0"
		aria-label={isStress ? m.analysis_tss() : m.analysis_trends()}
		aria-describedby={id}
		aria-valuemin={1}
		aria-valuemax={size}
		aria-valuenow={current + 1}
		aria-valuetext={announcement}
		onkeydown={move}
		onblur={() => (active = null)}
		onpointerdown={point}
		onpointermove={point}
		onpointerleave={(event) => {
			if (event.pointerType !== 'touch') active = null;
		}}
	>
		<svg viewBox={`0 0 ${width} ${height}`} aria-hidden="true">
			{#if !isStress && visible.length === 1 && visible[0]?.key === 'tsb'}
				{#each FORM_ZONES as zone, i (zone.from)}
					{@const min = Math.max(scale.min, zone.from)}
					{@const max = Math.min(scale.max, FORM_ZONES[i + 1]?.from ?? Infinity)}
					{#if max > min}
						<rect
							x={left}
							y={y(max)}
							width={plotWidth}
							height={y(min) - y(max)}
							fill={zone.color}
							opacity="0.13"
						/>
					{/if}
				{/each}
			{/if}

			{#each scale.ticks as tick (tick)}
				<line
					x1={left}
					x2={right}
					y1={y(tick)}
					y2={y(tick)}
					stroke={tick === 0 ? 'var(--plot-cursor)' : 'var(--plot-grid)'}
					stroke-dasharray={tick === 0 && !isStress ? '4 3' : undefined}
				/>
				<text x={left - 7} y={y(tick) + 4} text-anchor="end">{number.format(tick)}</text>
			{/each}

			{#if isStress}
				<text
					x={12}
					y={(top + bottom) / 2}
					text-anchor="middle"
					writing-mode="vertical-rl"
					transform={`rotate(180 12 ${(top + bottom) / 2})`}>{m.analysis_tss_axis()}</text
				>
			{/if}

			{#each ticks as tick (tick.label)}
				<text class="date-tick" x={tick.x} y={height - 7} text-anchor="middle">{tick.label}</text>
			{/each}

			{#if periods}
				{#each periods as period, i (period.date)}
					{@const px = x(i)}
					{#if period.tss !== null}
						{@const barHeight = Math.max(1.5, y(0) - y(period.tss))}
						<rect
							x={px - barWidth / 2}
							y={y(0) - barHeight}
							width={barWidth}
							height={barHeight}
							rx={Math.min(2, barWidth / 3)}
							fill="var(--accent-primary)"
							opacity={active === i ? 1 : 0.85}
						/>
					{/if}
					{#if period.total > period.scored}
						<line
							x1={px - barWidth / 2}
							x2={px + barWidth / 2}
							y1={y(period.tss ?? 0) - 3}
							y2={y(period.tss ?? 0) - 3}
							stroke="var(--warning-strong)"
							stroke-width="2"
							stroke-dasharray="2 1"
						/>
					{/if}
				{/each}
			{:else}
				{#each paths as { key, color, path } (key)}
					{@const first = analysis[key][0]}
					{#if size === 1 && first !== undefined}
						<circle cx={x(offset)} cy={y(first)} r="3" fill={color} />
					{:else}
						<path d={path} fill="none" stroke={color} stroke-width="2" stroke-linejoin="round" />
					{/if}
				{/each}
			{/if}

			{#if active !== null}
				{@const cursor = x(current + offset)}
				<line
					x1={cursor}
					x2={cursor}
					y1={top}
					y2={bottom}
					stroke="var(--plot-cursor)"
					opacity="0.65"
				/>
				{#if !isStress}
					{#each visible as { key, color } (key)}
						{@const value = analysis[key][current]}
						{#if value !== undefined}
							<circle cx={cursor} cy={y(value)} r="3.5" fill={color} stroke="var(--surface-card)" />
						{/if}
					{/each}
				{/if}
			{/if}
		</svg>

		{#if active !== null}
			<div class="readout" class:align-right={x(current + offset) < width / 2} aria-hidden="true">
				<p>{title}</p>
				<dl>
					{#each readout as { label, value, color } (label)}
						<div>
							<dt>
								{#if color}<span class="legend-dot" style:--series-color={color}></span>{/if}
								{label}
							</dt>
							<dd>{value}</dd>
						</div>
					{/each}
				</dl>
			</div>
		{/if}
	</div>

	{#if periods?.some((period) => period.total > period.scored)}
		<p class="missing-key"><span aria-hidden="true"></span>{m.analysis_tss_missing_key()}</p>
	{/if}
{/if}

<style>
	.chart {
		position: relative;
		min-width: 0;
		border-radius: 0.35rem;
		touch-action: pan-y;
	}
	svg {
		display: block;
		width: 100%;
		overflow: visible;
	}
	text {
		fill: var(--plot-axis);
		font-size: 11px;
		font-variant-numeric: tabular-nums;
	}
	.legend {
		display: flex;
		flex-wrap: wrap;
		gap: 0.1rem 0.3rem;
		margin: 0.45rem 0 0.2rem;
	}
	.legend button {
		display: inline-flex;
		align-items: center;
		gap: 0.35rem;
		min-height: 2.2rem;
		border: 1px solid transparent;
		border-radius: 0.45rem;
		padding: 0.15rem 0.4rem;
		color: var(--text-muted);
		font-size: 0.75rem;
		font-weight: 550;
	}
	.legend button:hover,
	.legend button[aria-pressed='true'] {
		background: color-mix(in srgb, var(--surface-muted) 32%, transparent);
		border-color: var(--plot-grid);
	}
	.legend button[aria-pressed='false'] .legend-dot {
		background: transparent;
	}
	.legend-dot {
		display: inline-block;
		flex: 0 0 auto;
		width: 0.6rem;
		height: 0.6rem;
		border: 1.5px solid var(--series-color);
		border-radius: 50%;
		background: var(--series-color);
	}
	.readout {
		position: absolute;
		z-index: 1;
		top: 0.45rem;
		left: min(3.15rem, 15%);
		width: max-content;
		max-width: calc(100% - 3.5rem);
		pointer-events: none;
		border: 1px solid var(--line-strong);
		border-radius: 0.55rem;
		background: var(--surface-panel);
		box-shadow: 0 3px 15px rgb(0 0 0 / 13%);
		padding: 0.5rem 0.65rem;
		font-size: 0.75rem;
	}
	.readout.align-right {
		right: 0.6rem;
		left: auto;
	}
	.readout p {
		margin-bottom: 0.3rem;
		font-weight: 650;
	}
	.readout dl > div,
	.readout dt {
		display: flex;
		align-items: center;
		gap: 0.45rem;
	}
	.readout dl > div {
		justify-content: space-between;
		gap: 0.85rem;
	}
	.readout dt {
		color: var(--text-muted);
	}
	.readout dd {
		font-weight: 650;
		font-variant-numeric: tabular-nums;
	}
	.missing-key {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		margin-top: 0.25rem;
		color: var(--text-quiet);
		font-size: 0.75rem;
	}
	.missing-key span {
		width: 1rem;
		border-top: 2px dashed var(--warning-strong);
	}
	.empty {
		display: grid;
		min-height: 10rem;
		place-items: center;
		color: var(--text-quiet);
		font-size: 0.875rem;
		text-align: center;
	}
	@media (max-width: 420px) {
		.legend {
			column-gap: 0.2rem;
		}
		.legend button {
			padding-inline: 0.3rem;
			font-size: 0.72rem;
		}
	}
</style>
