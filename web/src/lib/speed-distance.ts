import { axisTicks, tickStep } from '#lib/chart-scale.js';

export interface SpeedDistancePoint {
	id: string;
	title: string;
	start: string;
	distance_km: number;
	speed_kmh: number;
}

export function scatterScale(low: number, high: number, divisions: number) {
	const padding = Math.max(0.1, (high - low) * 0.08, high * 0.025);
	const step = tickStep((high - low + 2 * padding) / divisions);
	const min = Math.max(0, Math.floor((low - padding) / step) * step);
	const max = Math.ceil((high + padding) / step) * step;
	return { min, max, ticks: axisTicks(min, max, step) };
}

export function linearTrend(points: SpeedDistancePoint[]) {
	if (points.length < 2) return null;
	let x = 0;
	let y = 0;
	let low = Infinity;
	let high = -Infinity;
	for (const p of points) {
		x += p.distance_km;
		y += p.speed_kmh;
		low = Math.min(low, p.distance_km);
		high = Math.max(high, p.distance_km);
	}
	x /= points.length;
	y /= points.length;
	let covariance = 0;
	let variance = 0;
	for (const p of points) {
		const dx = p.distance_km - x;
		covariance += dx * (p.speed_kmh - y);
		variance += dx * dx;
	}
	if (!variance) return null;
	const slope = covariance / variance;
	return { low, high, lowSpeed: y + slope * (low - x), highSpeed: y + slope * (high - x) };
}

export function scatterQuery(search: string, year: string) {
	const params = new URLSearchParams(search);
	params.delete('member');
	if (year === 'all') params.delete('year');
	else params.set('year', year);
	return params.toString();
}

const extraHues = [303, 177, 11, 225, 73, 326];

export function riderColor(index: number) {
	if (index >= 0 && index < 6) return `var(--plot-rider-${index + 1})`;
	const hue = extraHues[index - 6] ?? (index * 137.508) % 360;
	return `hsl(${hue} 72% var(--scatter-lightness))`;
}
