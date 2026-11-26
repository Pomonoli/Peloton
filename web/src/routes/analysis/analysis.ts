import { intlLocale } from '#lib/format.js';
import { m } from '#lib/paraglide/messages.js';
import { prefs } from '#lib/prefs.svelte.js';

export const METRICS = [
	{
		key: 'ctl',
		label: m.analysis_fitness,
		color: 'var(--plot-speed)',
		info: m.analysis_fitness_description
	},
	{
		key: 'atl',
		label: m.analysis_fatigue,
		color: 'var(--plot-fatigue)',
		info: m.analysis_fatigue_description
	},
	{
		key: 'tsb',
		label: m.analysis_form,
		color: 'var(--plot-form)',
		info: m.analysis_form_description
	}
] as const;

type Metric = (typeof METRICS)[number]['key'];
export type MetricValues = Record<Metric, number>;
export type Coverage = {
	imported: number;
	sensor_estimate: number;
	duration_estimate: number;
	missing: number;
};
type StressDay = { date: string; tss: number | null; scored: number; total: number };
export type Analysis = Record<Metric, number[]> & {
	start: string;
	end: string;
	dates: string[];
	current: MetricValues;
	stress_days: StressDay[];
	coverage: Coverage;
	recent_scored: number;
	recent_total: number;
};

export const FORM_ZONES = [
	{
		from: -Infinity,
		color: 'var(--zone-very-fatigued)',
		label: m.analysis_zone_very_fatigued,
		detail: m.analysis_zone_very_fatigued_detail
	},
	{
		from: -20,
		color: 'var(--zone-accumulated)',
		label: m.analysis_zone_accumulated,
		detail: m.analysis_zone_accumulated_detail
	},
	{
		from: -10,
		color: 'var(--zone-balanced)',
		label: m.analysis_zone_balanced,
		detail: m.analysis_zone_balanced_detail
	},
	{
		from: 5,
		color: 'var(--zone-fresh)',
		label: m.analysis_zone_fresh,
		detail: m.analysis_zone_fresh_detail
	},
	{
		from: 20,
		color: 'var(--zone-very-fresh)',
		label: m.analysis_zone_very_fresh,
		detail: m.analysis_zone_very_fresh_detail
	}
] as const;

export function formZone(value: number) {
	return FORM_ZONES.findLast((zone) => value >= zone.from) ?? FORM_ZONES[0];
}

export const DAY = 86_400_000;
export const MIN_RECENT_SCORES = 3;

export function isoDay(time: number): string {
	return new Date(time).toISOString().slice(0, 10);
}

export function calendarDate(time: number): string {
	switch (prefs.dateFormat) {
		case 'iso':
			return isoDay(time);
		case 'eu':
			return new Intl.DateTimeFormat('de-DE', { timeZone: 'UTC' }).format(time);
		case 'us':
			return new Intl.DateTimeFormat('en-US', { timeZone: 'UTC' }).format(time);
		default:
			return new Intl.DateTimeFormat(intlLocale(), {
				timeZone: 'UTC',
				dateStyle: 'medium'
			}).format(time);
	}
}

export function metricNumber(value: number, signed = false): string {
	return new Intl.NumberFormat(intlLocale(), {
		minimumFractionDigits: 1,
		maximumFractionDigits: 1,
		signDisplay: signed ? 'exceptZero' : 'negative'
	}).format(value);
}

export function changeFrom(data: Analysis, metric: Metric, ago: number): number | null {
	const index = data.dates.length - 1 - ago;
	const previous = data[metric][index];
	if (previous === undefined || data.dates[index] !== isoDay(Date.parse(data.end) - ago * DAY))
		return null;
	return data.current[metric] - previous;
}

