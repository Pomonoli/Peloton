import { formatDistance, formatDuration, formatElevation, intlLocale } from './format.js';
import type { IconName } from './icons.js';
import { m } from './paraglide/messages.js';

export type ActivityTotals = {
	count: number;
	distance_m: number;
	moving_s: number;
	elevation_gain_m: number;
};

export function activityMetrics(totals: ActivityTotals) {
	return [
		{ label: m.nav_activities(), value: totals.count.toLocaleString(intlLocale()), icon: 'gear' },
		{ label: m.col_distance(), value: formatDistance(totals.distance_m), icon: 'routes' },
		{ label: m.metric_moving_time(), value: formatDuration(totals.moving_s), icon: 'clock' },
		{ label: m.col_elevation(), value: formatElevation(totals.elevation_gain_m), icon: 'segments' }
	] satisfies { label: string; value: string; icon: IconName }[];
}
