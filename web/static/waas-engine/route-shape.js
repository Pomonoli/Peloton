'use strict';

const WaasRouteShape = (() => {
	const sampleStep = 12;
	const cellSize = 20;
	const padding = 128;
	const longitudeScale = 69800;
	const latitudeScale = 111195;

	function project(points) {
		return points.map(([lat, lon, , meters]) => [
			(lon - 4) * longitudeScale,
			(lat - 51) * latitudeScale,
			meters
		]);
	}

	function at(points, meters, index) {
		while (index + 1 < points.length - 1 && points[index + 1][2] < meters) index++;
		const a = points[index],
			b = points[index + 1];
		const fraction = b[2] === a[2] ? 0 : (meters - a[2]) / (b[2] - a[2]);
		return [a[0] + fraction * (b[0] - a[0]), a[1] + fraction * (b[1] - a[1]), index];
	}

	function turnsBack(points, first = 1, last = points.length - 1) {
		if (points.length < 3) return false;
		const start = points[0][2],
			end = points.at(-1)[2];
		let before = 0,
			after = 0;
		for (let i = first; i < last; i++) {
			const pivot = points[i];
			if (pivot[2] - start < 10 || end - pivot[2] < 10) continue;
			const span = Math.min(24, pivot[2] - start, end - pivot[2]);
			const a = at(points, pivot[2] - span, before);
			const b = at(points, pivot[2] + span, after);
			before = a[2];
			after = b[2];
			const entry = Math.hypot(a[0] - pivot[0], a[1] - pivot[1]);
			const exit = Math.hypot(b[0] - pivot[0], b[1] - pivot[1]);
			if (entry > span * 0.72 && exit > span * 0.72 && Math.hypot(a[0] - b[0], a[1] - b[1]) < 5.5)
				return true;
		}
		return false;
	}

	function sample(points) {
		if (points.length < 2) return [];
		const result = [];
		let index = 0,
			last = points[0];
		for (let m = last[2]; m < points.at(-1)[2]; m += sampleStep) {
			const end = Math.min(m + sampleStep, points.at(-1)[2]);
			const next = at(points, end, index);
			index = next[2];
			const dx = next[0] - last[0],
				dy = next[1] - last[1],
				length = Math.hypot(dx, dy);
			if (length >= 4)
				result.push({
					x: (last[0] + next[0]) / 2,
					y: (last[1] + next[1]) / 2,
					dx: dx / length,
					dy: dy / length,
					meters: end - m,
					position: m
				});
			last = next;
		}
		return result;
	}

	const cellKey = (x, y) => x * 20000 + y;
	function add(index, piece) {
		const key = cellKey(Math.floor(piece.x / cellSize), Math.floor(piece.y / cellSize));
		const found = index.get(key);
		if (found) found.push(piece);
		else index.set(key, [piece]);
	}

	function matchingDirection(index, piece, sameTrack = false) {
		const x = Math.floor(piece.x / cellSize),
			y = Math.floor(piece.y / cellSize);
		let withDirection = 0;
		for (let a = x - 1; a <= x + 1; a++) {
			for (let b = y - 1; b <= y + 1; b++) {
				for (const previous of index.get(cellKey(a, b)) || []) {
					if (sameTrack && piece.position - previous.position < 40) continue;
					const alignment = piece.dx * previous.dx + piece.dy * previous.dy;
					if (Math.abs(alignment) < 0.88) continue;
					const dx = piece.x - previous.x,
						dy = piece.y - previous.y;
					const across = Math.abs(dx * piece.dy - dy * piece.dx);
					const along = Math.abs(dx * piece.dx + dy * piece.dy);
					if (across > 7 || along > sampleStep * 0.78) continue;
					if (alignment < 0) return -1;
					withDirection = 1;
				}
			}
		}
		return withDirection;
	}

	function spineIndices(lines) {
		return Object.fromEntries(
			Object.entries(lines).map(([name, paths]) => {
				const index = new Map();
				for (const path of paths) {
					for (const piece of sample(project(path))) add(index, piece);
				}
				return [name, index];
			})
		);
	}

	function prepare(lines) {
		const corridors = spineIndices(lines);
		const shapes = new Map(),
			seams = new Map(),
			pairs = new Map();

		function shape(chunk, cache = true) {
			if (cache && shapes.has(chunk.id)) return shapes.get(chunk.id);
			const points = project(chunk.points);
			const pieces = sample(points);
			const index = new Map();
			const uses = Object.fromEntries(Object.keys(corridors).map((name) => [name, 0]));
			let reverse = 0,
				repeat = 0;
			for (const piece of pieces) {
				const matched = matchingDirection(index, piece, true);
				if (matched < 0) reverse += piece.meters;
				if (matched > 0) repeat += piece.meters;
				add(index, piece);
				for (const [name, geometry] of Object.entries(corridors)) {
					if (matchingDirection(geometry, piece)) uses[name] += piece.meters;
				}
			}
			const result = {
				points,
				pieces,
				index,
				reverse,
				repeat,
				uses,
				uturn: turnsBack(points)
			};
			if (cache) shapes.set(chunk.id, result);
			return result;
		}

		function badJoin(left, right) {
			if (!left) return false;
			const key = left.id + '|' + right.id;
			if (seams.has(key)) return seams.get(key);
			const a = shape(left).points.filter((p) => p[2] >= left.meters - padding);
			const b = shape(right)
				.points.filter((p) => p[2] <= padding)
				.slice(1)
				.map(([x, y, m]) => [x, y, m + left.meters]);
			const bad = turnsBack(
				[...a, ...b],
				Math.max(1, a.length - 3),
				Math.min(a.length + 3, a.length + b.length - 1)
			);
			seams.set(key, bad);
			return bad;
		}

		function reused(left, right) {
			// Measure a stretch once, even when stored in two travel directions.
			const key = left.id < right.id ? left.id + '|' + right.id : right.id + '|' + left.id;
			if (pairs.has(key)) return pairs.get(key);
			const a = shape(left),
				b = shape(right);
			const [shorter, longer] = a.pieces.length <= b.pieces.length ? [a, b] : [b, a];
			let reverse = 0,
				repeat = 0;
			for (const piece of shorter.pieces) {
				const direction = matchingDirection(longer.index, piece);
				if (direction < 0) reverse += piece.meters;
				if (direction > 0) repeat += piece.meters;
			}
			const result = { reverse, repeat };
			pairs.set(key, result);
			return result;
		}

		return { shape, badJoin, reused };
	}

	const hasTurnback = (points) => turnsBack(project(points));
	return { prepare, hasTurnback };
})();

export { WaasRouteShape };
