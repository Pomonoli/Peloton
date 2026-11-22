import { expect, it } from 'vitest';
import {
	filterCommunityRecords,
	recordMetric,
	recordPath,
	recordSport,
	recordValue,
	recordWindow,
	type CommunityRecord
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

it('links exact record windows and formats their units', () => {
	expect(recordPath(initial)).toBe('/records/leaderboards/ride/power/duration/1200');
	expect(recordWindow('duration', 1200)).toBe('20 min');
	expect(recordWindow('duration', 5)).toBe('5 sec');
	expect(recordWindow('duration', 3600)).toBe('1 hr');
	expect(recordWindow('distance', 750)).toBe('750 m');
	expect(recordWindow('distance', 5000)).toBe('5 km');
	expect(recordValue('pace', 157)).toBe('2:37 /km');
	expect(recordValue('power', 285)).toBe('285 W');
	expect(recordValue('speed', 10)).toMatch(/36/);
	expect(recordValue('heart_rate', 180)).toBe('180 bpm');
	expect(recordValue('vam', 2750)).toBe('2,750 m/h');
	expect(recordValue('other', 28.2)).toBe('28');
	expect(recordMetric('power')).toBe('Best measured power');
	expect(recordMetric('power', false)).toBe('Best power');
	expect(['pace', 'speed', 'heart_rate', 'vam', 'other'].map((name) => recordMetric(name))).toEqual(
		['Fastest pace', 'Best speed', 'Peak heart rate', 'Best climbing rate', 'other']
	);
	expect(recordSport('ride')).toBe('Cycling');
	expect(recordSport('other_sport')).toBe('other sport');
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
