import { formatMetricDistance, formatSpeedKmh, intlLocale } from '#lib/format.js';
import { m } from '#lib/paraglide/messages.js';
import {
	recordValue,
	compareRecordCategories,
	isCyclingDistanceRecord,
	recordSpeed,
	recordLabel,
	type RecordBucket
} from '#lib/records.js';

export type Winner = 'you' | 'opponent' | 'tie';
type Effort = { activity_id: string; value: number };
export type Rider = { id: string; username: string; display_name: string; avatar_url: string };
type Segment = {
	id: string;
	name: string;
	distance_m: number;
	you: Effort | null;
	opponent: Effort | null;
	you_speed_kmh: number;
	opponent_speed_kmh: number;
	time_difference_s: number;
	speed_difference_kmh: number;
	winner: Winner;
};
type RecordResult = RecordBucket & {
	you: Effort | null;
	opponent: Effort | null;
	winner: Winner | '';
	difference: number | null;
	difference_percent: number | null;
};
export type Score = { wins: number; losses: number; ties: number; comparable: number };
export type Comparison = {
	first?: Rider;
	opponent: Rider;
	segments: Segment[];
	records: RecordResult[];
	segment_score: Score;
	record_score: Score;
};

export type ColumnKey = 'name' | 'distance' | 'you' | 'opponent' | 'winner';
export type SortKey = ColumnKey | 'category';
export type Column = { key: ColumnKey; label: string };
type Cell = { text: string; detail?: string | undefined; href?: string };
type SortValue = number | string | null;
export type ComparisonRow = {
	id: string;
	name: string;
	winner: Winner | '';
	cells: Partial<Record<ColumnKey, Cell>>;
	values: Partial<Record<SortKey, SortValue>>;
};

export function riderName(rider: Rider) {
	return rider.display_name.trim() || rider.username;
}

export function winnerLabel(winner: Winner, opponent: string, first: string = m.comparison_you()) {
	switch (winner) {
		case 'you':
			return first;
		case 'opponent':
			return opponent;
		case 'tie':
			return m.comparison_tie();
	}
}

function effortTime(seconds: number) {
	const total = Math.round(seconds * 100) / 100;
	const minutes = Math.floor(total / 60);
	const sec = new Intl.NumberFormat(intlLocale(), {
		minimumIntegerDigits: 2,
		maximumFractionDigits: 2
	}).format(total - minutes * 60);
	const minuteText = new Intl.NumberFormat(intlLocale(), {
		minimumIntegerDigits: minutes >= 60 ? 2 : 1,
		useGrouping: false
	}).format(minutes % 60);
	return minutes >= 60
		? `${Math.floor(minutes / 60)}:${minuteText}:${sec}`
		: `${minuteText}:${sec}`;
}

function effortCell(effort: Effort | null, text: string, detail?: string): Cell {
	return effort
		? { text, detail, href: `/activities/${encodeURIComponent(effort.activity_id)}` }
		: { text: m.comparison_unavailable() };
}

export function segmentRows(
	segments: Segment[],
	opponent: string,
	first: string = m.comparison_you()
): ComparisonRow[] {
	return segments.map((s) => ({
		id: s.id,
		name: s.name,
		winner: s.winner,
		cells: {
			name: { text: s.name, href: `/segment/${encodeURIComponent(s.id)}` },
			distance: { text: formatMetricDistance(s.distance_m) },
			you: effortCell(
				s.you,
				formatSpeedKmh(s.you_speed_kmh),
				s.you ? effortTime(s.you.value) : undefined
			),
			opponent: effortCell(
				s.opponent,
				formatSpeedKmh(s.opponent_speed_kmh),
				s.opponent ? effortTime(s.opponent.value) : undefined
			),
			winner: { text: winnerLabel(s.winner, opponent, first) }
		},
		values: {
			name: s.name,
			distance: s.distance_m,
			you:
				s.you && Number.isFinite(s.you_speed_kmh) && s.you_speed_kmh > 0 ? s.you_speed_kmh : null,
			opponent:
				s.opponent && Number.isFinite(s.opponent_speed_kmh) && s.opponent_speed_kmh > 0
					? s.opponent_speed_kmh
					: null,
			winner: winnerLabel(s.winner, opponent, first)
		}
	}));
}

export function recordRows(
	records: RecordResult[],
	opponent: string,
	first: string = m.comparison_you()
): ComparisonRow[] {
	return [...records].sort(compareRecordCategories).map((r, category) => {
		const name = recordLabel(r);
		const distance = isCyclingDistanceRecord(r);
		const value = (effort: Effort | null) =>
			effort && distance ? (effort.value * r.window_value) / 1000 : (effort?.value ?? null);
		const cell = (effort: Effort | null) => {
			if (!effort) return effortCell(null, '');
			return distance
				? effortCell(
						effort,
						effortTime((effort.value * r.window_value) / 1000),
						recordSpeed(3600 / effort.value)
					)
				: effortCell(effort, recordValue(r, effort.value));
		};
		const winner = r.winner ? winnerLabel(r.winner, opponent, first) : null;
		return {
			id: `${r.activity_type}:${r.metric}:${r.window_kind}:${r.window_value}`,
			name,
			winner: r.winner,
			cells: {
				name: { text: name },
				you: cell(r.you),
				opponent: cell(r.opponent),
				winner: {
					text:
						winner ?? (r.metric === 'heart_rate' ? m.comparison_unscored() : m.placeholder_dash())
				}
			},
			values: {
				name,
				you: value(r.you),
				opponent: value(r.opponent),
				category,
				winner
			}
		};
	});
}

export function filterComparison(
	rows: ComparisonRow[],
	query: string,
	winner: Winner | '',
	sort: SortKey,
	descending: boolean
) {
	const locale = intlLocale();
	const search = query.trim().toLocaleLowerCase(locale);
	return rows
		.filter(
			(row) =>
				(!winner || row.winner === winner) && row.name.toLocaleLowerCase(locale).includes(search)
		)
		.sort((a, b) => {
			const x = a.values[sort];
			const y = b.values[sort];
			// Missing values stay last and equal values retain their source order.
			if (x == null) return y == null ? 0 : 1;
			if (y == null) return -1;
			const order =
				typeof x === 'number' && typeof y === 'number'
					? x - y
					: String(x).localeCompare(String(y), locale, { numeric: true, sensitivity: 'base' });
			return descending ? -order : order;
		});
}
