<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import uPlot, { type AlignedData, type Options } from 'uplot';
	import 'uplot/dist/uPlot.min.css';
	import {
		formatClockTime,
		formatDate,
		formatDateOnly,
		formatDuration,
		intlLocale
	} from '#lib/format.js';
	import { appearance, appearanceColor } from '#lib/appearance.js';
	import { m } from '#lib/paraglide/messages.js';

	// Use clock intervals for elapsed time so the ticks land on whole minutes and hours.
	const elapsedIncrements = [
		0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 15, 30, 60, 120, 180, 300,
		600, 900, 1800, 3600, 7200, 14400, 28800, 43200, 86400, 172800, 604800, 2592000
	];

	interface Series {
		label: string;
		values: (number | null)[];
		color: string;
		unit?: string;
		formatValue?: (value: number | null) => string;
		fill?: boolean; // render as a filled area (e.g. elevation as a mountain profile)
	}

	let {
		offsets,
		series,
		height = 300,
		syncKey,
		hoverT = null,
		calendar = false,
		yAxis,
		legend = true,
		onHover
	}: {
		offsets: number[];
		series: Series[];
		height?: number;
		calendar?: boolean;
		yAxis?: {
			label: string;
			unit: string;
			range?: (min: number, max: number) => [number, number];
		};
		legend?: boolean;
		syncKey?: string;
		// hoverT: externally-driven hover position (seconds offset) — shows the
		// cursor at that x without emitting. null clears it.
		hoverT?: number | null;
		// onHover: fires with the hovered time (seconds offset) as the user moves
		// over the chart, or null on leave. Lets a parent sync a map marker.
		onHover?: (t: number | null) => void;
	} = $props();

	let container: HTMLDivElement;
	let plot: uPlot | undefined;
	let zoomed = $state(false);
	// Guards the setCursor hook from re-emitting when WE move the cursor
	// programmatically from an external hoverT.
	let applyingExternal = false;

	// Full x-extent of the data; used to detect zoom state and to reset.
	function fullRange(): [number, number] {
		const start = offsets[0] ?? 0;
		return [start, Math.max(start + 1, offsets.at(-1) ?? start)];
	}

	function resetZoom() {
		if (!plot) return;
		const [min, max] = fullRange();
		const charts = syncKey ? uPlot.sync(syncKey).plots : [plot];
		for (const chart of charts) chart.setScale('x', { min, max });
	}

	function build() {
		if (!container) return;
		if (offsets.length === 0 || series.length === 0) {
			plot?.destroy();
			plot = undefined;
			return;
		}

		// Filled series (e.g. elevation) render FIRST so they sit behind the
		// metric lines like a background mountain profile.
		const ordered = [...series].sort((a, b) => Number(b.fill ?? false) - Number(a.fill ?? false));

		const data: AlignedData = [offsets, ...ordered.map((s) => s.values)] as AlignedData;

		const number = new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 3 });
		const font = appearanceColor('--plot-font');
		const axisColor = appearanceColor('--plot-axis');
		const gridColor = appearanceColor('--plot-grid');
		const seriesColor = (color: string) =>
			color.startsWith('var(') ? appearanceColor(color.slice(4, -1)) : color;

		// Share a scale only between series reporting the same unit.
		const scaleKey = (s: Series) => `y:${s.unit ?? yAxis?.unit ?? s.label}`;
		const metrics = new Map(ordered.map((s) => [scaleKey(s), s]));
		const scales: Options['scales'] = {
			x: {
				time: calendar,
				range: (_u, min, max) => (min === max ? fullRange() : [min, max])
			}
		};
		const measure = document.createElement('canvas').getContext('2d')!;
		measure.font = font;
		const axes: uPlot.Axis[] = [
			{
				stroke: axisColor,
				font,
				size: 24,
				space: calendar ? 88 : 57,
				...(!calendar ? { incrs: elapsedIncrements } : {}),
				ticks: { size: 4, stroke: axisColor },
				border: { stroke: axisColor },
				grid: { stroke: gridColor },
				values: (_u, splits, _idx, increment) =>
					splits.map((value) =>
						calendar
							? increment < 86400
								? formatClockTime(value * 1000, { seconds: increment < 60 })
								: formatDateOnly(new Date(value * 1000).toISOString())
							: increment < 1
								? `${number.format(value)} s`
								: formatDuration(value)
					)
			}
		];

		for (const [key, metric] of metrics) {
			const range = yAxis?.range;
			scales[key] = range
				? {
						range: (_u, min, max) => (min == null || max == null ? [null, null] : range(min, max))
					}
				: {};
			const index = axes.length - 1;
			const stroke = metrics.size > 1 ? seriesColor(metric.color) : axisColor;
			const unit = metric.unit ?? yAxis?.unit;
			axes.push({
				scale: key,
				side: index % 2 === 0 ? 3 : 1,
				stroke,
				font,
				space: 34,
				gap: 4,
				size: (_u, values) =>
					Math.max(
						metrics.size === 1 ? 58 : 36,
						...(values ?? []).map((value) => measure.measureText(value).width + 11)
					),
				ticks: { size: 4, stroke },
				border: { stroke },
				grid: { show: index === 0, stroke: gridColor },
				values: (_u, splits) =>
					splits.map((value) => `${number.format(value)}${unit ? ' ' + unit : ''}`)
			});
		}

		const opts: Options = {
			width: container.clientWidth || 800,
			height,
			legend: { show: legend },
			cursor: {
				drag: { x: true, y: false },
				// When a syncKey is given, join a uPlot sync group so the hover
				// cursor (and drag-zoom selection) align across stacked charts.
				...(syncKey ? { sync: { key: syncKey, setSeries: true } } : {})
			},
			scales,
			padding: [8, metrics.size === 1 ? (calendar ? 37 : 22) : 4, 0, 3],
			axes,
			series: [
				{
					label: m.detail_label_time(),
					value: (_plot, value) =>
						value == null
							? '—'
							: calendar
								? formatDate(new Date(value * 1000).toISOString())
								: formatDuration(value)
				},
				...ordered.map((s) => {
					const stroke = seriesColor(s.color);
					const { formatValue } = s;
					return {
						label: s.unit ? `${s.label} (${s.unit})` : s.label,
						value: (_plot: uPlot, value: number | null) =>
							value == null
								? '—'
								: formatValue
									? formatValue(value)
									: `${number.format(value)}${s.unit ? ' ' + s.unit : ''}`,
						stroke,
						width: s.fill ? 1 : 1.8,
						...(s.fill ? { fill: stroke + '2e' } : {}),
						scale: scaleKey(s),
						spanGaps: calendar,
						points: { show: calendar || s.values.filter((value) => value != null).length === 1 }
					};
				})
			],
			hooks: {
				// Toggle the reset button whenever the x-scale changes (drag-zoom
				// narrows it; double-click / reset restores the full extent).
				setScale: [
					(u, key) => {
						const x = u.scales.x;
						if (key !== 'x' || !x) return;
						const { min, max } = x;
						const [full0, full1] = fullRange();
						const span = full1 - full0 || 1;
						const eps = span * 1e-6;
						zoomed = min != null && max != null && (min > full0 + eps || max < full1 - eps);
					}
				],
				// Emit the hovered time so a parent can sync a map marker. Skipped
				// when the cursor was moved programmatically (external hoverT).
				setCursor: [
					(u) => {
						if (applyingExternal || !onHover) return;
						const idx = u.cursor.idx;
						onHover(idx == null ? null : (offsets[idx] ?? null));
					}
				]
			}
		};

		plot?.destroy();
		plot = new uPlot(opts, data, container);
	}

	function inspectTouch(event: PointerEvent) {
		if (!plot || !onHover || event.pointerType === 'mouse') return;
		const rect = plot.over.getBoundingClientRect();
		if (
			event.clientX < rect.left ||
			event.clientX > rect.right ||
			event.clientY < rect.top ||
			event.clientY > rect.bottom
		)
			return;
		plot.setCursor({ left: event.clientX - rect.left, top: event.clientY - rect.top });
	}

	onMount(() => {
		let resizeFrame = 0;
		const observer = new ResizeObserver(() => {
			cancelAnimationFrame(resizeFrame);
			resizeFrame = requestAnimationFrame(() => {
				const width = container.clientWidth;
				if (width && plot && (plot.width !== width || plot.height !== height)) {
					plot.setSize({ width, height });
				}
			});
		});
		observer.observe(container);
		return () => {
			observer.disconnect();
			cancelAnimationFrame(resizeFrame);
		};
	});

	onDestroy(() => {
		plot?.destroy();
	});

	$effect(() => {
		// Rebuild on data or appearance changes because canvas colors are resolved at draw time.
		void $appearance.mode;
		void offsets;
		void series;
		build();
	});

	$effect(() => {
		// Apply an externally-driven hover position (e.g. from the map) by moving
		// the uPlot cursor to that x. Setting left off-canvas hides it.
		if (!plot) return;
		applyingExternal = true;
		if (hoverT == null) {
			plot.setCursor({ left: -10, top: -10 });
		} else {
			const left = plot.valToPos(hoverT, 'x');
			plot.setCursor({ left, top: height / 2 });
		}
		applyingExternal = false;
	});
</script>

<div class="relative w-full min-w-0">
	{#if zoomed}
		<button
			type="button"
			onclick={resetZoom}
			class="absolute right-2 top-2 z-10 rounded border border-zinc-700 bg-zinc-900/90 px-2 py-1 text-xs text-zinc-300 shadow hover:border-accent-500 hover:text-accent-300 max-md:py-2"
		>
			{m.ride_chart_reset()}
		</button>
	{/if}
	<div
		bind:this={container}
		class="w-full min-w-0"
		role="group"
		aria-label={yAxis?.label ?? series.map((s) => s.label).join(', ')}
		onpointerdown={inspectTouch}
	></div>
</div>

<style>
	:global(.uplot .u-legend) {
		font-size: 0.75rem;
	}
	:global(.uplot .u-legend tr) {
		margin-inline-end: 0.35rem;
		white-space: nowrap;
	}
</style>
