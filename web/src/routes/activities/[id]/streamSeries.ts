import { m } from '#lib/paraglide/messages.js';
import type { ActivityStreamView } from './+layout';

export interface ChartSeries {
	label: string;
	values: (number | null)[];
	color: string;
	unit?: string;
	fill?: boolean;
}

// Build the StreamChart series for an activity — only includes channels that
// actually carry data. Shared by the activity detail page and the full-screen
// streams subpage so both render identical series.
export function buildSeries(stream: ActivityStreamView | null): ChartSeries[] {
	if (!stream) return [];
	const xs: ChartSeries[] = [];
	if (stream.hr) {
		xs.push({
			label: m.detail_label_heart_rate(),
			values: stream.hr,
			color: 'var(--plot-heart-rate)',
			unit: 'bpm'
		});
	}
	if (stream.power) {
		xs.push({ label: 'Power', values: stream.power, color: 'var(--plot-power)', unit: 'W' });
	}
	if (stream.cadence) {
		xs.push({
			label: 'Cadence',
			values: stream.cadence,
			color: 'var(--plot-cadence)',
			unit: 'rpm'
		});
	}
	if (stream.speed) {
		// m/s → km/h for a readable axis.
		xs.push({
			label: 'Speed',
			values: stream.speed.map((v) => (v == null ? null : Math.round(v * 3.6 * 10) / 10)),
			color: 'var(--plot-speed)',
			unit: 'km/h'
		});
	}
	if (stream.altitude) {
		xs.push({
			label: 'Elevation',
			values: stream.altitude,
			color: 'var(--plot-elevation)',
			unit: 'm',
			fill: true
		});
	}
	return xs;
}
