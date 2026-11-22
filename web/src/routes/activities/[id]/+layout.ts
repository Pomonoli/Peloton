import { error, redirect } from '@sveltejs/kit';
import { connectClients, isUnauthenticated, Code, ConnectError } from '#lib/connect.js';
import {
	type Activity,
	type BestEffort,
	ActivityType,
	ActivitySourceStatus,
	GearSetBy,
	BestEffortMetric,
	BestEffortWindowKind,
	Discipline,
	ReimportStatus
} from '#lib/proto/cairn/v1/activity_pb.js';
import { enumName, protoDurationToSeconds, protoTimestampToISO } from '#lib/proto-helpers.js';
import { gearToView, type GearView } from '#lib/gear.js';
import { alignTrackSensors, streamToView, type ActivityStreamView } from '#lib/ride-stream.js';
export type { ActivityStreamView } from '#lib/ride-stream.js';
import type { LayoutLoad } from './$types';

export const load: LayoutLoad = async ({ fetch, url, params, parent }) => {
	const { user } = await parent();
	if (!user) {
		redirect(302, '/login');
	}

	const clients = connectClients(fetch, url.origin);

	let activity: Activity | undefined;
	try {
		const res = await clients.activity.getActivity({ id: params.id });
		activity = res.activity;
	} catch (err) {
		if (isUnauthenticated(err)) {
			redirect(302, '/login');
		}
		if (err instanceof ConnectError && err.code === Code.NotFound) {
			error(404, 'Activity not found');
		}
		error(500, (err as Error).message);
	}

	if (!activity) {
		error(404, 'Activity not found');
	}

	// Streams and best-efforts are independent — run them in parallel.
	const a = activity;
	const isOwner = a.userId === user.id;
	const [streamResult, bestEfforts, gear] = await Promise.all([
		loadStream(clients, a),
		clients.activity
			.listBestEfforts({ activityId: a.id })
			.then((res) => res.efforts.map(bestEffortToView))
			.catch((err) => {
				console.warn('ListBestEfforts failed', err);
				return [] as BestEffortView[];
			}),
		// For the "gear not set" prompt; failure just hides it.
		isOwner
			? clients.gear
					.listGear({ includeRetired: false })
					.then((res) => res.gear.map(gearToView))
					.catch(() => [] as GearView[])
			: []
	]);

	// Per-channel merged stream (best field from best source) — only worth
	// fetching when the activity has more than one source. Used by the charts;
	// the map keeps the primary GPS route.
	const merged =
		a.sources.length > 1
			? await loadMergedStream(fetch, url.origin, a, streamResult.stream?.track ?? [])
			: { stream: null, provenance: [], mapTrack: null };

	return {
		activity: activityToView(a),
		viewerId: user.id,
		isOwner,
		stream: streamResult.stream,
		mergedStream: merged.stream,
		mergedProvenance: merged.provenance,
		mapTrack: merged.mapTrack ?? streamResult.stream?.track ?? [],
		// streamStatus distinguishes the blank states so the page can explain
		// itself instead of silently dropping the map/streams/elevation:
		//   'ok'     — stream loaded with samples
		//   'empty'  — primary source exists but carries no samples yet
		//   'absent' — no primary stream source at all (nothing to load)
		//   'error'  — the stream request failed (offer a retry)
		streamStatus: streamResult.status,
		bestEfforts,
		gear
	};
};

type StreamStatus = 'ok' | 'empty' | 'absent' | 'error';

async function loadStream(
	clients: ReturnType<typeof connectClients>,
	a: Activity
): Promise<{ stream: ActivityStreamView | null; status: StreamStatus }> {
	if (!a.primaryStreamSourceId) {
		return { stream: null, status: 'absent' };
	}
	try {
		const res = await clients.activity.getActivityStream({
			activitySourceId: a.primaryStreamSourceId,
			maxResolutionHz: 0.2
		});
		const view = streamToView(res.stream);
		if (!view || view.sampleCount === 0) {
			return { stream: null, status: 'empty' };
		}
		return { stream: view, status: 'ok' };
	} catch (err) {
		console.warn('GetActivityStream failed', err);
		return { stream: null, status: 'error' };
	}
}

