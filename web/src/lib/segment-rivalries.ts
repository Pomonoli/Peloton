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

export function alignEffortHistory(histories: HistoryEffort[][]) {
	const riders = histories.map((efforts) => {
		const byTime = new Map<number, number>();
		for (const effort of efforts) {
			const at = Date.parse(effort.start_time) / 1000;
			if (!Number.isFinite(at) || !Number.isFinite(effort.elapsed_s) || effort.elapsed_s <= 0)
				continue;
			byTime.set(at, Math.min(effort.elapsed_s, byTime.get(at) ?? Infinity));
		}
		return byTime;
	});
	const offsets = [...new Set(riders.flatMap((rider) => [...rider.keys()]))].sort((a, b) => a - b);
	if (offsets.length === 1) {
		offsets.unshift(offsets[0]! - 86400);
		offsets.push(offsets[1]! + 86400);
	}
	return { offsets, values: riders.map((rider) => offsets.map((at) => rider.get(at) ?? null)) };
}
