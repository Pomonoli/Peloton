import { segmentSpeedKmh } from '#lib/format.js';

export type RivalryPeriod = 'all' | 'year' | 'recent';

export function rivalryFrom(period: RivalryPeriod, now: Date): string | null {
	switch (period) {
		case 'year':
			return new Date(now.getFullYear(), 0, 1).toISOString();
		case 'recent':
			return new Date(now.getTime() - 90 * 24 * 60 * 60 * 1000).toISOString();
		default:
			return null;
	}
}

export interface HistoryEffort {
	start_time: string;
	elapsed_s: number;
}

export function segmentSpeedRange(min: number, max: number): [number, number] {
	const padding = Math.max((max - min) * 0.1, 0.1);
	return [Math.max(0, min - padding), max + padding];
}

export function alignEffortHistory(histories: HistoryEffort[][], distanceM: number) {
	if (segmentSpeedKmh(distanceM, 1) === null) return { offsets: [], times: [], values: [] };
	const riders = histories.map((efforts) => {
		const byTime = new Map<number, number>();
		for (const effort of efforts) {
			const at = Date.parse(effort.start_time) / 1000;
			if (!Number.isFinite(at) || segmentSpeedKmh(distanceM, effort.elapsed_s) === null) continue;
			byTime.set(at, Math.min(effort.elapsed_s, byTime.get(at) ?? Infinity));
		}
		return byTime;
	});
	const offsets = [...new Set(riders.flatMap((rider) => [...rider.keys()]))].sort((a, b) => a - b);
	if (offsets.length === 1) {
		offsets.unshift(offsets[0]! - 86400);
		offsets.push(offsets[1]! + 86400);
	}
	const times = riders.map((rider) => offsets.map((at) => rider.get(at) ?? null));
	return {
		offsets,
		times,
		values: times.map((row) => row.map((time) => segmentSpeedKmh(distanceM, time)))
	};
}
