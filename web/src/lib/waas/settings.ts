import type { Options, Profile, Departure, Direction } from './types.js';

export const defaults: Options = {
	profile: 'climb',
	start: 'sn',
	length: 70,
	tolerance: 0.5,
	direction: 'auto',
	places: [],
	goal: 'max',
	minGain: 400,
	maxGain: 500,
	laps: 3,
	surface: 'smooth',
	zones: {}
};

const profiles: Profile[] = ['climb', 'cobbles', 'flow', 'gravel'];
const departures: Departure[] = ['sn', 'wm', 'ti', 'te'];
const directions: Direction[] = ['auto', 'cw', 'ccw'];
const zoneIds = ['cuesta', 'sombeke', 'tielrode', 'temse', 'oostberg', 'rupelmonde'];
export const placeIds = [
	'kerk-temse',
	'dijk-tielrode',
	'kerk-elversele',
	'markt-lokeren',
	'markt-beveren',
	'veer-linkeroever',
	'veer-kruibeke',
	'kerk-stekene',
	'markt-sint-niklaas',
	'centrum-waasmunster',
	'centrum-rupelmonde',
	'kerk-bazel'
];

function bounded(value: string | null, fallback: number, min: number, max: number): number {
	if (value === null || value.trim() === '') return fallback;
	const number = Number(value);
	return Number.isFinite(number) ? Math.min(max, Math.max(min, number)) : fallback;
}

export function readOptions(params: URLSearchParams): Options {
	const profile = params.get('profile') as Profile;
	const start = params.get('start') as Departure;
	const direction = params.get('direction') as Direction;
	const goal = params.get('goal');
	const zones: Options['zones'] = {};
	for (const id of zoneIds) {
		const value = params.get('zone_' + id);
		if (value === 'include' || value === 'avoid') zones[id] = value;
	}
	return {
		...defaults,
		profile: profiles.includes(profile) ? profile : defaults.profile,
		start: departures.includes(start) ? start : defaults.start,
		direction: directions.includes(direction) ? direction : defaults.direction,
		length: Math.round(bounded(params.get('km'), defaults.length, 15, 200) * 2) / 2,
		tolerance: bounded(params.get('margin'), defaults.tolerance, 0.5, 10),
		goal: goal === 'varied' || goal === 'range' ? goal : 'max',
		minGain: bounded(params.get('min'), defaults.minGain, 0, 5000),
		maxGain: bounded(params.get('max'), defaults.maxGain, 0, 5000),
		laps: Math.round(bounded(params.get('laps'), defaults.laps, 1, 3)),
		surface: params.get('surface') === 'any' ? 'any' : 'smooth',
		places: [...new Set(params.getAll('via').filter((id) => placeIds.includes(id)))],
		zones
	};
}

export function optionsQuery(options: Options): string {
	const params = new URLSearchParams({
		profile: options.profile,
		start: options.start,
		km: String(options.length),
		margin: String(options.tolerance),
		direction: options.direction,
		goal: options.goal,
		min: String(options.minGain),
		max: String(options.maxGain),
		laps: String(options.laps),
		surface: options.surface
	});
	for (const id of options.places) params.append('via', id);
	for (const [id, value] of Object.entries(options.zones))
		if (value !== 'free') params.set('zone_' + id, value);
	return params.toString();
}
