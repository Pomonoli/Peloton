import type { RecordedTrack, RoutePoint } from '#lib/recorded-routes.js';
import type { ElevationStats } from './types.js';

type RecordedElevation = (
	track: RecordedTrack,
	height: (point: RoutePoint) => number | null
) => ElevationStats;

export async function recordedElevation(): Promise<RecordedElevation> {
	const { WaasRouteMetrics } = await import('../../../static/waas-engine/route-metrics.js');
	return WaasRouteMetrics.elevation as RecordedElevation;
}
