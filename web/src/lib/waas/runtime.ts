import type { Planner, RideOptions } from './types.js';

interface Runtime {
	createPlanner(spines: unknown): Planner;
	createRideOptions(planner: Planner): RideOptions;
}

let loading: Promise<Runtime> | undefined;

// The original planner is maintained as local ESM assets, with a typed boundary
// around its catalog format. Only the planning page loads this runtime.
export function runtime(): Promise<Runtime> {
	if (!loading) {
		const base = '/waas-engine/';
		loading = Promise.all([
			import(/* @vite-ignore */ `${base}engine.js`),
			import(/* @vite-ignore */ `${base}ride-options.js`)
		]).then(([engine, options]) => {
			if (
				typeof engine.createPlanner !== 'function' ||
				typeof options.createRideOptions !== 'function'
			) {
				throw new Error('The Waasland planner could not be loaded. Reload the page and try again.');
			}
			return {
				createPlanner: engine.createPlanner,
				createRideOptions: options.createRideOptions
			} as Runtime;
		});
		void loading.catch(() => {
			loading = undefined;
		});
	}
	return loading;
}
