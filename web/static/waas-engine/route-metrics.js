'use strict';

const WaasRouteMetrics = (() => {
	const radius = 6371008.8;
	const radians = Math.PI / 180;
	const noiseThreshold = 3;
	const spacing = 20;
	const routes = new WeakMap();

	function distance(a, b) {
		const lat = (b[0] - a[0]) * radians,
			lon = (b[1] - a[1]) * radians;
		const h =
			Math.sin(lat / 2) ** 2 +
			Math.cos(a[0] * radians) * Math.cos(b[0] * radians) * Math.sin(lon / 2) ** 2;
		return 2 * radius * Math.asin(Math.sqrt(Math.min(1, h)));
	}

	function gain(points, first = 0, last = points.length - 1, height = (point) => point[2], sign = 1) {
		let bottom = null,
			peak = 0,
			rising = false,
			total = 0;
		for (let i = first; i <= last; i++) {
			const sample = height(points[i]);
			if (!Number.isFinite(sample)) {
				if (rising) total += peak - bottom;
				bottom = null;
				rising = false;
				continue;
			}
			const z = sign * sample;
			if (bottom == null) bottom = z;
			if (rising) {
				peak = Math.max(peak, z);
				if (peak - z >= noiseThreshold) {
					total += peak - bottom;
					bottom = z;
					rising = false;
				}
			} else {
				bottom = Math.min(bottom, z);
				if (z - bottom >= noiseThreshold) {
					peak = z;
					rising = true;
				}
			}
		}
		return total + (rising ? peak - bottom : 0);
	}

	function elevation(lines, height = (point) => point[2]) {
		let ascent = 0, descent = 0, highest = null, lowest = null;
		let hasLeg = false;
		for (const line of lines) {
			ascent += gain(line, 0, line.length - 1, height);
			descent += gain(line, 0, line.length - 1, height, -1);
			let previous = null;
			for (const point of line) {
				const z = height(point);
				if (Number.isFinite(z)) {
					highest = highest == null ? z : Math.max(highest, z);
					lowest = lowest == null ? z : Math.min(lowest, z);
					hasLeg ||= previous != null;
					previous = z;
				} else previous = null;
			}
		}
		return { ascent: hasLeg ? ascent : null, descent: hasLeg ? descent : null, highest, lowest };
	}

	function measure(input) {
		let meters = 0;
		let previous = null;
		const points = input.map(([latitude, longitude, z]) => {
			if (!Number.isFinite(latitude) || !Number.isFinite(longitude) || !Number.isFinite(z))
				throw new Error('The route contains invalid GPS or elevation data.');
			const lat = Number(latitude.toFixed(6)),
				lon = Number(longitude.toFixed(6));
			if (previous) meters += distance(previous, [lat, lon]);
			previous = [lat, lon];
			return [lat, lon, z, meters];
		});
		return { points, meters, gain: gain(points) };
	}

	function interpolate(points, at, column, index) {
		while (index + 1 < points.length - 1 && points[index + 1][3] < at) index++;
		const a = points[index],
			b = points[Math.min(index + 1, points.length - 1)];
		const fraction = b[3] === a[3] ? 0 : Math.max(0, Math.min(1, (at - a[3]) / (b[3] - a[3])));
		return { value: a[column] + fraction * (b[column] - a[column]), index };
	}

	function smooth(input, bridges) {
		if (input.length < 3 || input.at(-1)[3] < spacing) return input;
		const end = input.at(-1)[3],
			guide = [];
		let index = 0;
		for (let at = 0; at < end; at += spacing) {
			const sample = interpolate(input, at, 2, index);
			index = sample.index;
			guide.push([at, sample.value]);
		}
		guide.push([end, input.at(-1)[2]]);
		const protectedHeight = (at) => bridges.some(([a, b]) => at >= a && at <= b);
		const median = guide.map(([at, z], i) => {
			if (!i || i === guide.length - 1 || protectedHeight(at)) return z;
			return [guide[i - 1][1], z, guide[i + 1][1]].sort((a, b) => a - b)[1];
		});
		const filtered = guide.map(([at], i) => {
			if (!i || i === guide.length - 1 || protectedHeight(at)) return median[i];
			return 0.25 * median[i - 1] + 0.5 * median[i] + 0.25 * median[i + 1];
		});
		index = 0;
		return input.map(([lat, lon, z, at]) => {
			while (index + 1 < guide.length - 1 && guide[index + 1][0] < at) index++;
			const fraction = (at - guide[index][0]) / (guide[index + 1][0] - guide[index][0]);
			const height = protectedHeight(at)
				? z
				: filtered[index] + fraction * (filtered[index + 1] - filtered[index]);
			return [lat, lon, Math.round(height * 10) / 10, at];
		});
	}

	function normalize(route) {
		if (route.metricsVersion === 1) return route;
		if (routes.has(route)) return routes.get(route);
		const measured = measure(route.points);
		const original = route.points;
		function offset(at) {
			let low = 0,
				high = original.length - 1;
			while (low < high) {
				const mid = (low + high) >> 1;
				if (original[mid][3] < at) low = mid + 1;
				else high = mid;
			}
			const i = Math.max(0, low - 1),
				a = original[i],
				b = original[Math.min(i + 1, original.length - 1)];
			const f = b[3] === a[3] ? 0 : Math.max(0, Math.min(1, (at - a[3]) / (b[3] - a[3])));
			return (
				measured.points[i][3] +
				f * (measured.points[Math.min(i + 1, original.length - 1)][3] - measured.points[i][3])
			);
		}
		const bridges = (route.bridges || []).map(([a, b, ...details]) => [
			offset(a),
			offset(b),
			...details
		]);
		const points = smooth(measured.points, bridges);
		const ratio = route.meters > 0 ? measured.meters / route.meters : 1;
		const scale = Object.fromEntries(
			['rough', 'unknown', 'cobbles', 'gravel', 'separated', 'uniqueSurface']
				.filter((key) => Number.isFinite(route[key]))
				.map((key) => [key, route[key] * ratio])
		);
		const result = {
			...route,
			...scale,
			points,
			meters: measured.meters,
			gain: gain(points),
			bridges,
			metricsVersion: 1,
			climbs: (route.climbs || []).map(([id, name, , first, last]) => [
				id,
				name,
				gain(points, first, last),
				first,
				last
			]),
			sections: (route.sections || []).map(([kind, name, a, b]) => [
				kind,
				name,
				offset(a),
				offset(b)
			]),
			events: (route.events || []).map(([at, kind]) => [offset(at), kind]),
			...(route.spine
				? {
						spine: Object.fromEntries(
							Object.entries(route.spine).map(([name, m]) => [name, m * ratio])
						)
					}
				: {})
		};
		routes.set(route, result);
		return result;
	}

	function repeatedPassages(route) {
		const seen = new Map();
		return route.climbs.flatMap((climb) => {
			const index = (seen.get(climb.id) || 0) + 1;
			seen.set(climb.id, index);
			const count = route.streets.find((street) => street.id === climb.id)?.passes || 1;
			return count > 1 ? [{ ...climb, passage: index, passages: count }] : [];
		});
	}

	return { distance, gain, elevation, measure, normalize, repeatedPassages };
})();

export { WaasRouteMetrics };
