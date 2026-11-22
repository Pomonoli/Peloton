import { intlLocale } from '#lib/format.js';
import { m } from '#lib/paraglide/messages.js';
import type { RideMetric } from '#lib/ride-track.js';

export const metricLabel: Record<RideMetric, () => string> = {
	speedKmh: m.ride_speed,
	eleM: m.ride_elevation,
	gradient: m.ride_gradient,
	hr: m.detail_label_heart_rate,
	power: m.ride_power
};

export function metricValue(metric: RideMetric, value: number): string {
	const units = { speedKmh: 'km/h', eleM: 'm', gradient: '%', hr: 'bpm', power: 'W' };
	const digits = metric === 'speedKmh' || metric === 'gradient' ? 1 : 0;
	return `${new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: digits }).format(value)} ${units[metric]}`;
}
