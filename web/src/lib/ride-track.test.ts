import { describe, expect, it } from 'vitest';
import { create } from '@bufbuild/protobuf';
import { ActivityStreamSchema, StreamChannel } from '#lib/proto/cairn/v1/activity_pb.js';
import {
	analyzeTrack,
	metricLegend,
	metricRoute,
	pointAtTime,
	type TrackPoint
} from './ride-track';
import { alignTrackSensors, streamToView } from './ride-stream';
import { defaultResistance, estimatePower } from './estimated-power';
import { metricLabel, metricValue } from './ride-display';

function ride(gradient = 0, withSensors = false): TrackPoint[] {
	return Array.from({ length: 241 }, (_, i) => ({
		t: i,
		lat: 50.8 + i * 0.00009,
		lon: 4.4,
		distM: i * 10,
		eleM: 100 + (i * 10 * gradient) / 100,
		speedKmh: withSensors ? 36 : null,
		hr: withSensors && i < 120 ? 120 + Math.floor(i / 10) : null,
		power: withSensors && i > 120 ? 200 + i : null,
		cadence: null
	}));
}

describe('ride map and replay', () => {
	it('colors different intervals using real values and preserves partial sensor gaps', () => {
		const track = analyzeTrack(ride(6, true));
		expect(track[150]!.gradient).toBeCloseTo(6);
		expect(track[150]!.speedKmh).toBeCloseTo(36);
		expect(metricLegend(track, 'hr').map((entry) => entry.value)).toEqual([
			120, 122.75, 125.5, 128.25, 131
		]);
		expect(metricLegend(track, 'power').at(-1)?.value).toBe(440);
		const route = metricRoute(track, 'hr');
		expect(route.features[0]?.properties).toEqual({ value: 120, index: 1 });
		expect(route.features[150]?.properties?.value).toBeNull();
		expect(route.features[0]?.geometry.coordinates).toEqual([
			[4.4, 50.8],
			[4.4, 50.80009]
		]);
		expect(metricLegend(track, 'speedKmh')).toHaveLength(1);
	});

	it('derives speed and distance and suppresses short GPS, speed and altitude spikes', () => {
		const samples = ride(5).map((p) => ({ ...p, distM: null }));
		samples[130] = { ...samples[130]!, lat: 51.8, eleM: 2500, speedKmh: 1500 };
		samples[160] = { ...samples[160]!, eleM: 3500, speedKmh: 110 };
		const track = analyzeTrack(samples);
		expect(track[100]!.distM).toBeCloseTo(1000.75, 0);
		expect(track[100]!.speedKmh).toBeCloseTo(36.03, 1);
		expect(track[130]).toMatchObject({ connected: false, speedKmh: null, gradient: null });
		expect(track[160]!.eleM).toBeLessThan(185);
		expect(track[160]!.speedKmh).toBeLessThan(40);
		expect(Math.abs(track[160]!.gradient! - 5)).toBeLessThan(1);
		expect(metricLegend(track, 'hr')).toEqual([]);
		expect(metricLegend(track, 'power')).toEqual([]);
	});

	it('uses a zero-centered gradient legend when the ride climbs and descends', () => {
		const points = ride(5);
		for (let i = 120; i < points.length; i++) points[i]!.eleM = 160 - (i - 120) * 0.6;
		const legend = metricLegend(analyzeTrack(points), 'gradient');
		expect(legend[0]!.value).toBeCloseTo(-6);
		expect(legend[2]!.value).toBe(0);
		expect(legend.at(-1)!.value).toBeCloseTo(5);
		expect(metricValue('gradient', legend[0]!.value)).toContain('-6');
		expect(metricValue('hr', 130)).toBe('130 bpm');
		expect(metricValue('speedKmh', 36.22)).toBe('36.2 km/h');
		expect(metricLabel.power()).toBe('Power');
	});

	it('keeps irregular recorded timestamps and holds the prior GPS point in a pause', () => {
		const points = ride().slice(0, 4);
		points[1]!.t = 0.6;
		points[2]!.t = 42;
		points[3]!.t = 42.9;
		const track = analyzeTrack([...points, points[3]!, { ...points[3]!, t: 50, lat: 95 }]);
		expect(track.map((p) => p.t)).toEqual([0, 0.6, 42, 42.9]);
		expect(track[2]).toMatchObject({ connected: false, distM: 20, gradient: null });
		expect(pointAtTime(track, 20)).toBe(1);
		expect(pointAtTime(track, 42)).toBe(2);
		expect(pointAtTime(track, 500)).toBe(3);
		expect(metricRoute(track, null).features.map((f) => f.properties?.index)).toEqual([1, 3]);
	});

	it('decodes nullable stream channels and aligns a secondary sensor without moving the GPS route', () => {
		const raw = create(ActivityStreamSchema, {
			activitySourceId: 'source',
			resolutionHz: 1,
			sampleCount: 4,
			channels: [
				StreamChannel.LATITUDE,
				StreamChannel.LONGITUDE,
				StreamChannel.HEART_RATE,
				StreamChannel.POWER,
				StreamChannel.ALTITUDE
			],
			timeS: [0, 5, 10, 15],
			latitude: [50.8, 50.81, Number.NaN, 50.82],
			longitude: [4.4, 4.41, 4.42, 4.43],
			altitudeM: [50, Number.NaN, 60, 70],
			heartRateBpm: [130, 0, 0, 0],
			powerW: [200, 0, 0, 0]
		});
		expect(streamToView(undefined)).toBeNull();
		const view = streamToView(raw)!;
		expect(view.offsets).toEqual([0, 5, 10, 15]);
		expect(view.altitude).toEqual([50, null, 60, 70]);
		expect(view.track.map((p) => p.t)).toEqual([0, 5, 15]);
		const aligned = alignTrackSensors(
			view.track,
			[100, 105, 110],
			100,
			[140, 150, 160],
			[250, 0, 280]
		);
		expect(aligned.map((p) => [p.t, p.hr, p.power])).toEqual([
			[0, 130, 200],
			[5, 150, 0],
			[15, null, null]
		]);
		expect(aligned.map((p) => [p.lon, p.lat])).toEqual(view.coordinates);
		expect(alignTrackSensors(view.track, [], 100, null, null)).toBe(view.track);
	});
	it('decodes speed and cadence and preserves nullable channels and remote sensor gaps', () => {
		const sensorOnly = create(ActivityStreamSchema, {
			channels: [StreamChannel.SPEED, StreamChannel.CADENCE],
			speedMps: [10, Number.NaN],
			cadence: [90, 0],
			sampleCount: 2,
			timeS: [0, 5]
		});
		expect(streamToView(sensorOnly)).toMatchObject({
			track: [],
			altitude: null,
			hr: null,
			power: null,
			speed: [10, null],
			cadence: [90, null]
		});
		const gps = create(ActivityStreamSchema, {
			channels: [
				StreamChannel.LATITUDE,
				StreamChannel.LONGITUDE,
				StreamChannel.DISTANCE,
				StreamChannel.SPEED,
				StreamChannel.CADENCE
			],
			speedMps: [10],
			cadence: [90],
			distanceM: [0, 50],
			sampleCount: 4,
			timeS: [0, 5, 10, 15],
			latitude: [50, 50.01, 50.02],
			longitude: [4.0, 4.1, 4.2]
		});
		const track = streamToView(gps)!.track;
		expect(track).toHaveLength(3);
		expect(track[0]).toMatchObject({
			cadence: 90,
			speedKmh: 36,
			distM: 0,
			eleM: null,
			hr: null,
			power: null
		});
		expect(track[1]).toMatchObject({ cadence: null, speedKmh: null, distM: 50 });
		expect(track[2]!.distM).toBeNull();
		expect(alignTrackSensors(track, [50], 50, null, null)).toBe(track);
		expect(alignTrackSensors(track, [50], 50, [135], null)[0]).toMatchObject({
			hr: 135,
			power: null
		});
		expect(alignTrackSensors(track, [50], 50, null, [200])[0]).toMatchObject({
			hr: null,
			power: 200
		});
		expect(alignTrackSensors(track, [50], 50, [null], [null])[0]).toMatchObject({
			hr: null,
			power: null
		});
	});
});

