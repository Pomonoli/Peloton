import type { Feature } from 'geojson';
import type { DataDrivenPropertyValueSpecification, ExpressionSpecification } from 'maplibre-gl';
import { intlLocale } from '#lib/format.js';
import { m } from '#lib/paraglide/messages.js';

export const heatmapMetrics = [
	'density',
	'speed',
	'heart_rate',
	'elevation',
	'gradient',
	'power',
	'cadence'
] as const;
export type HeatmapMetric = (typeof heatmapMetrics)[number];
type NumericalHeatmapMetric = Exclude<HeatmapMetric, 'density'>;
export type HeatmapValues = Record<HeatmapMetric, number | null>;
export type HeatmapStop = { value: number; color: string };

const powerColors = ['#64748b', '#0369a1', '#0f766e', '#d97706', '#be185d'];

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
		colors: powerColors,
		unit: 'W'
	},
	cadence: { ticks: [0, 30, 60, 90, 120], colors: powerColors, unit: 'rpm' }
};

export function heatmapMetricLabel(metric: HeatmapMetric) {
	return {
		density: m.heatmap_density,
		speed: m.heatmap_speed,
		heart_rate: m.heatmap_heart_rate,
		elevation: m.heatmap_elevation,
		gradient: m.heatmap_gradient,
		power: m.heatmap_power,
		cadence: m.ride_cadence
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
function densityPercentileRange(sorted: number[]): [number, number] {
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
				: Number((low * (1 - fraction) + high * fraction).toFixed(metric === 'gradient' ? 1 : 0));
		// Keep thresholds at the precision of the aggregated values and ensure
		// MapLibre always receives strictly increasing stops.
		if (value > stops.at(-1)!.value && value < high) {
			stops.push({ value: value === 0 ? 0 : value, color: colors[i]! });
		}
	}
	stops.push({ value: high, color: colors[last]! });
	return stops;
}

const minimumRange: Record<NumericalHeatmapMetric, number> = {
	speed: 10,
	heart_rate: 20,
	elevation: 20,
	gradient: 4,
	power: 50,
	cadence: 20
};

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
	if (metric === 'density') {
		const [low, high] = densityPercentileRange(sorted);
		return low === high
			? [{ value: low, color: colors[middle]! }]
			: rangeStops(metric, low, high, colors);
	}

	// Use observed percentile thresholds and avoid trimming very small samples.
	const last = sorted.length - 1;
	const trim = sorted.length >= 10 ? Math.ceil(last * 0.02) : 0;
	let low = sorted[trim]!;
	let high = sorted[last - trim]!;
	const range = minimumRange[metric];

	if (sorted[0] === sorted[last]) return [{ value: low, color: colors[middle]! }];
	if (high - low < range) {
		const midpoint = (low + high) / 2;
		if (metric === 'gradient' && high <= 0) {
			high = Math.min(0, Math.ceil(midpoint + range / 2));
			low = high - range;
		} else {
			const lowerBound =
				metric === 'elevation' || (metric === 'gradient' && low < 0) ? -Infinity : 0;
			low = Math.max(lowerBound, Math.floor(midpoint - range / 2));
			high = low + range;
		}
	}

	if (metric === 'gradient') {
		// Keep the neutral color at zero and use the corresponding half of
		// the palette when the visible grades are all on one side of zero.
		if (low >= 0) return rangeStops(metric, low, high, colors.slice(middle));
		if (high <= 0) return rangeStops(metric, low, high, colors.slice(0, middle + 1));
		return [
			...rangeStops(metric, low, 0, colors.slice(0, middle + 1)),
			...rangeStops(metric, 0, high, colors.slice(middle)).slice(1)
		];
	}

	return rangeStops(metric, low, high, colors);
}

type ScreenPoint = { x: number; y: number };

export function visibleHeatmapFeatures(
	features: Feature[],
	viewport: {
		west: number;
		east: number;
		south: number;
		north: number;
		width: number;
		height: number;
		axisAligned: boolean;
		project: (position: [number, number]) => ScreenPoint;
	}
): Feature[] {
	const { west, east, south, north, width, height, axisAligned, project } = viewport;
	return features.filter(({ geometry }) => {
		if (geometry.type !== 'LineString') return false;
		// The performance endpoint returns a two-point line for each road bin.
		const [a, b] = geometry.coordinates;
		if (!a || !b) return false;
		const [ax, ay] = a as [number, number];
		const [bx, by] = b as [number, number];
		if (Math.max(ay, by) < south || Math.min(ay, by) > north) return false;
		const firstCopy = Math.ceil((west - Math.max(ax, bx)) / 360);
		const lastCopy = Math.floor((east - Math.min(ax, bx)) / 360);
		for (let copy = firstCopy; copy <= lastCopy; copy++) {
			const offset = copy * 360;
			if (
				axisAligned &&
				((ay >= south && ay <= north && ax + offset >= west && ax + offset <= east) ||
					(by >= south && by <= north && bx + offset >= west && bx + offset <= east))
			)
				return true;
			if (
				segmentIntersectsViewport(
					project([ax + offset, ay]),
					project([bx + offset, by]),
					width,
					height
				)
			) {
				return true;
			}
		}
		return false;
	});
}

function segmentIntersectsViewport(a: ScreenPoint, b: ScreenPoint, width: number, height: number) {
	// Clip the projected segment against each viewport axis. This also catches
	// lines that cross the viewport with both endpoints outside it.
	let enter = 0;
	let exit = 1;
	for (const [start, end, limit] of [
		[a.x, b.x, width],
		[a.y, b.y, height]
	] as const) {
		const delta = end - start;
		if (delta === 0) {
			if (start < 0 || start > limit) return false;
			continue;
		}
		const near = -start / delta;
		const far = (limit - start) / delta;
		enter = Math.max(enter, Math.min(near, far));
		exit = Math.min(exit, Math.max(near, far));
		if (enter > exit) return false;
	}
	return true;
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
