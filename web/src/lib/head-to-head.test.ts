import { describe, expect, it } from 'vitest';
import { filterComparison, recordRows, segmentRows, type Comparison } from './head-to-head';

describe('Head-to-Head rows', () => {
	const ride = { activity_id: 'ride', value: 100 };
	const shared: Comparison['segments'][number] = {
		id: 'canal',
		name: 'Canal Sprint',
		distance_m: 1200,
		you: ride,
		opponent: { activity_id: 'other', value: 120 },
		you_speed_kmh: 43.2,
		opponent_speed_kmh: 36,
		time_difference_s: 20,
		speed_difference_kmh: 7.2,
		winner: 'you'
	};
	const forest = { ...shared, id: 'forest', name: 'Forest', winner: 'opponent' as const };
	const village = {
		...shared,
		id: 'village',
		name: 'Village',
		time_difference_s: 0,
		speed_difference_kmh: 0,
		winner: 'tie' as const
	};

	it('retains both rides, elapsed times and both kinds of gap', () => {
		const [row] = segmentRows([shared], 'Mira');
		expect(row?.cells).toMatchObject({
			name: { href: '/segment/canal' },
			you: { href: '/activities/ride', text: '1:40', detail: '43.2 km/h' },
			opponent: { href: '/activities/other', text: '2:00', detail: '36.0 km/h' },
			difference: { text: '20 sec', detail: '7.2 km/h' }
		});
	});

	it('sorts absolute gaps and speeds in either direction, with stable ties', () => {
		const rows = segmentRows([shared, forest, village], 'Mira');
		expect(filterComparison(rows, '', '', 'difference', true).map((r) => r.id)).toEqual([
			'canal',
			'forest',
			'village'
		]);
		expect(filterComparison(rows, '', '', 'speedGap', false).map((r) => r.id)).toEqual([
			'village',
			'canal',
			'forest'
		]);
		expect(filterComparison(rows, '  CANAL', 'you', 'name', false).map((r) => r.id)).toEqual([
			'canal'
		]);
		expect(filterComparison(rows, '', 'opponent', 'youSpeed', false).map((r) => r.id)).toEqual([
			'forest'
		]);
		expect(filterComparison(rows, '', 'tie', 'winner', true).map((r) => r.id)).toEqual(['village']);
	});

	it('displays normalized distance times and speed, and sorts unavailable PRs last', () => {
		const five: Comparison['records'][number] = {
			activity_type: 'ride',
			metric: 'pace',
			window_kind: 'distance',
			window_value: 5000,
			you: ride,
			opponent: { activity_id: 'other', value: 125 },
			winner: 'you',
			difference: 125,
			difference_percent: 25
		};
		const rows = recordRows(
			[
				{
					...five,
					window_value: 20000,
					opponent: null,
					winner: '',
					difference: null,
					difference_percent: null
				},
				five,
				{
					...five,
					metric: 'heart_rate',
					window_kind: 'duration',
					window_value: 300,
					winner: '',
					difference: null,
					difference_percent: null
				}
			],
			'Mira'
		);
		expect(rows[1]?.cells).toMatchObject({
			you: { text: '8:20', detail: '36.0 km/h' },
			opponent: { text: '10:25', detail: '28.8 km/h' },
			difference: { text: '125 sec', detail: '25%' }
		});
		expect(rows[0]?.cells.opponent?.text).toBe('Unavailable');
		expect(rows[2]?.cells.winner?.text).toBe('Unscored');
		expect(filterComparison(rows, '', '', 'percentGap', false)[0]?.id).toBe('pace:distance:5000');
		expect(filterComparison(rows, '20 km', '', 'name', false)[0]?.id).toBe('pace:distance:20000');
	});
});
