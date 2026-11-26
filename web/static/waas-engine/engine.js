import { writeGPX } from './gpx.js';
import { WaasRouteMetrics } from './route-metrics.js';
import { WaasRouteShape } from './route-shape.js';

export function createPlanner(spines) {
	const hubs = {
		sn: 'Sint-Niklaas · Grote Markt',
		wm: 'Waasmunster · centrum',
		ti: 'Tielrode · centrum',
		te: 'Temse · centrum'
	};
	const geometryByDataset = new WeakMap();
	const rideNames = {
		climb: 'Waasland climbing ride',
		cobbles: 'Cobbled ride',
		flow: 'Continuous ride',
		gravel: 'Gravel ride'
	};
	const climbZones = [
		{ id: 'cuesta', name: 'Waasmunster · Cuesta' },
		{ id: 'sombeke', name: 'Sombeke en Elversele' },
		{ id: 'tielrode', name: 'Tielrode en omgeving' },
		{ id: 'temse', name: 'Temse · Kattebrug en centrum' },
		{ id: 'oostberg', name: 'Oostberg' },
		{ id: 'rupelmonde', name: 'Steendorp en Rupelmonde' }
	];
	const zoneBits = Object.fromEntries(climbZones.map(({ id }, i) => [id, 1 << i]));
	const localZones = {
		wm: zoneBits.cuesta,
		so: zoneBits.sombeke,
		el: zoneBits.sombeke,
		ti: zoneBits.tielrode,
		te: zoneBits.temse
	};
	const circuits = [
		['sn', 'wm', 'so', 'el', 'ti', 'te'],
		['sn', 'wm', 'so', 'ti', 'te'],
		['sn', 'so', 'ti', 'te'],
		['sn', 'ti', 'te'],
		['sn', 'wm', 'so', 'el', 'ti'],
		['sn', 'wm', 'so', 'ti'],
		['sn', 'so', 'el', 'ti'],
		['sn', 'so', 'ti'],
		['sn', 'wm', 'so', 'el', 'ti', 'so'],
		['sn', 'wm', 'so'],
		['sn', 'wm'],
		['sn', 'ti'],
		['sn', 'te'],
		['wm', 'so', 'el', 'ti', 'te'],
		['wm', 'so'],
		['wm', 'vi'],
		['wm', 'so', 'ti', 'te'],
		['so', 'el', 'ti'],
		['so', 'ti'],
		['ti', 'te'],
		['wm'],
		['ti'],
		['te'],
		['sn']
	];
	const turnaround = [
		['wm', 'so', 'el', 'ti', 'te'],
		['wm', 'so', 'ti', 'te'],
		['wm', 'so', 'ti'],
		['sn', 'so', 'el', 'ti', 'te'],
		['sn', 'so', 'ti'],
		['sn', 'ti', 'te']
	];

	function makeItineraries(start, profileCircuits) {
		const options = new Map();
		function register(circuit) {
			for (const [index, hub] of circuit.entries()) {
				if (hub !== start) continue;
				const ordered = [...circuit.slice(index), ...circuit.slice(0, index)];
				const variants = [ordered];
				const west = ordered.indexOf('wm');
				if (!profileCircuits && west >= 0 && ordered.length > 1 && !ordered.includes('vi')) {
					const withBridge = [...ordered];
					withBridge.splice(west + 1, 0, 'vi', 'wm');
					variants.push(withBridge);
				}
				for (const variant of variants) {
					options.set(variant.join('/'), variant);
				}
			}
		}
		for (const round of profileCircuits || circuits) {
			register(round);
			register([...round].reverse());
		}
		for (const path of profileCircuits ? [] : turnaround)
			register([...path, ...path.slice(1, -1).reverse()]);
		return [...options.values()];
	}

	function compare(a, b) {
		for (let i = 0; i < a.length; i++) {
			if (a[i] !== b[i]) return a[i] - b[i];
		}
		return 0;
	}

	function climbZone([id, , , , to], points) {
		const longitude = points[to][1];
		if (id === 'Oostberg') return zoneBits.oostberg;
		if (longitude >= 4.259) return zoneBits.rupelmonde;
		if (longitude >= 4.238) return zoneBits.oostberg;
		if (longitude >= 4.191) return zoneBits.temse;
		if (longitude >= 4.156) return zoneBits.tielrode;
		if (longitude >= 4.113) return zoneBits.sombeke;
		return zoneBits.cuesta;
	}

	function routeClimbZone(climb, route) {
		if (!route.standalone) return climbZone(climb, route.points);
		const [lat, lon] = route.points[climb[4]];
		if (lon >= 4.259) {
			return lat >= 51.117 && lat <= 51.149 && lon <= 4.305 ? climbZone(climb, route.points) : 0;
		}
		if (lat < 51.105 || lat > 51.141 || lon < 4.05) return 0;
		return climbZone(climb, route.points);
	}

	function zoneSelection(zones) {
		let required = 0;
		let excluded = 0;
		for (const { id } of climbZones) {
			if (zones?.[id] === 'include') required |= zoneBits[id];
			if (zones?.[id] === 'avoid') excluded |= zoneBits[id];
		}
		return { required, excluded };
	}

	function describeChunk(id, route, smooth, excluded) {
		route = WaasRouteMetrics.normalize(route);
		if (smooth && route.rough) return null;
		const counts = new Map();
		let zones = 0;
		for (const climb of route.climbs) {
			const [street] = climb;
			counts.set(street, (counts.get(street) || 0) + 1);
			zones |= routeClimbZone(climb, route);
		}
		if (!zones && !route.standalone && route.from === route.to && route.gain >= 8)
			zones = localZones[route.from] || 0;
		return zones & excluded ? null : { ...route, id, zones, counts: [...counts] };
	}

	function catalog(routes, smooth, excluded, geometry) {
		const links = new Map();
		const loops = new Map();
		for (const [id, route] of Object.entries(routes)) {
			const chunk = describeChunk(id, route, smooth, excluded);
			if (!chunk || geometry.shape(chunk).uturn) continue;
			const collection = chunk.from === chunk.to ? loops : links;
			const key = chunk.from === chunk.to ? chunk.from : chunk.from + '-' + chunk.to;
			if (!collection.has(key)) collection.set(key, []);
			collection.get(key).push(chunk);
		}
		return { links, loops };
	}

	function stagesFor(itinerary, links, loops) {
		const stops = [];
		const visited = new Set();
		for (const [index, hub] of itinerary.entries()) {
			if (!visited.has(hub)) {
				visited.add(hub);
				for (const loop of loops.get(hub) || [])
					stops.push({ choices: [loop], optional: true, minMeters: 0 });
			}
			if (itinerary.length === 1) continue;
			const next = itinerary[(index + 1) % itinerary.length];
			const choices = links.get(hub + '-' + next);
			if (!choices) return null;
			stops.push({
				choices,
				optional: false,
				minMeters: Math.min(...choices.map((route) => route.meters))
			});
		}
		return stops;
	}

	function scoring(settings) {
		const { goal, minGain, maxGain, profile } = settings;
		const target = settings.length * 1000;
		const margin = settings.tolerance * 1000;
		const from = target - margin;
		const to = target + margin;
		const gainOff = (gain) => (goal === 'range' ? Math.max(minGain - gain, gain - maxGain, 0) : 0);
		const distanceOff = (meters) => Math.max(from - meters, meters - to, 0);
		const order = (state) => {
			const {
				gain,
				meters,
				distinct,
				repeated,
				repeatedLoops,
				rough,
				uniqueSurface,
				matchedSurface,
				separated,
				stopCost,
				reverse,
				repeat,
				spine
			} = state;
			const repeatedRoad = reverse * 4 + repeat;
			const distanceDelta = Math.abs(meters - target);
			if (profile === 'flow') {
				const corridorMeters = Object.values(spine).reduce((sum, meters) => sum + meters, 0);
				return [-corridorMeters, stopCost, -separated, repeatedRoad, distanceDelta];
			}
			if (profile === 'cobbles' || profile === 'gravel') {
				return [-matchedSurface, -uniqueSurface, repeatedRoad, distanceDelta];
			}
			if (goal === 'range') {
				return [
					gainOff(gain),
					repeated,
					repeatedRoad,
					-distinct,
					Math.abs(gain - (minGain + maxGain) / 2),
					distanceDelta,
					rough
				];
			}
			if (goal === 'max') return [-gain, repeated, repeatedRoad, -distinct, distanceDelta, rough];
			const variety = gain + 3 * distinct - 14 * repeated - 12 * repeatedLoops;
			return [-variety, repeatedRoad, distanceDelta, rough];
		};
		const score = (state) => [distanceOff(state.meters), ...order(state)];
		return { from, to, distanceOff, gainOff, order, score };
	}

	function advance(state, chunk, times, limit, laps, profile) {
		const meters = state.meters + times * chunk.meters;
		if (
			meters > limit ||
			state.last?.blocked.includes(chunk.id) ||
			(times > 1 && chunk.blocked.includes(chunk.id))
		)
			return null;
		if (profile !== 'climb') {
			const matchedSurface = state.matchedSurface + (chunk[profile] || 0);
			const surfaces = chunk.surfaceWays?.[profile] || [];
			let uniqueSurface = state.uniqueSurface + (chunk.uniqueSurface || 0);
			for (const [id, length] of surfaces)
				uniqueSurface += Math.max(0, length - (state.surfaceSeen[id] || 0));
			return {
				meters,
				gain: state.gain + chunk.gain,
				rough: state.rough + chunk.rough,
				uniqueSurface,
				matchedSurface,
				separated: state.separated + chunk.separated,
				stopCost: state.stopCost + chunk.stopCost
			};
		}
		let distinct = state.distinct;
		let repeated = state.repeated;
		for (const [street, amount] of chunk.counts) {
			const before = state.streets[street] || 0;
			const added = times * amount;
			if (before + added > laps) return null;
			if (!before) distinct++;
			repeated += added - Number(!before);
		}
		return {
			meters,
			gain: state.gain + times * chunk.gain,
			rough: state.rough + times * chunk.rough,
			distinct,
			repeated,
			repeatedLoops: state.repeatedLoops + times - 1
		};
	}

	function extendGeometry(state, chunk, times, geometry, maxReverse) {
		if (geometry.badJoin(state.last, chunk) || (times > 1 && geometry.badJoin(chunk, chunk)))
			return null;
		const shape = geometry.shape(chunk);
		let reverse = state.reverse + shape.reverse * times;
		let repeat = state.repeat + shape.repeat * times + chunk.meters * (times - 1);
		for (let previous = state.chain; previous; previous = previous.previous) {
			const reused = geometry.reused(previous.chunk, chunk);
			reverse += reused.reverse * previous.times * times;
			repeat += reused.repeat * previous.times * times;
			if (reverse > maxReverse) return null;
		}
		if (reverse > maxReverse) return null;
		const spine = { ...state.spine };
		for (const [name, meters] of Object.entries(chunk.spine || shape.uses)) {
			spine[name] = (spine[name] || 0) + meters * times;
		}
		return { reverse, repeat, spine };
	}

	function routeSequence(state) {
		const chunks = [];
		for (let step = state.chain; step; step = step.previous) {
			for (let i = 0; i < step.times; i++) chunks.push(step.chunk);
		}
		return chunks.reverse();
	}

	function roadCells(chunks, cache) {
		const cells = new Set();
		for (const chunk of chunks) {
			if (!cache.has(chunk.id)) {
				const used = new Set();
				for (const [lat, lon] of chunk.points) {
					// Roughly 160 m cells. A reversed route still visits the same cells.
					used.add(Math.floor(lat * 700) * 100000 + Math.floor(lon * 440));
				}
				cache.set(chunk.id, used);
			}
			for (const cell of cache.get(chunk.id)) cells.add(cell);
		}
		return cells;
	}

	function overlap(a, b) {
		let common = 0;
		for (const cell of a) if (b.has(cell)) common++;
		return common / (a.size + b.size - common);
	}

	function chooseVariants(candidates, profile, distanceOff, gainOff, required) {
		candidates.sort((a, b) => compare(a.score, b.score));
		if (!candidates.length) return [];
		const leading = candidates[0];
		const variantDistance = leading.score[0] > 0.01 ? leading.score[0] + 3000 : 0.01;
		const sameTargets = candidates.filter(
			(candidate) =>
				candidate.score[0] <= variantDistance && candidate.gainMiss <= leading.gainMiss + 0.01
		);
		const cache = new Map();
		const accepted = [];
		const seen = new Set();
		const options = [];
		for (const { state } of sameTargets) {
			const chunks = routeSequence(state);
			const key = chunks.map((chunk) => chunk.id).join('/');
			if (seen.has(key)) continue;
			seen.add(key);
			options.push({ state, chunks, cells: roadCells(chunks, cache) });
		}
		const passes =
			profile === 'climb'
				? [
						[0.79, true],
						[0.87, true],
						[0.79, false],
						[0.87, false]
					]
				: [
						[0.79, false],
						[0.87, false]
					];
		const zoneCombinations = new Set();
		for (const [threshold, newZones] of passes) {
			for (const option of options) {
				if (accepted.includes(option) || (newZones && zoneCombinations.has(option.state.zones)))
					continue;
				if (accepted.some((other) => overlap(option.cells, other.cells) > threshold)) continue;
				accepted.push(option);
				zoneCombinations.add(option.state.zones);
				if (accepted.length === 4) break;
			}
			if (accepted.length === 4) break;
		}
		return accepted.map(({ state, chunks }) => ({
			...state,
			profile,
			chunks,
			distanceOff: distanceOff(state.meters),
			gainOff: gainOff(state.gain),
			selectedZones: climbZones
				.filter(({ id }) => state.zones & required & zoneBits[id])
				.map(({ name }) => name)
		}));
	}

	function findRoutes(settings, routes, profileCircuits, standalone = {}) {
		const { start, goal, surface, profile = 'climb' } = settings;
		const laps = Math.min(settings.laps, 3);
		const { required, excluded } = zoneSelection(profile === 'climb' ? settings.zones : null);
		if (!geometryByDataset.has(routes))
			geometryByDataset.set(routes, WaasRouteShape.prepare(spines));
		const geometry = geometryByDataset.get(routes);
		const maxReverse = Math.min(500, Math.max(160, settings.length * 7));
		const tooMuchReuse = (meters, reverse, repeat) =>
			repeat + reverse > Math.min(8500, Math.max(650, meters * 0.115));
		const smooth = profile === 'climb' && surface === 'smooth';
		const { links, loops } = catalog(routes, smooth, excluded, geometry);
		const { from, to, distanceOff, gainOff, order, score } = scoring({ ...settings, profile });
		const distanceBin = goal === 'range' ? 150 : 75;
		const altitudeBin = goal === 'range' ? 10 : Infinity;
		const candidates = [];
		let bestDistance = Infinity;

		const initial = {
			meters: 0,
			gain: 0,
			rough: 0,
			distinct: 0,
			repeated: 0,
			repeatedLoops: 0,
			zones: 0,
			uniqueSurface: 0,
			matchedSurface: 0,
			separated: 0,
			stopCost: 0,
			reverse: 0,
			repeat: 0,
			spine: {},
			surfaceSeen: Object.create(null),
			streets: Object.create(null),
			last: null,
			first: null,
			chain: null
		};

		for (const [id, route] of Object.entries(standalone)) {
			if (route.from !== start || (!required && route.meters > to + 15000)) continue;
			const intervalRide = route.intervalClimbRide || route.longClimbRide;
			if (intervalRide && (profile !== 'climb' || (goal !== 'max' && !route.extendedRide)))
				continue;
			const chunk = describeChunk(id, route, smooth, excluded);
			if (!chunk || (chunk.zones & required) !== required) continue;
			const measured = chunk.shape || geometry.shape(chunk, !route.catalogRoute);
			const reverseLimit = required
				? Math.min(500, Math.max(160, route.meters * 0.007))
				: maxReverse;
			if (
				measured.uturn ||
				(!intervalRide && !route.catalogRoute && !chunk.shape && geometry.badJoin(chunk, chunk))
			)
				continue;
			// Climb intervals can reuse a road to connect multiple through-loops.
			if (
				route.intervalClimbRide &&
				(measured.reverse > route.meters * 0.27 ||
					measured.reverse + measured.repeat > route.meters * 0.66)
			)
				continue;
			if (!intervalRide) {
				const extendedFlow =
					profile === 'flow' &&
					(route.catalogRoute || route.elevationSource === 'BRouter') &&
					route.meters >= 80000;
				const longSurface = route.extendedRide && (profile === 'gravel' || profile === 'cobbles');
				const allowedReverse =
					extendedFlow || longSurface ? Math.min(9000, route.meters * 0.08) : reverseLimit;
				const reused = measured.repeat + measured.reverse;
				const allowedReuse =
					route.intervalCobbleRide && profile === 'cobbles'
						? route.meters * 0.32
						: extendedFlow || longSurface
							? route.meters * 0.31
							: Math.min(8500, Math.max(650, route.meters * 0.115));
				if (measured.reverse > allowedReverse || reused > allowedReuse) continue;
			}
			const update = advance(initial, chunk, 1, Infinity, laps, profile);
			if (!update) continue;
			const state = {
				...update,
				reverse: measured.reverse,
				repeat: measured.repeat,
				spine: chunk.spine || measured.uses,
				zones: chunk.zones,
				chain: { chunk, times: 1, previous: null }
			};
			const ranking = score(state);
			bestDistance = Math.min(bestDistance, ranking[0]);
			candidates.push({ state, score: ranking, gainMiss: gainOff(state.gain) });
		}

		for (const itinerary of makeItineraries(start, profileCircuits)) {
			const stages = stagesFor(itinerary, links, loops);
			if (!stages?.length) continue;
			const reachable = Array(stages.length + 1).fill(0);
			for (let i = stages.length - 1; i >= 0; i--) {
				reachable[i] = stages[i].choices.reduce(
					(bits, chunk) => bits | chunk.zones,
					reachable[i + 1]
				);
			}
			if ((reachable[0] & required) !== required) continue;
			let remaining = stages.reduce((sum, stage) => sum + stage.minMeters, 0);
			const maximum = Math.max(to, remaining) + 15000;
			if (remaining > to + bestDistance + 3000) continue;
			let states = new Map([[0, initial]]);
			for (const [index, { choices, optional, minMeters }] of stages.entries()) {
				remaining -= minMeters;
				const limit = maximum - remaining + 0.01;
				const next = optional && !required ? new Map(states) : new Map();
				const maxPasses = optional && profile === 'climb' ? laps : 1;
				for (const [stateKey, state] of states) {
					if (
						optional &&
						required &&
						((state.zones | reachable[index + 1]) & required) === required
					) {
						const previous = next.get(stateKey);
						if (!previous || compare(order(state), order(previous)) < 0) next.set(stateKey, state);
					}
					for (const chunk of choices) {
						const zones = state.zones | chunk.zones;
						if (((zones | reachable[index + 1]) & required) !== required) continue;
						for (let times = 1; times <= maxPasses; times++) {
							const update = advance(state, chunk, times, limit, laps, profile);
							if (!update) break;
							const shape = extendGeometry(state, chunk, times, geometry, maxReverse);
							if (!shape) break;
							Object.assign(update, shape);
							const meters = update.meters;
							const bin =
								Math.round(meters / distanceBin) * 4096 + Math.round(update.gain / altitudeBin);
							// Keep the best partial result for each distance and required-zone combination.
							const distanceKey = bin * 3 + (meters < from ? 0 : meters > to ? 2 : 1);
							const key = distanceKey * (1 << climbZones.length) + (zones & required);
							const incumbent = next.get(key);
							if (incumbent && compare(order(update), order(incumbent)) >= 0) continue;
							const streets = Object.assign(Object.create(null), state.streets);
							for (const [street, amount] of chunk.counts)
								streets[street] = (streets[street] || 0) + amount * times;
							let surfaceSeen = state.surfaceSeen;
							if (profile === 'cobbles' || profile === 'gravel') {
								surfaceSeen = Object.assign(Object.create(null), surfaceSeen);
								for (const [id, length] of chunk.surfaceWays?.[profile] || [])
									surfaceSeen[id] = Math.max(surfaceSeen[id] || 0, length);
							}
							next.set(key, {
								...update,
								zones,
								streets,
								surfaceSeen,
								last: chunk,
								first: state.first || chunk,
								chain: { chunk, times, previous: state.chain }
							});
						}
					}
				}
				states = next;
			}
			// Keep multiple finalists from each itinerary without storing the entire search.
			const finalists = [];
			for (const state of states.values()) {
				if (state.meters < 1000 || !state.first || state.last.blocked.includes(state.first.id))
					continue;
				if (
					(state.zones & required) !== required ||
					tooMuchReuse(state.meters, state.reverse, state.repeat)
				)
					continue;
				const ranking = score(state);
				if (ranking[0] > bestDistance + 3000) continue;
				bestDistance = Math.min(bestDistance, ranking[0]);
				const entry = { state, score: ranking, gainMiss: gainOff(state.gain) };
				const at = finalists.findIndex((candidate) => compare(ranking, candidate.score) < 0);
				if (at >= 0) finalists.splice(at, 0, entry);
				else if (finalists.length < 12) finalists.push(entry);
				if (finalists.length > 12) finalists.pop();
			}
			candidates.push(...finalists);
		}
		return chooseVariants(candidates, profile, distanceOff, gainOff, required);
	}

	function decodeTrack({ trackStart, trackDeltas }) {
		const bytes = atob(trackDeltas);
		const sample = [...trackStart];
		const scale = [1e6, 1e6, 10, 10];
		const point = () => sample.map((value, i) => value / scale[i]);
		const points = [point()];
		for (let offset = 0; offset < bytes.length; offset += 8) {
			for (let i = 0; i < 4; i++) {
				const pos = offset + 2 * i;
				const delta = bytes.charCodeAt(pos) + 256 * bytes.charCodeAt(pos + 1);
				sample[i] += delta >= 32768 ? delta - 65536 : delta;
			}
			points.push(point());
		}
		return points;
	}

	function decodePriorityRoutes(routes) {
		return Object.fromEntries(
			Object.entries(routes).map(([id, route]) => {
				const details = { ...route };
				delete details.trackStart;
				delete details.trackDeltas;
				return [id, { ...details, points: decodeTrack(route) }];
			})
		);
	}

	function catalogOptions(catalog, settings, source, limit = 24) {
		if (!catalog) return {};
		const { required, excluded } = zoneSelection(settings.zones);
		const { profile = 'climb' } = settings;
		const { distanceOff, gainOff } = scoring({ ...settings, profile });
		const merit =
			profile === 'climb'
				? (route) => route.gain
				: profile === 'flow'
					? (route) => Object.values(route.spine).reduce((sum, meters) => sum + meters, 0)
					: (route) => route[profile];
		const matching = [];
		for (const [index, route] of catalog.routes.entries()) {
			if (
				profile === 'climb' &&
				(route.passes > Math.min(settings.laps, 3) ||
					(route.zones & required) !== required ||
					route.zones & excluded ||
					(route.intervalClimbRide && settings.goal !== 'max' && !route.extendedRide))
			)
				continue;
			const off = distanceOff(route.meters);
			if (off <= 15000)
				matching.push({ route, index, off, merit: merit(route), range: gainOff(route.gain) });
		}
		matching.sort((a, b) => a.off - b.off || a.range - b.range || b.merit - a.merit);
		if (!matching.length) return {};
		const tracks = new Map();
		const out = {};
		for (const { route, index, off } of matching.slice(0, limit)) {
			if (off > matching[0].off + 3000) break;
			const { slices, ...details } = route;
			delete details.passes;
			delete details.zones;
			const points = [];
			let meters = 0;
			for (const [track, first, last] of slices) {
				if (!tracks.has(track)) tracks.set(track, decodeTrack(catalog.tracks[track]));
				const source = tracks.get(track);
				const start = source[first][3];
				for (let i = first + Number(points.length > 0); i <= last; i++) {
					const [lat, lon, elevation, offset] = source[i];
					points.push([lat, lon, elevation, Math.round((meters + offset - start) * 10) / 10]);
				}
				meters = points.at(-1)[3];
			}
			out[source + '-' + settings.start + '-' + index] = { ...details, points, catalogRoute: true };
		}
		return out;
	}

	function assemble(plan) {
		const chunks = plan.chunks.map((chunk) => WaasRouteMetrics.normalize(chunk));
		let meters = 0;
		const points = [];
		const bridges = [];
		const climbs = [];
		const streets = new Map();
		const sections = [];
		const events = [];
		for (const chunk of chunks) {
			const gap = points.length ? WaasRouteMetrics.distance(points.at(-1), chunk.points[0]) : 0;
			meters += gap;
			for (const [id, name, gain, i, j] of chunk.climbs) {
				const section = chunk.points.slice(i, j + 1);
				const from = meters + section[0][3];
				const to = meters + section.at(-1)[3];
				const climb = { id, name, gain, from, to, points: section, meters: to - from };
				climbs.push(climb);
				const previous = streets.get(id);
				if (previous) {
					previous.gain += gain;
					previous.passes++;
				} else {
					streets.set(id, { ...climb, passes: 1 });
				}
			}
			for (const [kind, name, from, to] of chunk.sections || []) {
				const start = meters + from;
				const end = meters + to;
				const section = sections.at(-1);
				if (
					section?.kind === kind &&
					section.name === name &&
					Math.abs(start - section.to) < 0.25
				) {
					section.to = end;
				} else {
					sections.push({ id: 'section-' + sections.length, kind, name, from: start, to: end });
				}
			}
			for (const [offset, kind] of chunk.events || []) {
				events.push({ meters: meters + offset, kind });
			}
			for (const bridge of chunk.bridges) {
				const [from, to, name, hasSurface, point] = bridge;
				const pos = chunk.points[Math.min(chunk.points.length - 1, point + 2)];
				bridges.push({ from: meters + from, to: meters + to, name, hasSurface, pos });
			}
			for (const [i, [lat, lon, ele, d]] of chunk.points.entries()) {
				if (points.length && i === 0 && gap < 0.001) continue;
				points.push([lat, lon, ele, meters + d]);
			}
			meters += chunk.meters;
		}
		let longestGap = 0;
		let last = 0;
		for (const event of events) {
			longestGap = Math.max(longestGap, event.meters - last);
			last = event.meters;
		}
		longestGap = Math.max(longestGap, meters - last);
		const segmented = sections.map((section) => ({
			...section,
			meters: section.to - section.from,
			points: points.slice(pointIndex(points, section.from), pointIndex(points, section.to) + 1)
		}));
		return {
			...plan,
			chunks,
			meters: points.at(-1)?.[3] || 0,
			gain: WaasRouteMetrics.gain(points),
			points,
			bridges,
			climbs,
			streets: [...streets.values()],
			sections: segmented,
			events,
			longestGap
		};
	}

	function pointIndex(points, meters) {
		let low = 0,
			high = points.length - 1;
		while (low < high) {
			const mid = (low + high) >> 1;
			if (points[mid][3] < meters - 0.05) low = mid + 1;
			else high = mid;
		}
		return low;
	}

	function gpx(route, start) {
		const round = route.chunks[0];
		const title = `${round.title || rideNames[route.profile] || rideNames.climb} · ${hubs[start]} · ${(route.meters / 1000).toFixed(1)} km`;
		const direction = route.direction
			? ` Direction ${route.direction === 'cw' ? 'clockwise' : 'counterclockwise'}.`
			: '';
		const description = `Distance from GPS points ${(route.meters / 1000).toFixed(3)} km. Estimated ascent ${Math.round(route.gain)} m with distance smoothing and a 3 m noise threshold. Road data OpenStreetMap. Elevation ${round.elevationSource || 'DHM Vlaanderen II'}.${direction} All repeated passages and loops are included. Keep the original route when importing.`;
		const visits = (route.visits || []).map((place) => ({ point: place.point, name: place.name }));
		const passages = WaasRouteMetrics.repeatedPassages(route).map((climb) => ({
			point: climb.points.at(-1),
			name: `${climb.name} · passage ${climb.passage} of ${climb.passages}`
		}));
		const rounded = route.points.map(([lat, lon, elevation]) => [lat.toFixed(6), lon.toFixed(6), elevation]);
		return writeGPX(title, description, [rounded], [...visits, ...passages]);
	}

	return {
		findRoutes,
		assemble,
		gpx,
		hubs,
		rideNames,
		climbZones,
		routeClimbZone,
		decodePriorityRoutes,
		catalogOptions
	};
}
