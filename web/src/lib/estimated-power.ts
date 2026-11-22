import type { RidePoint } from '#lib/ride-track.js';

export interface PowerParameters {
	riderKg: number;
	equipmentKg: number;
	crr: number;
	cdaM2: number;
	headwindMps?: number;
}

export const defaultResistance = { crr: 0.005, cdaM2: 0.35 };

export function estimatePower(track: RidePoint[], params: PowerParameters) {
	const { riderKg, equipmentKg, crr, cdaM2, headwindMps = 0 } = params;
	if (!(
		riderKg >= 20 &&
		riderKg <= 300 &&
		equipmentKg >= 2 &&
		equipmentKg <= 100 &&
		crr >= 0.001 &&
		crr <= 0.03 &&
		cdaM2 >= 0.1 &&
		cdaM2 <= 1.5 &&
		Math.abs(headwindMps) <= 25
	))
		return null;
	// Keep estimates isolated even if a caller returns a different stream.
	if (track.some((p) => p.power != null)) return null;
	const mass = riderKg + equipmentKg;
	const gravity = 9.80665;
	const airDensity = 1.225; // kg/m³, standard sea-level conditions
	const efficiency = 0.97;
	let joules = 0;
	let validSeconds = 0;
	const values = track.map((point, i) => {
		const prev = track[i - 1];
		if (!prev || !point.connected || prev.speedKmh == null || point.speedKmh == null) return null;
		const dt = point.t - prev.t;
		if (!(dt >= 0.25 && dt <= 15)) return null;
		const speed = (point.speedKmh + prev.speedKmh) / 7.2;
		if (speed < 0.75 || point.gradient == null) return null;
		const slope = Math.atan(point.gradient / 100);
		const rolling = mass * gravity * crr * Math.cos(slope);
		const climbing = mass * gravity * Math.sin(slope);
		const airSpeed = speed + headwindMps;
		const drag = 0.5 * airDensity * cdaM2 * airSpeed * Math.abs(airSpeed);
		const acceleration = (point.speedKmh - prev.speedKmh) / (3.6 * dt);
		const inertia = Math.abs(acceleration) <= 0.8 ? mass * acceleration : 0;
		const power = Math.max(0, ((rolling + climbing + drag + inertia) * speed) / efficiency);
		validSeconds += dt;
		joules += power * dt;
		return Math.round(power);
	});
	const duration = (track.at(-1)?.t ?? 0) - (track[0]?.t ?? 0);
	if (validSeconds < 60 || validSeconds / duration < 0.6) return null;
	return { values, averageW: Math.round(joules / validSeconds), coverage: validSeconds / duration };
}
