export function tickStep(raw: number) {
	const base = 10 ** Math.floor(Math.log10(raw));
	return ([1, 2, 2.5, 5].find((step) => step * base >= raw) ?? 10) * base;
}

export function axisTicks(start: number, end: number, step: number) {
	return Array.from({ length: Math.max(0, Math.floor((end - start) / step + 1e-8) + 1) }, (_, i) =>
		Number((start + i * step).toPrecision(10))
	);
}

export function elevationScale(low: number, high: number, divisions: number) {
	const pad = Math.max(1, (high - low) * 0.08);
	const step = tickStep((high - low + 2 * pad) / divisions);
	const min = Math.floor((low - pad) / step) * step;
	const max = Math.ceil((high + pad) / step) * step;
	return { min, max, ticks: axisTicks(min, max, step) };
}
