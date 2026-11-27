import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const locale = vi.hoisted(() => ({ current: 'de' }));
vi.mock('#lib/paraglide/runtime.js', () => ({ getLocale: () => locale.current }));

import {
	formatClockTime,
	formatDate,
	formatDateOnly,
	formatDistance,
	formatDistanceEffort,
	formatDuration,
	formatElevation,
	formatHeartRate,
	formatPace,
	formatPower,
	formatRelativeDate,
	formatSpeed,
	formatSpeedKmh,
	formatTemp,
	formatTimeAndSpeed
} from './format';
import { setPrefs } from './prefs.svelte';

const norm = (s: string) => s.replace(/[\u00a0\u202f]/g, ' ');

// Dates are rendered in local time; pin it.
process.env.TZ = 'UTC';

beforeEach(() => {
	locale.current = 'de';
	setPrefs({ dateFormat: '', units: 'metric' });
});

describe('null handling', () => {
	it('renders a dash for missing values', () => {
		for (const f of [
			formatDistance,
			formatDuration,
			formatPace,
			formatElevation,
			formatSpeed,
			formatSpeedKmh,
			formatTemp,
			formatHeartRate,
			formatPower
		]) {
			expect(f(null)).toBe('—');
			expect(f(undefined)).toBe('—');
		}
	});
});

describe('formatDistance', () => {
	it('uses metres below 1 km and rounds', () => {
		expect(formatDistance(999.4)).toBe('999 m');
	});

	it('uses km with locale decimals', () => {
		expect(formatDistance(12345)).toBe('12,35 km');
		locale.current = 'en';
		expect(formatDistance(12345)).toBe('12.35 km');
	});

	it('falls back to English for unknown locales', () => {
		locale.current = 'fr';
		expect(formatDistance(1500)).toBe('1.50 km');
	});

	it('uses feet below a tenth of a mile, miles above', () => {
		setPrefs({ units: 'imperial' });
		expect(formatDistance(100)).toBe('328 ft');
		locale.current = 'en';
		expect(formatDistance(1609.344 * 2.5)).toBe('2.50 mi');
	});

	it('rounds large totals when compact formatting is requested', () => {
		locale.current = 'en';
		const options = { compact: true };
		expect(formatDistance(861770, options)).toBe('861.77 km');
		expect(formatDistance(2977810, options)).toBe('2,977.8 km');
		expect(formatDistance(12345678, options)).toBe('12,346 km');
		expect(formatDistance(2977810)).toBe('2,977.81 km');

		setPrefs({ units: 'imperial' });
		expect(formatDistance(1609.344 * 2977.81, options)).toBe('2,977.8 mi');
	});
});

describe('formatDuration', () => {
	it('formats m:ss under an hour', () => {
		expect(formatDuration(0)).toBe('0:00');
		expect(formatDuration(65)).toBe('1:05');
	});

	it('formats h:mm:ss and rounds seconds', () => {
		expect(formatDuration(3599.6)).toBe('1:00:00');
		expect(formatDuration(3723)).toBe('1:02:03');
	});
});

describe('formatPace', () => {
	it('rejects non-positive speeds', () => {
		expect(formatPace(0)).toBe('—');
		expect(formatPace(-1)).toBe('—');
	});

	it('formats min/km', () => {
		expect(formatPace(1000 / 300)).toBe('5:00 /km');
		expect(formatPace(1000 / 330)).toBe('5:30 /km');
	});

	it('formats min/mi', () => {
		setPrefs({ units: 'imperial' });
		expect(formatPace(1609.344 / 480)).toBe('8:00 /mi');
	});
});

describe('formatElevation / formatSpeed / formatTemp', () => {
	it('metric', () => {
		expect(formatElevation(123.6)).toBe('124 m');
		expect(formatSpeed(10)).toBe('36,00 km/h');
		expect(formatSpeed(0)).toBe('0,00 km/h');
		expect(formatSpeed(-1)).toBe('—');
		expect(formatTemp(21.5)).toBe('22°C');
	});

	it('imperial', () => {
		setPrefs({ units: 'imperial' });
		locale.current = 'en';
		expect(formatElevation(100)).toBe('328 ft');
		expect(formatSpeed(1609.344 / 3600)).toBe('1.00 mph');
		expect(formatTemp(100)).toBe('212°F');
	});

	it('heart rate and power pass through', () => {
		expect(formatHeartRate(142)).toBe('142 bpm');
		expect(formatPower(250)).toBe('250 W');
	});
});

