import { describe, expect, it } from 'vitest';
import {
	heatmapColor,
	heatmapLegend,
	heatmapMetricLabel,
	heatmapMetrics,
	heatmapValue,
	heatmapViewport,
	missingHeatmapColor
} from './performance-heatmap';

describe('performance heatmap layers', () => {
	it('formats SI values and distinguishes recorded zeros from missing data', () => {
		expect(heatmapValue('speed', 32.5)).toBe('32.5 km/h');
		expect(heatmapValue('power', 0)).toBe('0 W');
		expect(heatmapValue('heart_rate', null)).toBe('Insufficient data');
		expect(heatmapValue('elevation', Number.NaN)).toBe('Insufficient data');
		expect(heatmapValue('gradient', -5.2)).toBe('-5.2 %');
		expect(heatmapValue('density', 12)).toBe('12');
	});
	it('renders six legend ranges with stops in ascending order', () => {
		expect(heatmapMetrics.map(heatmapMetricLabel)).toEqual([
			'Density',
			'Speed',
			'Heart Rate',
			'Elevation',
			'Gradient',
			'Power'
		]);
		for (const metric of heatmapMetrics) {
			const stops = heatmapLegend(metric).map((tick) => tick.value);
			expect(stops).toEqual([...stops].sort((a, b) => a - b));
			expect(heatmapColor(metric)).toEqual([
				'case',
				['==', ['typeof', ['get', metric]], 'number'],
				expect.any(Array),
				missingHeatmapColor
			]);
		}
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
