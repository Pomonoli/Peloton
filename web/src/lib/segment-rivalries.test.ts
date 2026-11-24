import { expect, it } from 'vitest';
import { alignEffortHistory, rivalryFrom, segmentSpeedRange } from './segment-rivalries';

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
	const chart = alignEffortHistory(
		[
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
		],
		1000
	);
	expect(chart.offsets).toEqual([june, july, august].map((date) => Date.parse(date) / 1000));
	expect(chart.times).toEqual([
		[60, null, 50],
		[null, 49, null]
	]);
	expect(chart.values[0]).toEqual([expect.closeTo(60, 8), null, 72]);
	expect(alignEffortHistory([[{ start_time: june, elapsed_s: 30 }]], 1000).times).toEqual([
		[null, 30, null]
	]);
	expect(alignEffortHistory([], 1000)).toEqual({ offsets: [], times: [], values: [] });
	expect(alignEffortHistory([[{ start_time: june, elapsed_s: 30 }]], -1).offsets).toEqual([]);
});

it('fits the speed scale around equal or close efforts', () => {
	expect(segmentSpeedRange(36, 36)).toEqual([35.9, 36.1]);
	expect(segmentSpeedRange(35.98, 36.02)).toEqual([expect.closeTo(35.88), expect.closeTo(36.12)]);
	expect(segmentSpeedRange(20, 45)).toEqual([17.5, 47.5]);
});