// ChannelProvenance: which provider supplied a rendered stream channel.
export interface ChannelProvenance {
	channel: string;
	provider: string;
}

// loadMergedStream fetches the per-channel-merged, aligned stream
// (GET /api/activities/{id}/merged-stream) and projects it into the same
// ActivityStreamView the charts consume. Returns a null stream on any failure
// (the caller falls back to the single primary stream). The primary GPS
// geometry can use sensor data from other active sources on the same clock.
async function loadMergedStream(
	fetch: typeof globalThis.fetch,
	origin: string,
	a: Activity,
	track: ActivityStreamView['track']
): Promise<{
	stream: ActivityStreamView | null;
	provenance: ChannelProvenance[];
	mapTrack: ActivityStreamView['track'] | null;
}> {
	try {
		const res = await fetch(`${origin}/api/activities/${a.id}/merged-stream`);
		if (!res.ok) return { stream: null, provenance: [], mapTrack: null };
		const b = (await res.json()) as {
			grid: number[];
			primary_start?: number;
			channels: Record<string, (number | null)[]>;
			provenance: Record<string, string>;
		};
		const grid = b.grid ?? [];
		const ch = b.channels ?? {};
		const base = grid[0];
		if (base === undefined) return { stream: null, provenance: [], mapTrack: null };
		const offsets = grid.map((t) => t - base);
		const pick = (name: string) => (ch[name] && ch[name].length ? ch[name] : null);
		const hr = pick('heart_rate');
		const power = pick('power');
		const cadence = pick('cadence');
		const speed = pick('speed');
		const altitude = pick('altitude');
		if (!hr && !power && !cadence && !speed && !altitude) {
			return { stream: null, provenance: [], mapTrack: null };
		}

		const stream: ActivityStreamView = {
			sourceID: '',
			resolutionHz: 0,
			sampleCount: grid.length,
			coordinates: [],
			offsets,
			hr,
			power,
			cadence,
			speed,
			altitude,
			track: []
		};

		// Map source ids → provider labels for the "merged from" caption.
		const providerBySource = new Map(a.sources.map((s) => [s.id, s.provider]));
		const provenance: ChannelProvenance[] = Object.entries(b.provenance ?? {})
			.map(([channel, sid]) => ({ channel, provider: providerBySource.get(sid) ?? 'unknown' }))
			.sort((x, y) => x.channel.localeCompare(y.channel));

		const active = new Set(
			a.sources.filter((s) => s.status === ActivitySourceStatus.ACTIVE).map((s) => s.id)
		);
		const sensor = (name: string) => (active.has(b.provenance?.[name] ?? '') ? pick(name) : null);
		const mapTrack =
			typeof b.primary_start === 'number'
				? alignTrackSensors(track, grid, b.primary_start, sensor('heart_rate'), sensor('power'))
				: null;
		return { stream, provenance, mapTrack };
	} catch (err) {
		console.warn('merged-stream load failed', err);
		return { stream: null, provenance: [], mapTrack: null };
	}
}

// ---------------------------------------------------------------------------
// View types — what the .svelte file consumes. Projecting here keeps the
// component free of bigint timestamps and @bufbuild/protobuf machinery.
// ---------------------------------------------------------------------------

export interface ActivityView {
	id: string;
	title: string;
	description: string;
	type: string;
	discipline: string;
	isVirtual: boolean;
	isEbike: boolean;
	isCommute: boolean;
	isRace: boolean;
	customSubtype: string;
	startTime: string;
	timezone: string;
	movingDurationS: number;
	elapsedDurationS: number;
	primaryStreamSourceId: string | null;
	startPlace: string;
	gearId: string;
	gearName: string;
	gearSetBy: string;
	sources: SourceView[];
	summary: {
		distanceM: number | null;
		elevationGainM: number | null;
		elevationLossM: number | null;
		minElevationM: number | null;
		maxElevationM: number | null;
		avgSpeedMps: number | null;
		maxSpeedMps: number | null;
		avgHeartRateBpm: number | null;
		maxHeartRateBpm: number | null;
		avgPowerW: number | null;
		maxPowerW: number | null;
		normalizedPowerW: number | null;
		avgCadence: number | null;
		maxCadence: number | null;
		avgTemperatureC: number | null;
		caloriesKcal: number | null;
		tss: number | null;
		intensityFactor: number | null;
	};
	hasStream: boolean;
}

