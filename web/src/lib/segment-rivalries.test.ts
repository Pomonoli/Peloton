import { expect, it } from 'vitest';
import { alignEffortHistory, rivalryFrom } from './segment-rivalries';

it('uses the start of the local calendar year and a rolling 90-day window', () => {
	const now = new Date(2026, 10, 22, 10, 15);
	expect(rivalryFrom('all', now)).toBeNull();
	expect(rivalryFrom('year', now)).toBe(new Date(2026, 0, 1).toISOString());
	expect(Date.parse(rivalryFrom('recent', now)!)).toBe(now.getTime() - 90 * 86400_000);
});

it('plots each rider on the same real calendar, preserving gaps and fastest duplicates', () => {
	const june = '2026-06-01T10:00:00Z';
	const july = '2026-07-03T10:00:00Z';
	const august = '2026-08-01T10:00:00Z';
	const chart = alignEffortHistory([
		[
			{ start_time: august, elapsed_s: 50 },
			{ start_time: june, elapsed_s: 60 },
			{ start_time: august, elapsed_s: 55 },
			{ start_time: 'invalid', elapsed_s: 60 }
		],
		[
			{ start_time: july, elapsed_s: 49 },
			{ start_time: june, elapsed_s: 0 }
		]
	]);
	expect(chart.offsets).toEqual([june, july, august].map((date) => Date.parse(date) / 1000));
	expect(chart.values).toEqual([
		[60, null, 50],
		[null, 49, null]
	]);
	expect(alignEffortHistory([[{ start_time: june, elapsed_s: 30 }]]).values).toEqual([
		[null, 30, null]
	]);
	expect(alignEffortHistory([])).toEqual({ offsets: [], values: [] });
});
