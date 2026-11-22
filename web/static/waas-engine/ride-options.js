import { WaasRouteShape } from './route-shape.js';

export function createRideOptions(WaasPlanner) {
	const departures = {
		sn: [51.164565, 4.139184],
		wm: [51.109761, 4.084395],
		ti: [51.114126, 4.175777],
		te: [51.124926, 4.224447]
	};
	// Public landmarks. Routing uses the neighbouring accessible street.
	const places = [
		{ id: 'kerk-temse', name: 'Temse church', point: [51.12403, 4.21385] },
		{ id: 'dijk-tielrode', name: 'Tielrode dike', point: [51.10725, 4.18062] },
		{ id: 'kerk-elversele', name: 'Elversele church', point: [51.1146, 4.13836] },
		{ id: 'markt-lokeren', name: 'Lokeren market square', point: [51.10452, 3.98921] },
		{ id: 'markt-beveren', name: 'Beveren market square', point: [51.21321, 4.25765] },
		{
			id: 'veer-linkeroever',
			name: 'Linkeroever ferry landing',
			point: [51.22466, 4.39191],
			ferry: true
		},
		{
			id: 'veer-kruibeke',
			name: 'Kruibeke ferry landing',
			point: [51.17566, 4.32354],
			ferry: true
		},
		{ id: 'kerk-stekene', name: 'Stekene church', point: [51.2069, 4.04071] },
		{ id: 'markt-sint-niklaas', name: 'Sint-Niklaas market square', point: departures.sn },
		{ id: 'centrum-waasmunster', name: 'Waasmunster centre', point: departures.wm },
		{ id: 'centrum-rupelmonde', name: 'Rupelmonde centre', point: [51.12773, 4.2929] },
		{ id: 'kerk-bazel', name: 'Bazel church', point: [51.1448, 4.30145] }
	];
	const radians = Math.PI / 180;
	const cache = new Map();
	const compass = [
		'N',
		'NNE',
		'NE',
		'ENE',
		'E',
		'ESE',
		'SE',
		'SSE',
		'S',
		'SSW',
		'SW',
		'WSW',
		'W',
		'WNW',
		'NW',
		'NNW'
	];
	const distance = (a, b) =>
		6371000 *
		radians *
		Math.hypot(a[0] - b[0], (a[1] - b[1]) * Math.cos(((a[0] + b[0]) * radians) / 2));
	const selected = (ids) => places.filter((place) => ids?.includes(place.id));

	function orientation(points) {
		const [lat, lon] = points[0];
		let area = 0;
		for (let i = 1; i < points.length; i++) {
			const a = points[i - 1],
				b = points[i];
			area += (a[1] - lon) * (b[0] - lat) - (b[1] - lon) * (a[0] - lat);
		}
		return area < 0 ? 'cw' : 'ccw';
	}

	function nearest(points, point) {
		let best = Infinity,
			index = 0;
		for (let i = 0; i < points.length; i++) {
			const meters = distance(points[i], point);
			if (meters < best) {
				best = meters;
				index = i;
			}
		}
		return { meters: best, index, point: points[index] };
	}

	function visitPlaces(points, ids) {
		return selected(ids)
			.map((place) => ({ ...place, ...nearest(points, place.point) }))
			.sort((a, b) => a.index - b.index);
	}

	function turnLocations(points) {
		const turns = [];
		let before = 0,
			after = 1;
		function at(m, index) {
			while (index + 1 < points.length - 1 && points[index + 1][3] < m) index++;
			const a = points[index],
				b = points[index + 1];
			const f = b[3] === a[3] ? 0 : (m - a[3]) / (b[3] - a[3]);
			return { point: [a[0] + f * (b[0] - a[0]), a[1] + f * (b[1] - a[1])], index };
		}
		for (let i = 1; i < points.length - 1; i++) {
			const pivot = points[i],
				offset = pivot[3],
				span = Math.min(24, offset, points.at(-1)[3] - offset);
			if (span < 10) continue;
			const a = at(offset - span, before),
				b = at(offset + span, after);
			before = a.index;
			after = b.index;
			if (
				distance(a.point, pivot) > span * 0.72 &&
				distance(b.point, pivot) > span * 0.72 &&
				distance(a.point, b.point) < 5.5 &&
				!turns.some((point) => distance(point, pivot) < 80)
			)
				turns.push(pivot);
		}
		return turns;
	}

	function anchorsFor(plan, options, sparser = false) {
		const route = WaasPlanner.assemble(plan);
		let points = route.points;
		const reverse = options.direction !== 'auto' && orientation(points) !== options.direction;
		if (reverse) {
			const length = route.meters;
			points = points
				.slice()
				.reverse()
				.map(([lat, lon, z, m]) => [lat, lon, z, length - m]);
		}
		const visits = visitPlaces(points, options.places);
		const gap = sparser ? 5500 : 2400;
		const anchors = [{ point: points[0], meters: 0, protected: true }];
		let last = 0;
		for (let i = 1; i < points.length - 1; i++) {
			if (points[i][3] - last < gap) continue;
			const point = points[i];
			if (
				visits.some(
					(visit) => Math.abs(visit.point[3] - point[3]) < Math.max(1000, visit.meters * 1.25)
				)
			)
				continue;
			anchors.push({ point, meters: point[3] });
			last = point[3];
		}
		// Keep the selected profile's marked roads as anchors when changing direction.
		const sections =
			options.profile === 'climb'
				? route.climbs
				: options.profile === 'flow'
					? []
					: route.sections.filter((s) => s.kind === options.profile && s.meters >= 80);
		for (const section of sections) {
			const candidates = [
				section.points[0],
				section.points[Math.floor(section.points.length / 2)],
				section.points.at(-1)
			];
			for (const point of candidates) {
				const at = reverse ? route.meters - point[3] : point[3];
				if (at < 80 || route.meters - at < 80) continue;
				if (visits.some((visit) => Math.abs(visit.point[3] - at) < Math.max(700, visit.meters)))
					continue;
				if (anchors.some((anchor) => Math.abs(anchor.meters - at) < 100)) continue;
				anchors.push({ point, meters: at });
			}
		}
		for (const visit of visits) {
			if (distance(points[0], visit.point) < 120) continue;
			anchors.push({
				point: places.find((place) => place.id === visit.id).point,
				meters: visit.point[3],
				protected: true
			});
		}
		anchors.sort((a, b) => a.meters - b.meters);
		const ordered = anchors.filter(
			(anchor, i) => !i || distance(anchor.point, anchors[i - 1].point) > 35
		);
		ordered.push({ point: points.at(-1), meters: route.meters, protected: true });
		return ordered.map(({ point }) => point.slice(0, 2));
	}

	function compactAnchors(plan, options, measured) {
		const route = WaasPlanner.assemble(plan);
		const visits = visitPlaces(route.points, options.places);
		let points = [
			route.points[0].slice(0, 2),
			...visits.map((visit) => places.find((p) => p.id === visit.id).point)
		];
		points = points.filter((point, i) => !i || distance(points[0], point) > 150);
		points.push(points[0]);
		if (options.direction !== 'auto' && orientation(points) !== options.direction)
			points = points.slice().reverse();
		if (!measured) return points;
		const budget = options.length * 1000 - measured.meters;
		if (budget < 1500) return points;
		let best = null;
		// Insert a point from the original ride to use the remaining distance.
		for (let i = 0; i < route.points.length; i += 30) {
			const point = route.points[i].slice(0, 2);
			if (points.some((p) => distance(p, point) < 1000)) continue;
			let position = 1,
				cost = Infinity;
			for (let j = 1; j < points.length; j++) {
				const delta =
					distance(points[j - 1], point) +
					distance(point, points[j]) -
					distance(points[j - 1], points[j]);
				if (delta < cost) {
					position = j;
					cost = delta;
				}
			}
			const score = Math.abs(cost * 1.2 - budget);
			if (!best || score < best.score) best = { point, position, score };
		}
		if (best) points.splice(best.position, 0, best.point);
		return points;
	}

	async function fetchRoute(anchors, options, signal, nogos = []) {
		const params = new URLSearchParams({
			lonlats: anchors.map(([lat, lon]) => `${lon.toFixed(6)},${lat.toFixed(6)}`).join('|'),
			profile: options.profile === 'gravel' ? 'trekking' : 'fastbike',
			format: 'geojson',
			alternativeidx: '0',
			'profile:allow_ferries': '0',
			'profile:allow_steps': '0',
			'profile:correctMisplacedViaPoints': '1',
			'profile:correctMisplacedViaPointsDistance': '200'
		});
		const avoid = [[4.140822, 51.162562, 11], ...nogos];
		params.set('nogos', avoid.map((p) => p.join(',')).join('|'));
		const key = params.toString();
		if (cache.has(key)) return cache.get(key);
		const timer = new AbortController();
		const timeout = setTimeout(() => timer.abort(), 45000);
		const stop = () => timer.abort();
		signal?.addEventListener('abort', stop, { once: true });
		try {
			if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
			const response = await fetch('https://brouter.de/brouter?' + key, { signal: timer.signal });
			if (!response.ok)
				throw new Error('The cycling router is temporarily unavailable. Please try again.');
			const json = await response.json();
			const feature = json.features?.[0];
			if (!feature?.geometry?.coordinates?.length)
				throw new Error('No continuous cycling connection was found through these places.');
			cache.set(key, feature);
			if (cache.size > 12) cache.delete(cache.keys().next().value);
			return feature;
		} catch (error) {
			if (signal?.aborted) throw error;
			if (error.name === 'AbortError')
				throw new Error('The cycling router timed out. Please try again.', { cause: error });
			if (error instanceof TypeError)
				throw new Error('The cycling router is temporarily unavailable. Please try again.', { cause: error });
			throw error;
		} finally {
			clearTimeout(timeout);
			signal?.removeEventListener('abort', stop);
		}
	}

	function gainBetween(points, first, last) {
		let bottom = points[first][2],
			peak = bottom,
			rising = false,
			gain = 0;
		for (let i = first + 1; i <= last; i++) {
			const z = points[i][2];
			if (rising) {
				peak = Math.max(peak, z);
				if (peak - z >= 2) {
					gain += peak - bottom;
					bottom = z;
					rising = false;
				}
			} else {
				bottom = Math.min(bottom, z);
				if (z - bottom >= 2) {
					peak = z;
					rising = true;
				}
			}
		}
		return Math.round(gain + (rising ? peak - bottom : 0));
	}

	function readRoute(feature, options, template) {
		const coordinates = feature.geometry.coordinates;
		let meters = 0;
		const points = coordinates.map(([lon, lat, z], i) => {
			if (i) meters += distance([lat, lon], [coordinates[i - 1][1], coordinates[i - 1][0]]);
			return [lat, lon, Number.isFinite(z) ? z : 0, meters];
		});
		if (distance(points[0], points.at(-1)) > 8)
			throw new Error('The route does not close at the departure point. Please try again.');
		const lookups = new Map();
		for (const [i, [lat, lon]] of points.entries()) {
			const key = Math.round(lon * 1e6) + ',' + Math.round(lat * 1e6);
			if (!lookups.has(key)) lookups.set(key, []);
			lookups.get(key).push(i);
		}
		const sections = [],
			events = [],
			restricted = [];
		let last = 0,
			unknown = 0,
			rough = 0,
			cobbles = 0,
			gravel = 0,
			separated = 0;
		const spine = {},
			uniqueSurface = new Map();
		for (const row of (feature.properties.messages || []).slice(1)) {
			const [x, y, , , , , , , , way = '', node = ''] = row;
			const tags = Object.fromEntries(
				way
					.split(' ')
					.filter(Boolean)
					.map((tag) => {
						const at = tag.indexOf('=');
						return [tag.slice(0, at), tag.slice(at + 1)];
					})
			);
			const index = lookups.get(`${x},${y}`)?.find((i) => i >= last);
			if (index === undefined) continue;
			const a = points[last][3],
				b = points[index][3],
				length = b - a;
			if (
				tags.bicycle === 'no' ||
				tags.bicycle === 'dismount' ||
				tags.highway === 'steps' ||
				tags.route === 'ferry' ||
				(['no', 'private'].includes(tags.access) &&
					!['yes', 'designated', 'permissive'].includes(tags.bicycle))
			) {
				restricted.push([Number(x) / 1e6, Number(y) / 1e6, 18]);
			}
			if (!tags.surface) unknown += length;
			const isCobble = ['sett', 'cobblestone', 'unhewn_cobblestone'].includes(tags.surface);
			const isGravel = [
				'gravel',
				'fine_gravel',
				'compacted',
				'ground',
				'dirt',
				'earth',
				'unpaved',
				'pebblestone'
			].includes(tags.surface);
			if (isCobble) cobbles += length;
			if (isGravel) gravel += length;
			if (isCobble || isGravel || tags.surface === 'paving_stones') rough += length;
			const kind = isCobble
				? 'cobbles'
				: isGravel
					? 'gravel'
					: tags.highway === 'cycleway'
						? 'flow'
						: null;
			if (kind) {
				const name =
					kind === 'flow'
						? 'Mapped cycle path'
						: kind === 'cobbles'
							? 'Cobbled section'
							: 'Gravel section';
				const previous = sections.at(-1);
				if (previous?.[0] === kind && Math.abs(previous[3] - a) < 1) previous[3] = b;
				else sections.push([kind, name, a, b]);
				if (kind === options.profile) {
					for (let i = last + 1; i <= index; i++) {
						const left = points[i - 1],
							right = points[i];
						const key = [left.slice(0, 2).join(','), right.slice(0, 2).join(',')].sort().join('/');
						uniqueSurface.set(key, Math.max(uniqueSurface.get(key) || 0, right[3] - left[3]));
					}
				}
			}
			if (tags.highway === 'cycleway') {
				separated += length;
				spine['Mapped cycle path'] = (spine['Mapped cycle path'] || 0) + length;
			}
			const event = /traffic_signals/.test(node)
				? 'signal'
				: /highway=stop/.test(node)
					? 'stop'
					: /give_way/.test(node)
						? 'giveway'
						: /barrier=/.test(node)
							? 'barrier'
							: null;
			if (event && (!events.length || b - events.at(-1)[0] > 20)) events.push([b, event]);
			last = index;
		}
		const base = WaasPlanner.assemble(template);
		const climbs = [];
		let bottom = 0,
			peak = 0;
		function addClimb() {
			const gain = points[peak][2] - points[bottom][2];
			if (gain < 6 || points[peak][3] - points[bottom][3] < 30) return;
			const reference = base.climbs.find(
				(climb) => nearest(climb.points, points[peak]).meters < 110
			);
			climbs.push([
				reference?.id || 'rise-' + climbs.length,
				reference?.name || 'Climb',
				Math.round(gain),
				bottom,
				peak
			]);
		}
		for (let i = 1; i < points.length; i++) {
			if (points[i][2] > points[peak][2]) peak = i;
			if (points[peak][2] - points[i][2] >= 2) {
				addClimb();
				bottom = i;
				peak = i;
			} else if (points[i][2] < points[bottom][2]) {
				bottom = i;
				peak = i;
			}
		}
		addClimb();
		const shape = WaasRouteShape.prepare({}).shape({ points }, false);
		const visits = visitPlaces(points, options.places);
		const missing = visits.filter((place) => place.meters > 180);
		if (missing.length)
			throw new Error(
				'No accessible cycling connection near ' +
					missing.map((p) => p.name).join(', ') +
					'. Choose a different place.'
			);
		const direction = orientation(points);
		if (options.direction !== 'auto' && direction !== options.direction)
			throw new Error('These places do not form a continuous loop in the selected direction.');
		const chunk = {
			id: 'custom-' + Date.now(),
			from: options.start,
			to: options.start,
			points,
			meters,
			gain: gainBetween(points, 0, points.length - 1),
			standalone: true,
			customRoute: true,
			elevationSource: 'BRouter',
			sections,
			events,
			climbs,
			bridges: [],
			blocked: [],
			rough,
			unknown,
			cobbles,
			gravel,
			separated,
			spine,
			shape: { uturn: shape.uturn, reverse: shape.reverse, repeat: shape.repeat },
			uniqueSurface: [...uniqueSurface.values()].reduce((sum, m) => sum + m, 0),
			stopCost: events.length,
			...(base.chunks[0].scheldeRound && !options.places.length
				? { title: 'Scheldt and canal loop', scheldeRound: true }
				: {})
		};
		const required = WaasPlanner.climbZones.filter(
			(zone) => options.profile === 'climb' && options.zones?.[zone.id] === 'include'
		);
		const zones = climbs.reduce(
			(bits, climb) => bits | WaasPlanner.routeClimbZone(climb, chunk),
			0
		);
		const hasZone = (zone) =>
			zones & (1 << WaasPlanner.climbZones.findIndex((item) => item.id === zone.id));
		if (required.some((zone) => !hasZone(zone)))
			throw new Error(
				'This connection does not include every selected climbing zone. Adjust the places or zones.'
			);
		const excluded = WaasPlanner.climbZones.filter(
			(zone) => options.profile === 'climb' && options.zones?.[zone.id] === 'avoid'
		);
		if (excluded.some(hasZone))
			throw new Error('This connection climbs in an excluded zone. Adjust the places or zones.');
		const plan = {
			...template,
			chunks: [chunk],
			meters,
			gain: chunk.gain,
			rough,
			spine,
			separated,
			matchedSurface: chunk[options.profile] || 0,
			uniqueSurface: chunk.uniqueSurface,
			reverse: shape.reverse,
			repeat: shape.repeat,
			selectedZones: required.map((zone) => zone.name),
			direction,
			visits,
			distanceOff: Math.max(0, Math.abs(meters - options.length * 1000) - options.tolerance * 1000),
			gainOff:
				options.goal === 'range'
					? Math.max(0, options.minGain - chunk.gain, chunk.gain - options.maxGain)
					: 0
		};
		return { plan, restricted, turns: turnLocations(points) };
	}

	function messagesFor(plan) {
		const notices = [];
		const chunk = plan.chunks[0];
		if (chunk.customRoute) {
			notices.push(
				'Direction and places were calculated by the cycling router. Elevation was estimated again using BRouter.'
			);
			if (plan.reverse + plan.repeat > 1000)
				notices.push(
					'The connection between these places includes ' +
						(Math.round((plan.reverse + plan.repeat) / 100) / 10).toLocaleString('en-US') +
						' km of previously used roads.'
				);
		}
		return notices;
	}

	async function customize(plans, options, signal, onProgress) {
		if (options.direction === 'auto' && !options.places.length) return plans;
		const ranked = plans
			.map((plan) => {
				const route = WaasPlanner.assemble(plan),
					visits = visitPlaces(route.points, options.places);
				const direction = orientation(route.points);
				const misses = visits.reduce((sum, p) => sum + Math.max(0, p.meters - 150), 0);
				return { plan, route, visits, direction, misses };
			})
			.sort((a, b) => a.misses - b.misses);
		const matching = ranked.filter(
			({ misses, direction }) =>
				!misses && (options.direction === 'auto' || direction === options.direction)
		);
		if (matching.length)
			return matching.map(({ plan, visits, direction }) => ({ ...plan, visits, direction }));
		if (!ranked.length)
			throw new Error(
				'No base route is available for this distance. Choose a wider distance margin.'
			);
		onProgress('Calculating cycling connections through your places…');
		const results = [];
		let lastError;
		for (const [i, { plan }] of ranked.slice(0, 2).entries()) {
			if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
			try {
				let anchors = anchorsFor(plan, options);
				let feature = await fetchRoute(anchors, options, signal);
				let result = readRoute(feature, options, plan);
				if (result.restricted.length || result.turns.length) {
					const protectedPoints = [anchors[0], ...selected(options.places).map((p) => p.point)];
					anchors = anchors.filter(
						(point, index) =>
							index === 0 ||
							index === anchors.length - 1 ||
							protectedPoints.some((p) => distance(point, p) < 50) ||
							!result.turns.some((turn) => distance(point, turn) < 220)
					);
					const avoid = result.turns
						.filter((turn) => !protectedPoints.some((p) => distance(turn, p) < 90))
						.map(([lat, lon]) => [lon, lat, 9]);
					feature = await fetchRoute(anchors, options, signal, [...result.restricted, ...avoid]);
					result = readRoute(feature, options, plan);
				}
				if (result.restricted.length)
					throw new Error(
						'The router used a restricted road. Please choose a different combination.'
					);
				if (result.plan.chunks[0].shape.uturn)
					throw new Error(
						'This combination requires a U-turn. Choose a different direction or places.'
					);
				results.push(result.plan);
				if (result.plan.distanceOff === 0 && !result.plan.chunks[0].shape.uturn) break;
				if (i === 0 && Math.abs(result.plan.meters - options.length * 1000) > 6000) {
					feature = await fetchRoute(anchorsFor(plan, options, true), options, signal);
					const shorter = readRoute(feature, options, plan);
					if (!shorter.restricted.length && !shorter.plan.chunks[0].shape.uturn)
						results.push(shorter.plan);
				}
			} catch (error) {
				if (signal?.aborted) throw error;
				lastError = error;
			}
		}
		if (
			options.profile === 'flow' &&
			options.places.length >= 2 &&
			(!results.length || results.every((plan) => plan.distanceOff > 5000))
		) {
			const template = ranked[0].plan;
			try {
				let feature = await fetchRoute(compactAnchors(template, options), options, signal);
				let direct = readRoute(feature, options, template);
				if (!direct.restricted.length && !direct.plan.chunks[0].shape.uturn)
					results.push(direct.plan);
				if (direct.plan.meters < options.length * 1000 - 1500) {
					feature = await fetchRoute(
						compactAnchors(template, options, direct.plan),
						options,
						signal
					);
					direct = readRoute(feature, options, template);
					if (!direct.restricted.length && !direct.plan.chunks[0].shape.uturn)
						results.push(direct.plan);
				}
			} catch (error) {
				if (signal?.aborted) throw error;
				lastError = error;
			}
		}
		if (!results.length)
			throw lastError || new Error('No continuous route was found for these choices.');
		results.sort(
			(a, b) =>
				a.distanceOff - b.distanceOff ||
				(options.profile === 'climb'
					? b.gain - a.gain
					: options.profile === 'flow'
						? b.separated - a.separated
						: b.matchedSurface - a.matchedSurface)
		);
		return results;
	}

	async function weather(start, signal) {
		const [latitude, longitude] = departures[start];
		const params = new URLSearchParams({
			latitude,
			longitude,
			current: 'wind_speed_10m,wind_direction_10m,wind_gusts_10m',
			timezone: 'Europe/Brussels'
		});
		const response = await fetch('https://api.open-meteo.com/v1/forecast?' + params, { signal });
		if (!response.ok) throw new Error('Wind data is temporarily unavailable');
		const data = await response.json(),
			now = data.current;
		const speed = now?.wind_speed_10m,
			direction = now?.wind_direction_10m;
		if (
			!Number.isFinite(speed) ||
			!Number.isFinite(direction) ||
			speed < 0 ||
			direction < 0 ||
			direction > 360 ||
			!now.time
		)
			throw new Error('Wind data is missing');
		const timestamp = Date.parse(now.time + 'Z') - data.utc_offset_seconds * 1000;
		if (!Number.isFinite(timestamp) || Math.abs(Date.now() - timestamp) > 90 * 60000)
			throw new Error('No recent wind data is available');
		return {
			speed,
			direction,
			compass: compass[Math.round(direction / 22.5) % 16],
			gusts: now.wind_gusts_10m,
			timestamp
		};
	}

	return {
		places,
		departures,
		orientation,
		distance,
		nearest,
		visitPlaces,
		customize,
		weather,
		messagesFor
	};
}
