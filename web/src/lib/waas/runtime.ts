import type { Planner, RideOptions } from './types.js';

interface Runtime {
	createPlanner(spines: unknown): Planner;
	createRideOptions(planner: Planner): RideOptions;
}

let loading: Promise<Runtime> | undefined;

// Literal imports let Vite bundle and fingerprint the entire engine dependency
// graph with the UI. Fixed public URLs can retain an older engine after an
// upgrade, leaving new summary fields undefined. Loading remains lazy.
export function runtime(): Promise<Runtime> {
	if (!loading) {
		loading = Promise.all([
			import('../../../static/waas-engine/engine.js'),
			import('../../../static/waas-engine/ride-options.js')
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
