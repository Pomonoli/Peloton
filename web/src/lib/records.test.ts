import { expect, it } from 'vitest';
import {
	bestEffortValue,
	filterCommunityRecords,
	recordLabel,
	recordMetric,
	recordPath,
	recordSport,
	recordValue,
	recordWindow,
	type CommunityRecord,
	type RecordBucket
} from './records';

const initial: CommunityRecord = {
	activity_type: 'ride',
	metric: 'power',
	window_kind: 'duration',
	window_value: 1200,
	member_count: 2,
	result: {
		rank: 1,
		achieved_value: 285,
		activity_id: 'ride',
		username: 'zoe',
		display_name: 'Zoe',
		timestamp: '2026-11-14'
	}
};

const cyclingDistance: RecordBucket = {
	activity_type: 'ride',
	metric: 'pace',
	window_kind: 'distance',
	window_value: 1000
};

it('links exact record windows and formats their units', () => {
	expect(recordPath(initial)).toBe('/records/leaderboards/ride/power/duration/1200');
	expect(recordWindow('duration', 1200)).toBe('20 min');
	expect(recordWindow('duration', 5)).toBe('5 sec');
	expect(recordWindow('duration', 3600)).toBe('1 hr');
	expect(recordWindow('distance', 750)).toBe('750 m');
	expect(recordWindow('distance', 5000)).toBe('5 km');
	expect(recordValue({ ...cyclingDistance, activity_type: 'run' }, 157)).toBe('2:37 /km');
	expect(recordValue(initial, 285)).toBe('285 W');
	expect(recordValue({ ...initial, metric: 'speed' }, 10)).toBe('36 km/h');
	expect(recordValue({ ...initial, metric: 'heart_rate' }, 180)).toBe('180 bpm');
	expect(recordValue({ ...initial, metric: 'other' }, 28.2)).toBe('28.2');
	expect(recordMetric('power')).toBe('Best measured power');
	expect(recordMetric('power', false)).toBe('Best power');
	expect(['pace', 'speed', 'heart_rate', 'vam', 'other'].map((name) => recordMetric(name))).toEqual(
		['Fastest pace', 'Best speed', 'Peak heart rate', 'Most elevation gained', 'other']
	);
	expect(recordSport('ride')).toBe('Cycling');
	expect(recordSport('other_sport')).toBe('other sport');
});

it.each([
	[300, '75 m'],
	[1200, '300 m'],
	[3600, '900 m'],
	[450, '112.5 m']
])('formats a %i second climbing record as elevation gained', (seconds, expected) => {
	const bucket = { ...initial, metric: 'vam', window_value: seconds };
	expect(recordValue(bucket, 900)).toBe(expected);
	expect(bestEffortValue(bucket, 900)).toBe(expected);
});

it.each([
	[1000, 105, '1:45 · 34.29 km/h'],
	[5000, 102, '8:30 · 35.29 km/h'],
	[10000, 104, '17:20 · 34.62 km/h'],
	[400, 120, '0:48 · 30 km/h'],
	[42195, 100, '1:10:20 · 36 km/h'],
	[1000, 105.45, '1:45 · 34.14 km/h']
])('formats a %i m cycling record using the ranked pace', (distance, pace, expected) => {
	const bucket = { ...cyclingDistance, window_value: distance };
	expect(recordValue(bucket, pace)).toBe(expected);
	expect(bestEffortValue(bucket, pace)).toBe(expected);
});

it('labels cycling distance records and keeps duration metrics in their own units', () => {
	expect(recordLabel(cyclingDistance)).toBe('Fastest 1 km');
	expect(recordLabel(initial)).toBe('Best measured power · 20 min');
	expect(recordValue({ ...cyclingDistance, window_kind: 'duration' }, 105)).toBe('1:45 /km');
	expect(recordValue({ ...cyclingDistance, metric: 'power' }, 285)).toBe('285 W');
	expect(bestEffortValue({ ...initial, metric: 'speed' }, 10)).toBe('36 km/h');
	expect(bestEffortValue({ ...initial, metric: 'heart_rate' }, 179.6)).toBe('179.6 bpm');
});

it.each([0, -1, NaN, Infinity])('handles invalid distance or pace (%s)', (invalid) => {
	expect(recordValue({ ...cyclingDistance, window_value: invalid }, 105)).toBe('—');
	expect(recordValue(cyclingDistance, invalid)).toBe('—');
});

it('filters exact categories and sorts by member count or holder with stable fallback', () => {
	const popular = { ...initial, window_value: 300, member_count: 7 };
	const other = {
		...initial,
		window_value: 60,
		result: { ...initial.result!, display_name: 'Anna' }
	};
	const empty = { ...initial, metric: 'heart_rate', member_count: 0, result: null };
	const run = { ...initial, activity_type: 'run' };
	const rows = [initial, run, empty, popular, other];
	expect(filterCommunityRecords(rows, 'ride', 'power', false, 'members')).toEqual([
		popular,
		other,
		initial
	]);
	expect(filterCommunityRecords(rows, 'ride', 'power', false, 'holder')).toEqual([
		other,
		popular,
		initial
	]);
	expect(filterCommunityRecords(rows, 'ride', '', true, 'category')).toEqual([
		empty,
		other,
		popular,
		initial
	]);
	expect(filterCommunityRecords(rows, '', 'power', false, 'category')).toContain(run);
	expect(filterCommunityRecords(rows, '', 'heart_rate', false, 'category')).toEqual([]);
});

it('formats record values with at most two decimals', () => {
	expect(recordValue(initial, 504.333333333)).toBe('504.33 W');
	expect(recordValue({ ...initial, metric: 'speed' }, 28.75645 / 3.6)).toBe('28.76 km/h');
});
