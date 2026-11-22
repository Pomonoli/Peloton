import type { DataDrivenPropertyValueSpecification, ExpressionSpecification } from 'maplibre-gl';
import { intlLocale } from '#lib/format.js';
import { m } from '#lib/paraglide/messages.js';

export type HeatmapMetric = 'density' | 'speed' | 'heart_rate' | 'elevation' | 'gradient' | 'power';

export const heatmapMetrics = [
	'density',
	'speed',
	'heart_rate',
	'elevation',
	'gradient',
	'power'
] as const;
export type HeatmapValues = Record<HeatmapMetric, number | null>;
export type HeatmapStop = { value: number; color: string };

const scales: Record<HeatmapMetric, { ticks: number[]; colors: string[]; unit: string }> = {
	density: {
		ticks: [1, 5, 15, 30],
		colors: ['#fbbf24', '#f97316', '#ef4444', '#be185d'],
		unit: ''
	},
	speed: {
		ticks: [0, 15, 30, 45, 60],
		colors: ['#64748b', '#2563eb', '#0891b2', '#15803d', '#eab308'],
		unit: 'km/h'
	},
	heart_rate: {
		ticks: [60, 100, 140, 180, 220],
		colors: ['#7e22ce', '#2563eb', '#16a34a', '#f97316', '#b91c1c'],
		unit: 'bpm'
	},
	elevation: {
		ticks: [-100, 0, 250, 750, 1500, 3000],
		colors: ['#075985', '#047857', '#84a52e', '#ad792c', '#9a5b41', '#eee3d2'],
		unit: 'm'
	},
	gradient: {
		ticks: [-15, -6, 0, 6, 15],
		colors: ['#312e81', '#2563eb', '#a8a29e', '#f59e0b', '#b91c1c'],
		unit: '%'
	},
	power: {
		ticks: [0, 100, 200, 300, 500],
		colors: ['#64748b', '#0369a1', '#0f766e', '#d97706', '#be185d'],
		unit: 'W'
	}
};

export function heatmapMetricLabel(metric: HeatmapMetric) {
	return {
		density: m.heatmap_density,
		speed: m.heatmap_speed,
		heart_rate: m.heatmap_heart_rate,
		elevation: m.heatmap_elevation,
		gradient: m.heatmap_gradient,
		power: m.heatmap_power
	}[metric]();
}

export function heatmapValue(metric: HeatmapMetric, value: number | null | undefined) {
	if (typeof value !== 'number' || !Number.isFinite(value)) return m.heatmap_insufficient();
	const number = new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 1 }).format(value);
	const unit = scales[metric].unit;
	return unit ? `${number} ${unit}` : number;
}

export const missingHeatmapColor = '#737b86';

export function fixedHeatmapScale(metric: HeatmapMetric): HeatmapStop[] {
	const { ticks, colors } = scales[metric];
	return ticks.map((value, i) => ({ value, color: colors[i]! }));
}

// Round P5/P95 ranks inward so an isolated extreme can't pull the thresholds
// outward through interpolation. With fewer than four values use the full range.
function percentileRange(sorted: number[]): [number, number] {
	const last = sorted.length - 1;
	const trim = last >= 3 ? Math.ceil(last * 0.05) : 0;
	const low = sorted[trim]!;
	const high = sorted[last - trim]!;
	if (low !== high) return [low, high];
	// Repeated values can occupy both percentiles. Recover any variation using
	// the nearest distinct values, which may still leave only one stop.
	return [sorted.findLast((v) => v < low) ?? low, sorted.find((v) => v > high) ?? high];
}

function rangeStops(
	metric: HeatmapMetric,
	low: number,
	high: number,
	colors: string[]
): HeatmapStop[] {
	const last = colors.length - 1;
	const stops = [{ value: low, color: colors[0]! }];
	for (let i = 1; i < last; i++) {
		const fraction = i / last;
		const value =
			metric === 'density'
				? Math.round(Math.expm1(Math.log1p(low) * (1 - fraction) + Math.log1p(high) * fraction))
				: Number((low * (1 - fraction) + high * fraction).toFixed(1));
		// Keep thresholds at the precision of the aggregated values and ensure
		// MapLibre always receives strictly increasing stops.
		if (value > stops.at(-1)!.value && value < high) {
			stops.push({ value: value === 0 ? 0 : value, color: colors[i]! });
		}
	}
	stops.push({ value: high, color: colors[last]! });
	return stops;
}

export function adaptiveHeatmapScale(
	metric: HeatmapMetric,
	values: readonly unknown[]
): HeatmapStop[] {
	const sorted = values
		.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
		.sort((a, b) => a - b);
	if (!sorted.length) return [];
	const { colors } = scales[metric];
	const middle = Math.floor(colors.length / 2);
	if (metric === 'gradient') {
		// Find the extent on each side independently so a minority of climbs or
		// descents can still use its palette. Flat roads always use the neutral color.
		const descents = sorted.filter((v) => v < 0);
		const climbs = sorted.filter((v) => v > 0);
		const below = descents.length
			? rangeStops(metric, percentileRange(descents)[0], 0, colors.slice(0, middle + 1)).slice(
					0,
					-1
				)
			: [];
		const above = climbs.length
			? rangeStops(metric, 0, percentileRange(climbs)[1], colors.slice(middle)).slice(1)
			: [];
		return [...below, { value: 0, color: colors[middle]! }, ...above];
	}
	const [low, high] = percentileRange(sorted);
	if (low === high) return [{ value: low, color: colors[middle]! }];
	return rangeStops(metric, low, high, colors);
}

export function heatmapColor(
	metric: HeatmapMetric,
	stops: readonly HeatmapStop[]
): DataDrivenPropertyValueSpecification<string> {
	if (!stops.length) return missingHeatmapColor;
	const get: ExpressionSpecification = ['get', metric];
	const scale =
		stops.length === 1
			? stops[0]!.color
			: ([
					'interpolate',
					['linear'],
					['number', get],
					...stops.flatMap(({ value, color }) => [value, color])
				] as ExpressionSpecification);
	// MapLibre clamps interpolation to the endpoint colors. Sensor values stay in GeoJSON.
	return ['case', ['==', ['typeof', get], 'number'], scale, missingHeatmapColor];
}

export function heatmapViewport(
	zoom: number,
	west: number,
	south: number,
	east: number,
	north: number
) {
	const level = zoom >= 12 ? 2 : zoom >= 7 ? 1 : 0;
	if (level === 0) return '';
	const query = new URLSearchParams({ level: String(level) });
	const span = east - west;
	if (span > 0 && span < 360) {
		const latitude = 85.05112878;
		const wrap = (x: number) => ((((x + 180) % 360) + 360) % 360) - 180;
		query.set('west', wrap(west).toFixed(6));
		query.set('east', wrap(east).toFixed(6));
		query.set('south', Math.max(-latitude, south).toFixed(8));
		query.set('north', Math.min(latitude, north).toFixed(8));
	}
	return query.toString();
}
