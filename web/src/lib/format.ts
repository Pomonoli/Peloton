import { getLocale } from '#lib/paraglide/runtime.js';
import { prefs } from '#lib/prefs.svelte.js';

// UI-side formatters. The Go server reports SI units (meters, seconds, m/s);
// this module converts to human-friendly strings, honouring the user's
// preferences (unit system and date format) from #lib/prefs. Reading
// `prefs.*` here makes every caller reactive to preference changes.

const DASH = '—';

const M_PER_MILE = 1609.344;
const FT_PER_M = 3.28084;

// Map Paraglide locales to regional formatting conventions.
const intlLocales: Partial<Record<string, string>> = { en: 'en-US', de: 'de-DE', 'nl-BE': 'nl-BE' };

export function intlLocale(): string {
	return intlLocales[getLocale()] ?? 'en-US';
}

function num(v: number, frac = 2): string {
	return new Intl.NumberFormat(intlLocale(), {
		minimumFractionDigits: frac,
		maximumFractionDigits: frac
	}).format(v);
}

export function formatDistance(m: number | null | undefined): string {
	if (m == null) return DASH;
	if (prefs.units === 'imperial') {
		if (m < M_PER_MILE / 10) return `${Math.round(m * FT_PER_M)} ft`;
		return `${num(m / M_PER_MILE)} mi`;
	}
	return formatMetricDistance(m);
}

export function formatMetricDistance(m: number): string {
	if (!(m > 0) || !Number.isFinite(m)) return Number.isFinite(m) && m === 0 ? '0 m' : DASH;
	if (m < 1000) return `${num(m, 0)} m`;
	return `${num(m / 1000)} km`;
}

export function formatSpeedKmh(kmh: number | null | undefined): string {
	if (kmh == null || !Number.isFinite(kmh) || kmh <= 0) return DASH;
	return `${num(kmh, 1)} km/h`;
}

export function formatTimeAndSpeed(time: string, speedKmh: number | null | undefined): string {
	const speed = formatSpeedKmh(speedKmh);
	return speed === DASH ? time : `${time} · ${speed}`;
}

export function segmentSpeedKmh(
	distanceM: number | null | undefined,
	elapsedS: number | null | undefined
): number | null {
	if (
		distanceM == null ||
		elapsedS == null ||
		!Number.isFinite(distanceM) ||
		!Number.isFinite(elapsedS) ||
		distanceM <= 0 ||
		elapsedS <= 0
	)
		return null;
	const speed = (distanceM / elapsedS) * 3.6;
	return Number.isFinite(speed) && speed > 0 ? speed : null;
}

export function formatDistanceEffort(
	distanceM: number | null | undefined,
	elapsedS: number | null | undefined
): string {
	if (elapsedS == null || !Number.isFinite(elapsedS) || elapsedS <= 0) return DASH;
	const speedKmh = segmentSpeedKmh(distanceM, elapsedS);
	return formatTimeAndSpeed(formatDuration(elapsedS), speedKmh);
}

export function formatDuration(seconds: number | null | undefined): string {
	if (seconds == null) return DASH;
	const total = Math.round(seconds);
	const h = Math.floor(total / 3600);
	const m = Math.floor((total % 3600) / 60);
	const s = total % 60;
	if (h > 0) return `${h}:${pad(m)}:${pad(s)}`;
	return `${m}:${pad(s)}`;
}

export function formatPace(metersPerSecond: number | null | undefined): string {
	if (metersPerSecond == null || metersPerSecond <= 0) return DASH;
	if (prefs.units === 'imperial') {
		const minPerMile = M_PER_MILE / metersPerSecond / 60;
		const whole = Math.floor(minPerMile);
		const rem = Math.round((minPerMile - whole) * 60);
		return `${whole}:${pad(rem)} /mi`;
	}
	const minPerKm = 1000 / metersPerSecond / 60;
	const whole = Math.floor(minPerKm);
	const rem = Math.round((minPerKm - whole) * 60);
	return `${whole}:${pad(rem)} /km`;
}

export function formatElevation(m: number | null | undefined): string {
	if (m == null) return DASH;
	if (prefs.units === 'imperial') return `${Math.round(m * FT_PER_M)} ft`;
	return `${Math.round(m)} m`;
}

export function formatSpeed(metersPerSecond: number | null | undefined): string {
	if (metersPerSecond == null || !Number.isFinite(metersPerSecond) || metersPerSecond < 0)
		return DASH;
	if (prefs.units === 'imperial') {
		return `${num((metersPerSecond * 3600) / M_PER_MILE)} mph`;
	}
	return `${num((metersPerSecond * 3600) / 1000)} km/h`;
}

export function formatTemp(celsius: number | null | undefined): string {
	if (celsius == null) return DASH;
	if (prefs.units === 'imperial') return `${Math.round((celsius * 9) / 5 + 32)}°F`;
	return `${Math.round(celsius)}°C`;
}

export function formatHeartRate(bpm: number | null | undefined): string {
	if (bpm == null) return DASH;
	return `${bpm} bpm`;
}

export function formatPower(w: number | null | undefined): string {
	if (w == null) return DASH;
	return `${w} W`;
}

// ---- Date / time, honouring the user's explicit format choices ----

function formatDatePart(d: Date): string {
	switch (prefs.dateFormat) {
		case 'iso':
			return d.toLocaleDateString('sv-SE'); // YYYY-MM-DD
		case 'us':
			return d.toLocaleDateString('en-US'); // M/D/YYYY
		case 'eu':
			return d.toLocaleDateString('de-DE'); // DD.MM.YYYY
		default:
			return d.toLocaleDateString(intlLocale(), { dateStyle: 'medium' });
	}
}

export function formatClockTime(
	value: string | number | Date,
	{ timeZone, seconds = false }: { timeZone?: string; seconds?: boolean } = {}
): string {
	const date = value instanceof Date ? value : new Date(value);
	return date.toLocaleTimeString(intlLocale(), {
		hour: '2-digit',
		minute: '2-digit',
		second: seconds ? '2-digit' : undefined,
		hourCycle: 'h23',
		numberingSystem: 'latn',
		timeZone
	});
}

export function formatDate(
	iso: string,
	{ seconds = false, numericDate = false }: { seconds?: boolean; numericDate?: boolean } = {}
): string {
	const date = new Date(iso);
	const day = numericDate ? date.toLocaleDateString(intlLocale()) : formatDatePart(date);
	return `${day} ${formatClockTime(date, { seconds })}`;
}

export function formatDateOnly(iso: string): string {
	return formatDatePart(new Date(iso));
}

export function formatRelativeDate(iso: string): string {
	const d = new Date(iso);
	const now = new Date();
	const diffSec = (now.getTime() - d.getTime()) / 1000;
	const rtf = new Intl.RelativeTimeFormat(intlLocale(), { numeric: 'auto' });
	if (diffSec < 60) return rtf.format(-Math.round(diffSec), 'second');
	if (diffSec < 3600) return rtf.format(-Math.round(diffSec / 60), 'minute');
	if (diffSec < 86400) return rtf.format(-Math.round(diffSec / 3600), 'hour');
	if (diffSec < 86400 * 7) return rtf.format(-Math.round(diffSec / 86400), 'day');
	return formatDatePart(d);
}

function pad(n: number): string {
	return n.toString().padStart(2, '0');
}
