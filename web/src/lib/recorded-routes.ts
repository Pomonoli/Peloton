import { intlLocale } from '#lib/format.js';
import { m } from '#lib/paraglide/messages.js';
import type { RidePoint } from '#lib/ride-track.js';
import type { MemberView } from '#lib/member.js';

export type RouteKind = 'loop' | 'out_and_back' | 'point_to_point';

export interface RoutePoint {
	lat: number;
	lon: number;
	elevation_m?: number;
	distance_m: number;
}
export type RecordedTrack = RoutePoint[][];

export interface RouteRider extends MemberView {
	avatar_url: string;
	rides: number;
	best_speed_kmh: number | null;
}

export interface RecordedRoute {
	id: string;
	name: string;
	type: RouteKind;
	distance_m: number;
	elevation_gain_m: number | null;
	rides: number;
	rider_count: number;
	best_speed_kmh: number | null;
	track: RecordedTrack;
	riders: RouteRider[];
}

export interface MatchingRide {
	id: string;
	rider_id: string;
	title: string;
	start_time: string;
	timezone: string;
	distance_m: number | null;
	speed_kmh: number | null;
}

export interface RouteDetail extends RecordedRoute {
	recent: MatchingRide[];
}

export function recordedRoutePath(id: string) {
	return '/api/routes/' + encodeURIComponent(id);
}

export function recordedRoutePlannerPath(id: string) {
	return '/routes/waasland/recorded/' + encodeURIComponent(id);
}

export async function routeData<T>(url: string, signal: AbortSignal): Promise<T> {
	const response = await fetch(url, { signal });
	if (!response.ok) throw new Error(String(response.status));
	return response.json() as Promise<T>;
}

export function routeKind(kind: RouteKind) {
	switch (kind) {
		case 'loop':
			return m.routes_loop();
		case 'out_and_back':
			return m.routes_out_and_back();
		case 'point_to_point':
			return m.routes_point_to_point();
	}
}

export function routeDistance(meters: number | null) {
	return meters == null
		? '—'
		: new Intl.NumberFormat(intlLocale(), {
				style: 'unit',
				unit: 'kilometer',
				maximumFractionDigits: meters < 1000 ? 2 : 1
			}).format(meters / 1000);
}

export function routeElevation(meters: number | null) {
	return meters == null
		? '—'
		: new Intl.NumberFormat(intlLocale(), {
				style: 'unit',
				unit: 'meter',
				maximumFractionDigits: 0
			}).format(meters);
}

export function riderName(rider: MemberView) {
	return rider.display_name.trim() || rider.username;
}

export function previewPoints(track: RecordedTrack): RidePoint[] {
	return track.flatMap((line) =>
		line.map((p, i) => ({
			t: 0,
			lat: p.lat,
			lon: p.lon,
			connected: i > 0,
			distM: p.distance_m,
			eleM: p.elevation_m ?? null,
			speedKmh: null,
			hr: null,
			power: null,
			cadence: null,
			gradient: null
		}))
	);
}

// Unwrap longitudes around the recorded start, so a date-line crossing
// doesn't span the full world on the interactive map.
export function mapLines(track: RecordedTrack): [number, number][][] {
	const origin = track[0]?.[0]?.lon ?? 0;
	return track.map((line) =>
		line.map((p) => [origin + ((((p.lon - origin + 180) % 360) + 360) % 360) - 180, p.lat])
	);
}
