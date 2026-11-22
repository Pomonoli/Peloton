import {
	formatDistanceEffort,
	formatHeartRate,
	formatPower,
	formatSpeed,
	formatSpeedKmh,
	intlLocale
} from '#lib/format.js';
import { m } from '#lib/paraglide/messages.js';

export type RecordBucket = {
	activity_type: string;
	metric: string;
	window_kind: string;
	window_value: number;
};
export type PersonalRecord = RecordBucket & {
	achieved_value: number;
	activity_id: string;
	timestamp: string;
};
export type CommunityRecord = RecordBucket & {
	member_count: number;
	result:
		| (Omit<PersonalRecord, keyof RecordBucket> & {
				rank: number;
				username: string;
				display_name: string;
		  })
		| null;
};

export function recordPath(bucket: RecordBucket) {
	return (
		'/records/leaderboards/' +
		[bucket.activity_type, bucket.metric, bucket.window_kind, bucket.window_value]
			.map(encodeURIComponent)
			.join('/')
	);
}

export function recordWindow(kind: string, value: number): string {
	const unit =
		kind === 'distance'
			? value >= 1000
				? 'kilometer'
				: 'meter'
			: value >= 3600
				? 'hour'
				: value >= 60
					? 'minute'
					: 'second';
	const divisor = { kilometer: 1000, meter: 1, hour: 3600, minute: 60, second: 1 }[unit];
	return new Intl.NumberFormat(intlLocale(), {
		style: 'unit',
		unit,
		maximumFractionDigits: 2
	}).format(value / divisor);
}

export function recordMetric(metric: string, measured = true): string {
	switch (metric) {
		case 'pace':
			return m.record_pace();
		case 'speed':
			return m.record_speed();
		case 'power':
			return measured ? m.record_power() : m.record_personal_power();
		case 'heart_rate':
			return m.record_heart_rate();
		case 'vam':
			return m.record_vam();
		default:
			return metric;
	}
}

export function isCyclingDistanceRecord({
	activity_type,
	metric,
	window_kind
}: RecordBucket): boolean {
	return activity_type === 'ride' && metric === 'pace' && window_kind === 'distance';
}

export function recordLabel(bucket: RecordBucket, measured = true): string {
	const window = recordWindow(bucket.window_kind, bucket.window_value);
	return isCyclingDistanceRecord(bucket)
		? m.record_fastest_distance({ distance: window })
		: `${recordMetric(bucket.metric, measured)} · ${window}`;
}

export function recordValue(bucket: RecordBucket, value: number): string {
	const { metric, window_value } = bucket;
	if (isCyclingDistanceRecord(bucket)) {
		if (!Number.isFinite(window_value) || window_value <= 0) return m.placeholder_dash();
		// Stored pace is seconds per kilometer, calculated over the sampled window.
		// Scale to the category distance so displayed time follows the ranked value.
		return formatDistanceEffort(window_value, value * (window_value / 1000));
	}
	switch (metric) {
		case 'pace': {
			const seconds = Math.round(value);
			return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')} /km`;
		}
		case 'speed':
			return formatSpeed(value);
		case 'power':
			return formatPower(value);
		case 'heart_rate':
			return formatHeartRate(value);
		case 'vam':
			return `${Math.round(value).toLocaleString(intlLocale())} m/h`;
		default:
			return Math.round(value).toLocaleString(intlLocale());
	}
}

export function bestEffortValue(bucket: RecordBucket, value: number): string {
	switch (bucket.metric) {
		case 'speed':
			return formatSpeedKmh(value * 3.6);
		case 'power':
		case 'heart_rate':
			return recordValue(bucket, Math.round(value));
		default:
			return recordValue(bucket, value);
	}
}

export function recordSport(type: string) {
	return (
		(
			{
				ride: m.record_sport_ride(),
				run: m.record_sport_run(),
				swim: m.record_sport_swim(),
				walk: m.record_sport_walk(),
				hike: m.record_sport_hike(),
				row: m.record_sport_row(),
				ski: m.record_sport_ski(),
				workout: m.record_sport_workout()
			} as Record<string, string>
		)[type] || type.replaceAll('_', ' ')
	);
}

export type RecordOrder = 'category' | 'members' | 'holder';

export function filterCommunityRecords(
	rows: CommunityRecord[],
	sport: string,
	metric: string,
	showEmpty: boolean,
	order: RecordOrder
) {
	return rows
		.filter(
			(row) =>
				(!sport || row.activity_type === sport) &&
				(!metric || row.metric === metric) &&
				(showEmpty || row.member_count > 0)
		)
		.sort((a, b) => {
			const category =
				a.activity_type.localeCompare(b.activity_type) ||
				a.metric.localeCompare(b.metric) ||
				a.window_kind.localeCompare(b.window_kind) ||
				a.window_value - b.window_value;
			switch (order) {
				case 'members':
					return b.member_count - a.member_count || category;
				case 'holder':
					return (
						(a.result?.display_name ?? '').localeCompare(
							b.result?.display_name ?? '',
							intlLocale()
						) || category
					);
				default:
					return category;
			}
		});
}
