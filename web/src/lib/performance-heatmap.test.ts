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
	});
	it('preserves the six fixed scales and their MapLibre color stops', () => {
		expect(heatmapMetrics.map(heatmapMetricLabel)).toEqual([
			'Density',
			'Speed',
			'Heart Rate',
			'Elevation',
			'Gradient',
			'Power'
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
		expect(thresholds(stops)).toEqual([20, 22.5, 25, 27.5, 30]);
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
		expect(thresholds(stops)).toEqual([5, 27.5, 50, 72.5, 95]);
	});
	it('uses the recorded distribution and units for each sensor', () => {
		expect(thresholds(adaptiveHeatmapScale('heart_rate', [null, 137, 141]))).toEqual([
			137, 138, 139, 140, 141
		]);
		expect(thresholds(adaptiveHeatmapScale('elevation', [-3, 2]))).toEqual([-3, -2, -1, 0, 1, 2]);
		expect(thresholds(adaptiveHeatmapScale('power', [0, 4, null]))).toEqual([0, 1, 2, 3, 4]);
	});
	it('keeps zero neutral and adapts the two gradient sides independently', () => {
		const stops = adaptiveHeatmapScale('gradient', [
			-50,
			-6,
			-3,
			-1,
			...Array<number>(100).fill(0),
			1,
			2,
			8,
			50
		]);
		expect(thresholds(stops)).toEqual([-6, -3, 0, 4, 8]);
		expect(stops[2]).toEqual(fixedHeatmapScale('gradient')[2]);
		expect(thresholds(adaptiveHeatmapScale('gradient', [-4, -2]))).toEqual([-4, -2, 0]);
		expect(thresholds(adaptiveHeatmapScale('gradient', [2, 4]))).toEqual([0, 2, 4]);
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
		expect(thresholds(narrow)).toEqual([24, 24.1]);
		expect(narrow[0]!.color).toBe(fixedHeatmapScale('speed')[0]!.color);
		expect(narrow[1]!.color).toBe(fixedHeatmapScale('speed').at(-1)!.color);
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
