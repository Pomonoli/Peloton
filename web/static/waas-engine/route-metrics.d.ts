import type { ElevationStats, Point } from '../../src/lib/waas/types.js';

export const WaasRouteMetrics: {
 distance(a: readonly number[], b: readonly number[]): number;
 gain(points: readonly Point[], first?: number, last?: number): number;
 measure(input: readonly (readonly number[])[]): { points: Point[]; meters: number; gain: number };
 elevation(lines: readonly (readonly Point[])[]): ElevationStats;
 elevation<T>(lines: readonly (readonly T[])[], height: (point: T) => number | null): ElevationStats;
};