describe('date formatting', () => {
	const iso = '2024-03-05T14:07:00Z';

	it('explicit date formats', () => {
		setPrefs({ dateFormat: 'iso' });
		expect(formatDateOnly(iso)).toBe('2024-03-05');
		setPrefs({ dateFormat: 'us' });
		expect(formatDateOnly(iso)).toBe('3/5/2024');
		setPrefs({ dateFormat: 'eu' });
		expect(formatDateOnly(iso)).toBe('5.3.2024');
	});

	it('locale default date format', () => {
		expect(formatDateOnly(iso)).toBe('05.03.2024');
		locale.current = 'en';
		expect(formatDateOnly(iso)).toBe('Mar 5, 2024');
	});

	it.each(['en', 'de', 'nl-BE'])('uses the 24-hour clock in %s', (language) => {
		locale.current = language;
		setPrefs({ dateFormat: 'iso' });
		expect(formatDate(iso)).toBe('2024-03-05 14:07');
		expect(formatClockTime('2024-03-05T00:00:00Z')).toBe('00:00');
		expect(formatClockTime('2024-03-05T08:05:00Z')).toBe('08:05');
		expect(formatClockTime('2024-03-05T12:00:00Z')).toBe('12:00');
		expect(formatClockTime('2024-03-05T17:24:03Z', { seconds: true })).toBe('17:24:03');
	});

	it('honors an explicit timezone including summer and winter offsets', () => {
		const options = { timeZone: 'Europe/Brussels' };
		expect(formatClockTime('2024-07-04T22:00:00Z', options)).toBe('00:00');
		expect(formatClockTime('2024-01-04T22:00:00Z', options)).toBe('23:00');
		locale.current = 'en';
		expect(formatDate('2024-03-05T17:24:03Z', { numericDate: true, seconds: true })).toBe(
			'3/5/2024 17:24:03'
		);
	});
});

describe('formatRelativeDate', () => {
	beforeEach(() => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date('2024-03-10T12:00:00Z'));
		locale.current = 'en';
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it('picks the largest fitting unit', () => {
		expect(formatRelativeDate('2024-03-10T11:59:30Z')).toBe('30 seconds ago');
		expect(formatRelativeDate('2024-03-10T11:55:00Z')).toBe('5 minutes ago');
		expect(formatRelativeDate('2024-03-10T09:00:00Z')).toBe('3 hours ago');
		expect(formatRelativeDate('2024-03-09T12:00:00Z')).toBe('yesterday');
		expect(formatRelativeDate('2024-03-07T12:00:00Z')).toBe('3 days ago');
	});

	it('falls back to an absolute date after a week', () => {
		setPrefs({ dateFormat: 'iso' });
		expect(formatRelativeDate('2024-03-01T12:00:00Z')).toBe('2024-03-01');
	});

	it('uses now for zero seconds', () => {
		expect(formatRelativeDate('2024-03-10T12:00:00Z')).toBe('now');
	});
});

it('formats segment speed with one decimal in km/h regardless of unit preferences', () => {
	locale.current = 'en';
	setPrefs({ units: 'imperial' });
	expect(formatSpeedKmh(32.74)).toBe('32.7 km/h');
	expect(formatSpeedKmh(32)).toBe('32.0 km/h');
	expect(formatSpeedKmh(0)).toBe('—');
	expect(formatSpeedKmh(Number.NaN)).toBe('—');
	expect(formatSpeedKmh(Infinity)).toBe('—');
	expect(formatSpeedKmh(null)).toBe('—');

	locale.current = 'nl-BE';
	expect(norm(formatSpeedKmh(32.74))).toBe('32,7 km/h');
});

describe('formatDistanceEffort', () => {
	it('uses matched distance and unrounded elapsed time with metric speed', () => {
		locale.current = 'en';
		setPrefs({ units: 'imperial' });
		expect(formatDistanceEffort(1090, 120)).toBe('2:00 · 32.7 km/h');
		expect(formatDistanceEffort(1090, 120.49)).toBe('2:00 · 32.6 km/h');

		locale.current = 'nl-BE';
		expect(formatDistanceEffort(1090, 120)).toBe('2:00 · 32,7 km/h');
	});

	it.each([0, -1, null, undefined, NaN, Infinity])('handles invalid input (%s)', (invalid) => {
		expect(formatDistanceEffort(1090, invalid)).toBe('—');
		expect(formatDistanceEffort(invalid, 120)).toBe('2:00');
	});
});

it('adds provided segment speed while preserving the formatted time', () => {
	locale.current = 'en';
	expect(formatTimeAndSpeed('120.49 sec', 32.567)).toBe('120.49 sec · 32.6 km/h');
	expect(formatTimeAndSpeed('120.49 sec', null)).toBe('120.49 sec');
});
