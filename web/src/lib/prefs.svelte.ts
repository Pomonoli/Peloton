// Shared, reactive user display preferences. Seeded from the logged-in user
// (in the root layout) and updated immediately on change, so every formatter
// that reads it re-renders site-wide. Mirrors the server-side, Connect-exposed
// User preferences (units + date format).

export type DateFmt = '' | 'iso' | 'us' | 'eu';
export type Units = 'metric' | 'imperial';

export const prefs = $state<{ dateFormat: DateFmt; units: Units }>({
	dateFormat: '',
	units: 'metric'
});

export function setPrefs(p: { dateFormat?: string; units?: string }) {
	if (p.dateFormat !== undefined) prefs.dateFormat = (p.dateFormat || '') as DateFmt;
	if (p.units !== undefined)
		prefs.units = (p.units === 'imperial' ? 'imperial' : 'metric') as Units;
}