describe('Estimated Power', () => {
	const settings = { riderKg: 75, equipmentKg: 10, ...defaultResistance };

	it('matches the documented flat-road model and increases on a climb or into a headwind', () => {
		const flat = analyzeTrack(ride());
		const result = estimatePower(flat, settings);
		expect(result?.averageW).toBe(264);
		expect(result?.values[0]).toBeNull();
		expect(result?.values[100]).toBe(264);
		expect(result?.coverage).toBe(1);
		expect(estimatePower(analyzeTrack(ride(6)), settings)?.averageW).toBeGreaterThan(740);
		expect(estimatePower(flat, { ...settings, headwindMps: 4 })?.averageW).toBeGreaterThan(440);
		expect(estimatePower(flat, { ...settings, headwindMps: -4 })?.averageW).toBeLessThan(150);
		expect(estimatePower(analyzeTrack(ride(-8)), settings)?.averageW).toBe(0);
	});

	it('uses modest acceleration and ignores abrupt speed changes', () => {
		const track = analyzeTrack(ride());
		track[100]!.speedKmh = 36.9;
		track[160]!.speedKmh = 51;
		const result = estimatePower(track, settings)!;
		expect(result.values[100]).toBeGreaterThan(450);
		expect(result.values[160]).toBeLessThan(500);
	});

	it('declines measured, incomplete, short, and sparsely timed rides', () => {
		expect(estimatePower([], settings)).toBeNull();
		expect(estimatePower(analyzeTrack(ride(0, true)), settings)).toBeNull();
		expect(
			estimatePower(
				analyzeTrack(ride().map((p, i) => ({ ...p, t: p.t + (i > 120 ? 600 : 0) }))),
				settings
			)
		).toBeNull();
		expect(
			estimatePower(analyzeTrack(ride().map((p) => ({ ...p, eleM: null }))), settings)
		).toBeNull();
		expect(estimatePower(analyzeTrack(ride().slice(0, 45)), settings)).toBeNull();
		expect(
			estimatePower(analyzeTrack(ride().map((p) => ({ ...p, t: p.t * 25 }))), settings)
		).toBeNull();
		expect(estimatePower(analyzeTrack(ride()), { ...settings, riderKg: Number.NaN })).toBeNull();
	});
});