export interface SourceView {
	id: string;
	provider: string;
	externalId: string;
	status: string; // enum name, e.g. "active" | "detached"
	statusReason: string;
	reimportStatus: string;
	importedAt: string;
	isPrimary: boolean;
	hasRawBlob: boolean;
}

export interface BestEffortView {
	id: string;
	metric: string;
	windowKind: string;
	windowValue: number;
	achievedValue: number;
	distanceM?: number | undefined;
	durationS: number;
	startOffsetS: number;
}

function activityToView(a: Activity): ActivityView {
	return {
		id: a.id,
		title: a.title,
		description: a.description,
		type: enumName(ActivityType, a.type),
		discipline: enumName(Discipline, a.discipline),
		isVirtual: a.isVirtual,
		isEbike: a.isEbike,
		isCommute: a.isCommute,
		isRace: a.isRace,
		customSubtype: a.customSubtype,
		startTime: protoTimestampToISO(a.startTime),
		timezone: a.timezone,
		movingDurationS: protoDurationToSeconds(a.movingDuration),
		elapsedDurationS: protoDurationToSeconds(a.elapsedDuration),
		primaryStreamSourceId: a.primaryStreamSourceId || null,
		startPlace: a.startPlace,
		gearId: a.gearId,
		gearName: a.gearName,
		gearSetBy: enumName(GearSetBy, a.gearSetBy) || 'merge',
		sources: a.sources.map((s) => ({
			id: s.id,
			provider: s.provider,
			externalId: s.externalId,
			status: enumName(ActivitySourceStatus, s.status),
			statusReason: s.statusReason,
			reimportStatus: enumName(ReimportStatus, s.reimportStatus),
			importedAt: protoTimestampToISO(s.importedAt),
			isPrimary: !!a.primaryStreamSourceId && s.id === a.primaryStreamSourceId,
			hasRawBlob: !!s.rawBlobId
		})),
		summary: {
			distanceM: a.summary?.distanceM ?? null,
			elevationGainM: a.summary?.elevationGainM ?? null,
			elevationLossM: a.summary?.elevationLossM ?? null,
			minElevationM: a.summary?.minElevationM ?? null,
			maxElevationM: a.summary?.maxElevationM ?? null,
			avgSpeedMps: a.summary?.avgSpeedMps ?? null,
			maxSpeedMps: a.summary?.maxSpeedMps ?? null,
			avgHeartRateBpm: a.summary?.avgHeartRateBpm ?? null,
			maxHeartRateBpm: a.summary?.maxHeartRateBpm ?? null,
			avgPowerW: a.summary?.avgPowerW ?? null,
			maxPowerW: a.summary?.maxPowerW ?? null,
			normalizedPowerW: a.summary?.normalizedPowerW ?? null,
			avgCadence: a.summary?.avgCadence ?? null,
			maxCadence: a.summary?.maxCadence ?? null,
			avgTemperatureC: a.summary?.avgTemperatureC ?? null,
			caloriesKcal: a.summary?.caloriesKcal ?? null,
			tss: a.summary?.tss ?? null,
			intensityFactor: a.summary?.intensityFactor ?? null
		},
		hasStream: !!a.primaryStreamSourceId
	};
}

function bestEffortToView(b: BestEffort): BestEffortView {
	return {
		id: b.id,
		metric: enumName(BestEffortMetric, b.metric),
		windowKind: enumName(BestEffortWindowKind, b.windowKind),
		windowValue: b.windowValue,
		achievedValue: b.achievedValue,
		distanceM: b.distanceM,
		durationS: protoDurationToSeconds(b.duration),
		startOffsetS: b.startOffset
	};
}
