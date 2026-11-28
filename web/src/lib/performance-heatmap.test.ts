import type { Feature, LineString } from 'geojson';
import { describe, expect, it } from 'vitest';
import {
	adaptiveHeatmapScale,
	fixedHeatmapScale,
	heatmapColor,
	heatmapMetricLabel,
	heatmapMetrics,
	heatmapValue,
	heatmapViewport,
	missingHeatmapColor,
	visibleHeatmapFeatures,
	type HeatmapStop
} from './performance-heatmap';

const thresholds = (stops: HeatmapStop[]) => stops.map((stop) => stop.value);

describe('performance heatmap layers', () => {
	it('formats SI values and distinguishes recorded zeros from missing data', () => {
		expect(heatmapValue('speed', 32.5)).toBe('32.5 km/h');
		expect(heatmapValue('power', 0)).toBe('0 W');
		expect(heatmapValue('heart_rate', null)).toBe('Insufficient data');
		expect(heatmapValue('elevation', Number.NaN)).toBe('Insufficient data');
		expect(heatmapValue('gradient', -5.2)).toBe('-5.2 %');
		expect(heatmapValue('density', 12)).toBe('12');
		expect(heatmapValue('cadence', 86)).toBe('86 rpm');
	});
	it('preserves the palettes and their MapLibre color stops', () => {
		expect(heatmapMetrics.map(heatmapMetricLabel)).toEqual([
			'Density',
			'Speed',
			'Heart Rate',
			'Elevation',
			'Gradient',
			'Power',
			'Cadence'
		]);
		expect(thresholds(fixedHeatmapScale('speed'))).toEqual([0, 15, 30, 45, 60]);
		for (const metric of heatmapMetrics) {
			const stops = fixedHeatmapScale(metric);
			expect(thresholds(stops)).toEqual(thresholds(stops).sort((a, b) => a - b));
			expect(heatmapColor(metric, stops)).toEqual([
				'case',
				['==', ['typeof', ['get', metric]], 'number'],
				expect.any(Array),
				missingHeatmapColor
			]);
		}
	});
	it('spreads typical riding speeds across the palette and clamps the visual outliers', () => {
		const values = Object.freeze([1000, 0, 30, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29]);
		const stops = adaptiveHeatmapScale('speed', values);
		expect(thresholds(stops)).toEqual([20, 23, 25, 28, 30]);
		expect(stops.map(({ color }) => color)).toEqual(
			fixedHeatmapScale('speed').map(({ color }) => color)
		);
		expect(heatmapColor('speed', stops)).toEqual([
			'case',
			['==', ['typeof', ['get', 'speed']], 'number'],
			[
				'interpolate',
				['linear'],
				['number', ['get', 'speed']],
				...stops.flatMap(({ value, color }) => [value, color])
			],
			missingHeatmapColor
		]);
		expect(heatmapValue('speed', values[0])).toBe('1,000 km/h');
	});
	it('sets percentile thresholds from their ranks in a larger sample', () => {
		const values = Array.from({ length: 101 }, (_, i) => i);
		const stops = adaptiveHeatmapScale('power', values);
		expect(thresholds(stops)).toEqual([2, 26, 50, 74, 98]);
	});
	it('keeps the scale wide when sensor differences are small', () => {
		expect(thresholds(adaptiveHeatmapScale('heart_rate', [null, 137, 141]))).toEqual([
			129, 134, 139, 144, 149
		]);
		expect(thresholds(adaptiveHeatmapScale('elevation', [-3, 2]))).toEqual([-11, -7, -3, 1, 5, 9]);
		expect(thresholds(adaptiveHeatmapScale('power', [0, 4, null]))).toEqual([0, 13, 25, 38, 50]);
		expect(thresholds(adaptiveHeatmapScale('cadence', [79, 81]))).toEqual([70, 75, 80, 85, 90]);
	});
	it('keeps flat roads neutral and trims extreme grades', () => {
		const stops = adaptiveHeatmapScale('gradient', [
			-50,
			-6,
			-3,
			-1,
			...Array<number>(20).fill(0),
			1,
			2,
			8,
			50
		]);
		expect(thresholds(stops)).toEqual([-6, -3, 0, 4, 8]);
		expect(stops[2]).toEqual(fixedHeatmapScale('gradient')[2]);
		expect(thresholds(adaptiveHeatmapScale('gradient', [-4, -2]))).toEqual([-5, -3, -1]);
		expect(thresholds(adaptiveHeatmapScale('gradient', [2, 4]))).toEqual([1, 3, 5]);
		expect(adaptiveHeatmapScale('gradient', [0, 0])).toEqual([stops[2]]);
	});
	it('spaces density thresholds logarithmically and recovers distinct low passage counts', () => {
		expect(
			thresholds(adaptiveHeatmapScale('density', [1, 1, 2, 2, 3, 4, 12, 100, 100000]))
		).toEqual([1, 6, 26, 100]);
		const ties = Array<number>(100).fill(1);
		expect(thresholds(adaptiveHeatmapScale('density', [...ties, 2, 4000]))).toEqual([1, 2]);
	});
	it('handles missing values, identical values and narrow ranges with valid color expressions', () => {
		const empty = adaptiveHeatmapScale('power', [NaN, Infinity, -Infinity, '200', null, undefined]);
		expect(empty).toEqual([]);
		expect(heatmapColor('power', empty)).toBe(missingHeatmapColor);
		const constant = adaptiveHeatmapScale('power', [null, 0, 0]);
		expect(thresholds(constant)).toEqual([0]);
		expect(heatmapColor('power', constant)).toEqual([
			'case',
			['==', ['typeof', ['get', 'power']], 'number'],
			constant[0]!.color,
			missingHeatmapColor
		]);
		const narrow = adaptiveHeatmapScale('speed', [24, 24.1]);
		expect(thresholds(narrow)).toEqual([19, 22, 24, 27, 29]);
		expect(narrow[0]!.color).toBe(fixedHeatmapScale('speed')[0]!.color);
		expect(narrow.at(-1)!.color).toBe(fixedHeatmapScale('speed').at(-1)!.color);
	});
	it('requests an all-world overview and wraps precise regional viewports', () => {
		expect(heatmapViewport(6.9, -180, -50, 180, 50)).toBe('');
		expect(heatmapViewport(8, -500, -40, 500, 40)).toBe('level=1');
		const query = new URLSearchParams(heatmapViewport(14, 179.99, -90, 180.02, 90));
		expect(Object.fromEntries(query)).toEqual({
			level: '2',
			west: '179.990000',
			east: '-179.980000',
			south: '-85.05112878',
			north: '85.05112878'
		});
	});
});

