import { beforeEach, describe, expect, it } from 'vitest';
import { prefs, setPrefs } from './prefs.svelte';

describe('setPrefs', () => {
	beforeEach(() => {
		setPrefs({ dateFormat: '', units: 'metric' });
	});

	it('starts at defaults', () => {
		expect({ ...prefs }).toEqual({ dateFormat: '', units: 'metric' });
	});

	it('applies provided fields only', () => {
		setPrefs({ dateFormat: 'iso' });
		expect(prefs.dateFormat).toBe('iso');
		setPrefs({ units: 'imperial' });
		expect(prefs).toMatchObject({ dateFormat: 'iso', units: 'imperial' });
	});

	it('falls back to metric for unknown units', () => {
		setPrefs({ units: 'imperial' });
		setPrefs({ units: 'furlongs' });
		expect(prefs.units).toBe('metric');
	});

	it('leaves fields unchanged when undefined', () => {
		setPrefs({ dateFormat: 'us', units: 'imperial' });
		setPrefs({});
		expect(prefs).toMatchObject({ dateFormat: 'us', units: 'imperial' });
	});
});