export function describeTraining(data: Analysis) {
	const { ctl, atl, tsb } = data.current;
	const zone = formZone(tsb);
	const peakFitness = Math.max(0, ...data.ctl.slice(-90));
	const peakFatigue = Math.max(0, ...data.atl.slice(-42));
	const fitnessChange = changeFrom(data, 'ctl', 30) ?? changeFrom(data, 'ctl', 7);
	const fatigueChange = changeFrom(data, 'atl', 7);
	const formChange = changeFrom(data, 'tsb', 7);
	// Interpret load relative to the rider's own recent history. The small
	// floor keeps rounding noise from changing the status near zero.
	const tolerance = Math.max(0.5, Math.abs(ctl - (fitnessChange ?? 0)) * 0.08);
	const lowFitness = ctl <= 0.5 || ctl < peakFitness * 0.65;
	const building = fitnessChange !== null && fitnessChange >= tolerance;
	const falling = fitnessChange !== null && fitnessChange <= -tolerance;
	const lowFatigue = atl <= 0.5 || atl < Math.max(peakFatigue, ctl) * 0.25;
	const elevatedFatigue = tsb < FORM_ZONES[2].from;
	const metrics = {
		ctl: lowFitness
			? { label: m.analysis_fitness_low(), detail: m.analysis_fitness_low_detail() }
			: building
				? { label: m.analysis_fitness_rising(), detail: m.analysis_fitness_rising_detail() }
				: falling
					? { label: m.analysis_fitness_falling(), detail: m.analysis_fitness_falling_detail() }
					: fitnessChange === null
						? { label: m.analysis_fitness_early(), detail: m.analysis_fitness_early_detail() }
						: { label: m.analysis_fitness_steady(), detail: m.analysis_fitness_steady_detail() },
		atl: elevatedFatigue
			? { label: m.analysis_fatigue_high(), detail: m.analysis_fatigue_high_detail() }
			: lowFatigue
				? { label: m.analysis_fatigue_low(), detail: m.analysis_fatigue_low_detail() }
				: { label: m.analysis_fatigue_typical(), detail: m.analysis_fatigue_typical_detail() },
		tsb: {
			label: zone.label(),
			detail:
				tsb > 0
					? m.analysis_form_positive()
					: tsb < 0
						? m.analysis_form_negative()
						: m.analysis_form_neutral()
		}
	};
	const interpret = (
		tone: 'neutral' | 'fresh' | 'load',
		title: string,
		detail: string,
		insight = ''
	) => ({
		tone,
		title,
		detail,
		insight:
			data.recent_scored >= MIN_RECENT_SCORES && data.recent_scored / data.recent_total >= 0.75
				? insight
				: ''
	});

	let status;
	if (!data.dates.length) {
		status = interpret(
			'neutral',
			m.analysis_status_insufficient(),
			data.coverage.missing
				? m.analysis_status_insufficient_detail()
				: m.analysis_status_no_activities()
		);
	} else if (data.recent_scored === 0) {
		status = interpret(
			'neutral',
			data.recent_total ? m.analysis_status_missing_recent() : m.analysis_status_inactive(),
			m.analysis_status_no_recent_detail({ zone: zone.label() })
		);
	} else if (
		elevatedFatigue &&
		fatigueChange !== null &&
		fatigueChange < -0.5 &&
		(formChange ?? 0) > 0
	) {
		status = interpret(
			'load',
			m.analysis_status_recovering(),
			m.analysis_status_recovering_detail(),
			m.analysis_insight_recover()
		);
	} else if (zone === FORM_ZONES[0]) {
		status = interpret(
			'load',
			m.analysis_status_high(),
			m.analysis_status_high_detail(),
			m.analysis_insight_recover()
		);
	} else if (zone === FORM_ZONES[1]) {
		status = interpret(
			'load',
			m.analysis_status_fatigued(),
			m.analysis_status_fatigued_detail(),
			m.analysis_insight_recover()
		);
	} else if (zone === FORM_ZONES[3] && lowFitness) {
		status = interpret(
			'fresh',
			m.analysis_status_fresh_low(),
			m.analysis_status_fresh_low_detail(),
			m.analysis_insight_build()
		);
	} else if (falling) {
		status = interpret(
			'neutral',
			m.analysis_status_falling(),
			m.analysis_status_falling_detail({ zone: zone.label() }),
			m.analysis_insight_build()
		);
	} else if (zone === FORM_ZONES[3] || zone === FORM_ZONES[4]) {
		status = interpret(
			'fresh',
			m.analysis_status_fresh(),
			m.analysis_status_fresh_detail(),
			m.analysis_insight_fresh()
		);
	} else if (data.recent_scored < MIN_RECENT_SCORES && fitnessChange === null) {
		status = interpret(
			'neutral',
			m.analysis_status_early(),
			m.analysis_status_early_detail({ zone: zone.label() })
		);
	} else if (building) {
		status = interpret(
			'fresh',
			m.analysis_status_building(),
			m.analysis_status_building_detail(),
			m.analysis_insight_build()
		);
	} else {
		status = interpret(
			'neutral',
			m.analysis_status_balanced(),
			m.analysis_status_balanced_detail()
		);
	}
	return { ...status, metrics, formDetail: zone.detail() };
}

export type Grouping = 'daily' | 'weekly' | 'monthly';

export type StressPeriod = {
	date: number;
	from: number;
	to: number;
	tss: number | null;
	total: number;
	scored: number;
};

function periodStart(time: number, group: Grouping): number {
	const date = new Date(time);
	if (group === 'monthly') return time - (date.getUTCDate() - 1) * DAY;
	if (group === 'weekly') return time - ((date.getUTCDay() + 6) % 7) * DAY;
	return time;
}

function nextPeriod(time: number, group: Grouping): number {
	if (group === 'monthly') {
		const date = new Date(time);
		return date.setUTCMonth(date.getUTCMonth() + 1);
	}
	return time + (group === 'weekly' ? 7 : 1) * DAY;
}

export function aggregateStress(
	data: Pick<Analysis, 'start' | 'end' | 'stress_days'>,
	group: Grouping
): StressPeriod[] {
	const start = Date.parse(data.start);
	const end = Date.parse(data.end);
	const periods = new Map<number, StressPeriod>();
	for (let date = periodStart(start, group); date <= end; date = nextPeriod(date, group)) {
		periods.set(date, {
			date,
			from: Math.max(date, start),
			to: Math.min(end, nextPeriod(date, group) - DAY),
			tss: null,
			total: 0,
			scored: 0
		});
	}
	for (const day of data.stress_days) {
		const period = periods.get(periodStart(Date.parse(day.date), group));
		if (!period) continue;
		period.total += day.total;
		period.scored += day.scored;
		if (day.tss !== null) period.tss = (period.tss ?? 0) + day.tss;
	}
	return [...periods.values()];
}

export function yScale(min: number, max: number) {
	const step = Math.max((max - min) / 4, 1);
	const power = 10 ** Math.floor(Math.log10(step));
	const interval = ([1, 2, 2.5, 5, 10].find((factor) => factor * power >= step) ?? 10) * power;
	const bottom = Math.floor(min / interval);
	const top = Math.max(bottom + 1, Math.ceil(max / interval));
	return {
		min: bottom * interval,
		max: top * interval,
		ticks: Array.from({ length: top - bottom + 1 }, (_, i) => (bottom + i) * interval)
	};
}
