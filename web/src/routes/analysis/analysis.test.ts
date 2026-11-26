import { describe, expect, it } from 'vitest';
import {
	aggregateStress,
	changeFrom,
	describeTraining,
	formZone,
	isoDay,
	DAY,
	type Analysis
} from './analysis.js';

describe('training stress periods', () => {
	const data = {
		start: '2026-11-28',
		end: '2026-12-03',
		stress_days: [
			{ date: '2026-11-29', tss: 31.5, total: 2, scored: 1 },
			{ date: '2026-11-30', tss: null, total: 1, scored: 0 },
			{ date: '2026-12-01', tss: 0, total: 1, scored: 1 },
			{ date: '2026-12-03', tss: 64.25, total: 1, scored: 1 }
		]
	};

	it('distinguishes rest, missing scores and recorded zero in daily mode', () => {
		const days = aggregateStress(data, 'daily');
		expect(days.map(({ tss, total, scored }) => [tss, total, scored])).toEqual([
			[null, 0, 0],
			[31.5, 2, 1],
			[null, 1, 0],
			[0, 1, 1],
			[null, 0, 0],
			[64.25, 1, 1]
		]);
	});

	it('starts weeks on Monday and clips the reported period to the selected range', () => {
		const weeks = aggregateStress(data, 'weekly');
		expect(weeks.map(({ date, from, to }) => [isoDay(date), isoDay(from), isoDay(to)])).toEqual([
			['2026-11-23', '2026-11-28', '2026-11-29'],
			['2026-11-30', '2026-11-30', '2026-12-03']
		]);
		expect(weeks.map(({ tss, total, scored }) => [tss, total, scored])).toEqual([
			[31.5, 2, 1],
			[64.25, 3, 2]
		]);
	});

	it('preserves empty calendar months and handles year changes', () => {
		const months = aggregateStress({ ...data, end: '2027-02-02' }, 'monthly');
		expect(months.map(({ date, tss, total }) => [isoDay(date), tss, total])).toEqual([
			['2026-11-01', 31.5, 3],
			['2026-12-01', 64.25, 2],
			['2027-01-01', null, 0],
			['2027-02-01', null, 0]
		]);
	});
});

describe('training status', () => {
	it.each([
		[-125, 'Very fatigued'],
		[-20.1, 'Very fatigued'],
		[-20, 'Accumulated fatigue'],
		[-10.1, 'Accumulated fatigue'],
		[-10, 'Balanced'],
		[4.9, 'Balanced'],
		[5, 'Fresh'],
		[19.9, 'Fresh'],
		[20, 'Very fresh'],
		[125, 'Very fresh']
	])('puts %s in %s', (value, label) => {
		expect(formZone(value).label()).toBe(label);
	});

	const end = '2026-11-26';
	const history: Analysis = {
		start: '2026-10-27',
		end,
		dates: Array.from({ length: 31 }, (_, i) => isoDay(Date.parse(end) - (30 - i) * DAY)),
		ctl: [...Array<number>(30).fill(14), 7.9],
		atl: [...Array<number>(30).fill(1), 0.3],
		tsb: [...Array<number>(30).fill(13), 7.6],
		current: { ctl: 7.9, atl: 0.3, tsb: 7.6 },
		coverage: { imported: 0, sensor_estimate: 12, duration_estimate: 0, missing: 44 },
		recent_scored: 12,
		recent_total: 56,
		stress_days: []
	};

	it('distinguishes positive form from low long-term load and withholds advice when coverage is limited', () => {
		const status = describeTraining(history);
		expect(status.title).toBe('Fresh, but training load is low');
		expect(status.metrics.ctl.label).toBe('Low training load');
		expect(status.metrics.atl.label).toBe('Very low fatigue');
		expect(status.metrics.tsb.label).toBe('Fresh');
		expect(status.insight).toBe('');
	});

	it('uses exact historical dates and skips comparisons with insufficient history', () => {
		expect(changeFrom(history, 'ctl', 30)).toBeCloseTo(-6.1);
		expect(changeFrom(history, 'atl', 7)).toBeCloseTo(-0.7);
		expect(changeFrom({ ...history, dates: history.dates.slice(1) }, 'ctl', 30)).toBeNull();
		expect(describeTraining({ ...history, dates: [] }).title).toBe('Insufficient training data');
	});
});
