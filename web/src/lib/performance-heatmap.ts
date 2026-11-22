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

export function heatmapLegend(metric: HeatmapMetric) {
	const { ticks, colors } = scales[metric];
	return ticks.map((value, i) => ({
		value,
		color: colors[i]!,
		label: heatmapValue(metric, value)
	}));
}

export function heatmapColor(metric: HeatmapMetric): DataDrivenPropertyValueSpecification<string> {
	const get: ExpressionSpecification = ['get', metric];
	const stops = heatmapLegend(metric).flatMap(({ value, color }) => [value, color] as const);
	const scale = ['interpolate', ['linear'], ['number', get], ...stops] as ExpressionSpecification;
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
