import { runtime } from './runtime.js';
import type { Options, Planner, Ride, Routes } from './types.js';

const defaultOrigin = '/api/route-planner/waasland/data/';
const origin = (import.meta.env.VITE_WAAS_DATA_URL as string | undefined) || defaultOrigin;
const requests = new Map<string, Promise<unknown>>();

export function dataUrl(file: string): string {
	return `${origin.replace(/\/$/, '')}/${file}`;
}

// Dataset assignments are parsed as JSON. Remote JavaScript is never executed
// inside the signed-in application.
export function parseDataset(source: string, key: string): unknown {
	const marker = 'window.' + key;
	const index = source.lastIndexOf(marker);
	if (index < 0) throw new Error('The regional route dataset is invalid.');
	const value = source.slice(index + marker.length).trim();
	if (!value.startsWith('=')) throw new Error('The regional route dataset is invalid.');
	return JSON.parse(value.slice(1).trim().replace(/;\s*$/, ''));
}

async function load(file: string, key?: string): Promise<unknown> {
	if (!requests.has(file)) {
		const pending = (async () => {
			const response = await fetch(dataUrl(file), {
				credentials: 'omit',
				signal: AbortSignal.timeout(30000)
			});
			if (!response.ok)
				throw new Error('Regional route data is temporarily unavailable. Please try again.');
			if (key) return parseDataset(await response.text(), key);
			if (!response.body) throw new Error('The regional route dataset is empty.');
			return new Response(response.body.pipeThrough(new DecompressionStream('gzip'))).json();
		})();
		requests.set(file, pending);
		void pending.catch(() => {
			if (requests.get(file) === pending) requests.delete(file);
		});
	}
	return requests.get(file)!;
}

interface Dataset {
	routes: Routes;
	circuits: string[][];
	bounds: [[number, number], [number, number]];
}
let ready: Promise<{ planner: Planner; core: Dataset }> | undefined;
export function initialize() {
	if (!ready) {
		ready = Promise.all([
			load('route-spines.js', 'WAAS_SPINES'),
			load('terrain-data.js', 'WAAS_DATA'),
			runtime()
		]).then(([spines, data, engine]) => ({
			planner: engine.createPlanner(spines),
			core: data as Dataset
		}));
		void ready.catch(() => {
			ready = undefined;
		});
	}
	return ready;
}

export async function calculate(
	options: Options,
	signal: AbortSignal,
	progress: (message: string) => void
): Promise<{ rides: Ride[]; warnings: string[] }> {
	const { planner, core } = await initialize();
	const rideOptions = (await runtime()).createRideOptions(planner);
	const key = `${options.profile}-${options.start}`;
	const regional = options.length - options.tolerance <= 125;
	const extended = options.length + options.tolerance >= 120;
	const longFlow = options.profile === 'flow' && extended;
	const packed = (file: string) => load(file).catch(() => null);
	const script = (file: string, name: string) => load(file, name).catch(() => null);
	if (longFlow && options.length === 200) {
		const rounds = (await load('schelde-kanaal.js', 'WAAS_SCHELDE_ROUNDS')) as Record<
			string,
			unknown
		>;
		const plans = planner.findRoutes(
			options,
			{},
			[],
			planner.decodePriorityRoutes(rounds[options.start])
		);
		const custom = await rideOptions.customize(plans, options, signal, progress);
		if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
		return { rides: custom.map((plan) => planner.assemble(plan)), warnings: [] };
	}
	const includeZones =
		options.profile === 'climb' &&
		Object.values(options.zones).filter((v) => v === 'include').length >= 2 &&
		!Object.values(options.zones).includes('avoid');
	const datasets = await Promise.all([
		options.profile === 'climb'
			? core
			: load(`profile-${options.profile}.js`, `WAAS_PROFILES.${options.profile}`),
		regional ? script(`circuits-${key}.js`, `WAAS_CIRCUITS[${JSON.stringify(key)}]`) : {},
		regional && includeZones
			? script(`climb-zones-${options.start}.js`, `WAAS_ZONE_ROUTES.${options.start}`)
			: {},
		regional && ['climb', 'cobbles'].includes(options.profile)
			? script(`priority-${key}.js`, `WAAS_PRIORITY_ROUTES[${JSON.stringify(key)}]`)
			: null,
		regional && options.profile === 'climb' && options.goal === 'max'
			? packed(`max-climbs-${options.start}.bin`)
			: null,
		regional ? packed(`focus-${key}.bin`) : null,
		extended && !longFlow ? packed(`extended-${key}.bin`) : null,
		longFlow ? script('long-flow.js', 'WAAS_LONG_FLOW') : null,
		longFlow && options.length >= 185 ? script('schelde-kanaal.js', 'WAAS_SCHELDE_ROUNDS') : null,
		regional &&
		options.profile === 'climb' &&
		options.goal === 'max' &&
		options.length + options.tolerance >= 114.5 &&
		options.laps >= 2
			? script(`long-climbs-${options.start}.js`, `WAAS_LONG_CLIMBS.${options.start}`)
			: null
	]);
	if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
	const [base, circuits, zones, priority, maximum, focus, longer, flow, schelde, legacy] = datasets;
	const unpack = (value: unknown) => (value ? planner.decodePriorityRoutes(value) : {});
	const dataset = base as Dataset;
	const byStart = (value: unknown) => (value as Record<string, unknown> | null)?.[options.start];
	const standalone = {
		...(circuits as Routes | null),
		...(zones as Routes | null),
		...unpack(priority),
		...unpack(legacy),
		...planner.catalogOptions(maximum, options, 'max-climb'),
		...planner.catalogOptions(focus, options, 'focus'),
		...planner.catalogOptions(longer, options, 'extended'),
		...unpack(byStart(flow)),
		...unpack(byStart(schelde))
	};
	let plans = planner.findRoutes(options, dataset.routes, dataset.circuits, standalone);
	if (!plans.length)
		throw new Error(
			'No continuous ride was found. Try different places, climbing zones or a wider distance margin.'
		);
	plans = await rideOptions.customize(plans, options, signal, progress);
	if (signal.aborted) throw new DOMException('Aborted', 'AbortError');
	const warnings =
		(regional && !focus) || (extended && !longFlow && !longer)
			? [
					'Some regional alternatives are temporarily unavailable. The available route data was used.'
				]
			: [];
	return { rides: plans.map((plan) => planner.assemble(plan)), warnings };
}