describe('visible heatmap features', () => {
	function road(id: string, a: number[], b: number[]): Feature<LineString> {
		return {
			type: 'Feature',
			id,
			properties: {},
			geometry: { type: 'LineString', coordinates: [a, b] }
		};
	}

	it('includes crossing lines and excludes lines that only overlap the viewport bounding box', () => {
		const crossing = road('crossing', [-5, 5], [15, 5]);
		const offscreen = road('offscreen', [-5, 7], [7, 19]);
		const far = road('far', [60, 5], [61, 5]);
		const viewport = {
			west: 0,
			east: 10,
			south: 0,
			north: 10,
			width: 10,
			height: 10,
			axisAligned: true,
			project: ([x, y]: [number, number]) => ({ x, y })
		};
		expect(visibleHeatmapFeatures([crossing, offscreen, far], viewport)).toEqual([crossing]);
	});

	it('counts a road once when its visible copy is across the antimeridian', () => {
		const across = road('across', [-179, 1], [-178, 1]);
		const viewport = {
			west: 178,
			east: 188,
			south: 0,
			north: 10,
			width: 10,
			height: 10,
			axisAligned: false,
			project: ([x, y]: [number, number]) => ({ x: x - 178, y })
		};
		expect(visibleHeatmapFeatures([across], viewport)).toEqual([across]);
	});
});
