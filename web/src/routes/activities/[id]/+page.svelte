<script lang="ts">
	import MetricReadout from '#lib/components/MetricReadout.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import MetricStrip from '#lib/components/MetricStrip.svelte';
	import SortHeader from '#lib/components/SortHeader.svelte';
	import { compareSortedValues, promoteSort, type SortCriterion } from '#lib/stacked-sort.js';
	import { memberURL } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import {
		formatDate,
		formatDistance,
		formatDuration,
		formatElevation,
		formatHeartRate,
		formatPace,
		formatPower,
		formatSpeed,
		formatSpeedKmh,
		formatMetricDistance,
		formatTemp
	} from '#lib/format.js';
	import EstimatedPower from '#lib/components/EstimatedPower.svelte';
	import ActivityMap from '#lib/components/ActivityMap.svelte';
	import StreamChart from '#lib/components/StreamChart.svelte';
	import ElevationProfile from '#lib/components/ElevationProfile.svelte';
	import SportIcon from '#lib/components/SportIcon.svelte';

	import GearPrompt from '#lib/components/GearPrompt.svelte';
	import ActivityNav from '#lib/components/ActivityNav.svelte';
	import {
		recordValue,
		isCyclingDistanceRecord,
		recordMetric,
		recordWindow
	} from '#lib/records.js';
	import { goto, refreshAll } from '$app/navigation';
	import { page } from '$app/state';
	import { buildSeries } from './streamSeries';
	import type { PageData } from './$types';
	import type { SourceView } from './+layout';

	let { data }: { data: PageData } = $props();

	let a = $derived(data.activity);
	let stream = $derived(data.stream);
	let streamStatus = $derived(data.streamStatus);
	let bestEfforts = $derived(data.bestEfforts);
	const effortHistoryQuery = $derived(
		page.url.searchParams.get('exclude_downhills') === 'true' ? '?exclude_downhills=true' : ''
	);

	type SegEffort = {
		id: string;
		segment_id: string;
		segment_name: string;
		segment_distance_m: number;
		climb_category: string | null;
		elapsed_s: number;
		avg_heart_rate: number | null;
		avg_power: number | null;
		average_speed_kmh: number | null;
		local_kom_position?: number;
		is_personal_record: boolean;
		personal_rank: number;
		start_distance_m?: number;
		end_distance_m?: number;
	};
	let segmentEfforts = $state<SegEffort[]>([]);
	type EffortColumn =
		| 'segment_name'
		| 'segment_distance_m'
		| 'elapsed_s'
		| 'average_speed_kmh'
		| 'local_kom_position'
		| 'avg_heart_rate';
	let effortOrder = $state<SortCriterion<EffortColumn>[]>([]);
	let showPRs = $state(false);
	const achievements = $derived(segmentEfforts.filter((effort) => effort.is_personal_record));
	const sortedEfforts = $derived(
		[...segmentEfforts].sort((left, right) => {
			for (const { column, direction } of effortOrder) {
				const cmp = compareSortedValues(left[column], right[column], direction);
				if (cmp !== 0) return cmp;
			}
			return 0;
		})
	);
	let canRescan = $state(false);
	let rescanning = $state(false);
	let rescanQueued = $state(false);
	let segmentMessage = $state('');
	// Segment effort the pointer is over — shades its span on the elevation profile.
	let hoveredSeg = $state<SegEffort | null>(null);
	const segHighlight = $derived(
		hoveredSeg && hoveredSeg.start_distance_m != null && hoveredSeg.end_distance_m != null
			? { startDist: hoveredSeg.start_distance_m, endDist: hoveredSeg.end_distance_m }
			: null
	);

	type ZoneBand = { label: string; low_pct: number; high_pct: number; seconds: number };
	type ZoneSet = { basis: string; reference?: number; total_seconds: number; zones: ZoneBand[] };
	type Decoupling = { pct: number; basis: string; coupled: boolean };
	type Zones = { hr: ZoneSet | null; power: ZoneSet | null; decoupling?: Decoupling };
	let zones = $state<Zones | null>(null);

	type Attachment = { id: string; url: string; caption: string; content_type: string };
	let attachments = $state<Attachment[]>([]);
	let uploadingPhoto = $state(false);
	let photoInput = $state<HTMLInputElement | null>(null);

	async function loadAttachments(activityID: string, signal?: AbortSignal) {
		try {
			const res = await fetch(`/api/activities/${activityID}/attachments`, {
				signal: signal ?? null
			});
			if (!res.ok) return;
			const result = await res.json();
			if (activityID === a.id && !signal?.aborted) attachments = result.attachments ?? [];
		} catch {
			/* optional detail */
		}
	}

	async function uploadPhotos(files: FileList | null) {
		if (!files || files.length === 0) return;
		const activityID = a.id;
		uploadingPhoto = true;
		try {
			for (const file of Array.from(files)) {
				const fd = new FormData();
				fd.append('file', file);
				await fetch(`/api/activities/${activityID}/attachments`, { method: 'POST', body: fd });
			}
			await loadAttachments(activityID);
		} finally {
			uploadingPhoto = false;
		}
	}

	async function deleteAttachment(id: string) {
		const activityID = a.id;
		const res = await fetch(`/api/activities/${activityID}/attachments/${id}`, {
			method: 'DELETE'
		});
		if (res.ok && activityID === a.id) attachments = attachments.filter((x) => x.id !== id);
	}

	async function loadSegmentEfforts() {
		const activityID = a.id;
		try {
			const res = await fetch(`/api/activities/${activityID}/segment-efforts`);
			if (!res.ok) throw new Error();
			const result = await res.json();
			if (activityID === a.id) {
				segmentEfforts = result.efforts ?? [];
				canRescan = result.can_rescan === true;
				return true;
			}
		} catch {
			if (activityID === a.id && rescanQueued) segmentMessage = m.segment_refresh_failed();
		}
		return false;
	}

	async function rescanSegments() {
		const activityID = a.id;
		rescanning = true;
		try {
			const res = await fetch(`/api/activities/${activityID}/rescan-segments`, { method: 'POST' });
			if (!res.ok) throw new Error();
			if (activityID === a.id) {
				rescanQueued = true;
				segmentMessage = m.segment_rescan_queued();
			}
		} catch {
			if (activityID === a.id) segmentMessage = m.segment_rescan_failed();
		} finally {
			rescanning = false;
		}
	}

	$effect(() => {
		// Reset controls when navigating directly between activities.
		if (a.id) {
			segmentEfforts = [];
			showPRs = false;
			canRescan = false;
			rescanQueued = false;
			segmentMessage = '';
			void loadSegmentEfforts();
		}
	});

	async function loadRideStats(activityID: string, signal: AbortSignal) {
		const [zoneData, lapData] = await Promise.all(
			['zones', 'laps'].map(async (detail) => {
				try {
					const res = await fetch(`/api/activities/${activityID}/${detail}`, { signal });
					return res.ok ? await res.json() : null;
				} catch {
					return null;
				}
			})
		);
		if (signal.aborted) return;
		zones = zoneData;
		laps = lapData?.laps ?? [];
	}

	$effect(() => {
		const activityID = a.id;
		zones = null;
		laps = [];
		attachments = [];
		hoveredSeg = null;
		const controller = new AbortController();
		void loadRideStats(activityID, controller.signal);
		void loadAttachments(activityID, controller.signal);
		return () => controller.abort();
	});

	type Lap = {
		index: number;
		label: string;
		elapsed_s: number;
		moving_s: number;
		distance_m: number | null;
		avg_speed_mps: number | null;
		avg_heart_rate: number | null;
		avg_power: number | null;
		avg_cadence: number | null;
		elevation_gain_m: number | null;
	};
	let laps = $state<Lap[]>([]);
	const anyLapPower = $derived(laps.some((l) => l.avg_power != null));

	// Distinct colour per zone index (Z1 cool → Z7 hot).
	const ZONE_COLORS = ['#3b82f6', '#22c55e', '#eab308', '#f97316', '#ef4444', '#b91c1c', '#7f1d1d'];
	function zonePct(z: ZoneSet, seconds: number): number {
		return z.total_seconds > 0 ? (seconds / z.total_seconds) * 100 : 0;
	}
	const hasZones = $derived(
		!!zones &&
			((zones.hr && zones.hr.total_seconds > 0) || (zones.power && zones.power.total_seconds > 0))
	);

	// --- sources / detach ---
	const activeSources = $derived((a.sources ?? []).filter((s) => s.status !== 'detached'));
	let detaching = $state<string | null>(null);
	let sourceError = $state<string | null>(null);

	async function detachSource(s: SourceView) {
		const last = activeSources.length <= 1;
		const msg = last
			? 'This is the only remaining source. Detaching it will delete this activity. Continue?'
			: `Detach the ${s.provider} source? The activity will re-merge from its remaining sources.`;
		if (!confirm(msg)) return;
		detaching = s.id;
		sourceError = null;
		try {
			const res = await fetch(`/api/activities/${a.id}/sources/${s.id}/detach`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ reason: 'user_detach' })
			});
			if (!res.ok) throw new Error((await res.text()).trim() || res.statusText);
			const body = await res.json();
			if (body.soft_deleted) {
				await goto('/activities');
				return;
			}
			await refreshAll();
		} catch (e) {
			sourceError = (e as Error).message;
		} finally {
			detaching = null;
		}
	}

	function humanizeStatus(v: string): string {
		return v.replace(/_/g, ' ');
	}

	const sourceCount = $derived(a.sources?.length ?? 0);
	const sourcesLabel = $derived(
		sourceCount === 1
			? m.detail_sources_count({ count: sourceCount })
			: m.detail_sources_count_plural({ count: sourceCount })
	);

	const coordinates = $derived(stream?.coordinates ?? []);
	const offsets = $derived(stream?.offsets ?? []);
	const track = $derived(stream?.track ?? []);
	const hasElevation = $derived(track.some((p) => p.eleM != null));

	// Shared hover position (seconds offset) syncing the map marker and the
	// streams chart cursor — set by whichever the pointer is over.
	let hoverT = $state<number | null>(null);

	// Build chart series — only include channels that actually carry data
	// for this activity. Shared with the full-screen streams subpage.
	const series = $derived(buildSeries(stream));
	let streamChartWidth = $state(0);
	const stackStreams = $derived(series.length > 1 && streamChartWidth < 230 + series.length * 68);
	const streamSyncKey = $derived(`activity-streams-${a.id}`);

	// --- overview stats ---
	// Pace for foot/swim sports, speed for wheels.
	const speedSports = new Set(['ride', 'ebikeride', 'velomobile', 'handcycle', 'inline_skate']);
	const useSpeed = $derived(speedSports.has(a.type) || speedSports.has(a.discipline));

	const primaryPace = $derived(
		useSpeed
			? { label: m.col_average_speed(), value: formatSpeed(a.summary.avgSpeedMps) }
			: { label: m.col_average_pace(), value: formatPace(a.summary.avgSpeedMps) }
	);

	// Secondary stats — only those that actually have a value are rendered.
	const secondaryStats = $derived.by(() => {
		const s = a.summary;
		const out: { label: string; value: string }[] = [];
		const push = (label: string, v: string) => {
			if (v && v !== '–' && v !== '—') out.push({ label, value: v });
		};
		push(m.ride_elapsed(), formatDuration(a.elapsedDurationS));
		if (useSpeed) push(m.col_max_speed(), formatSpeed(s.maxSpeedMps));
		else push(m.col_average_speed(), formatSpeed(s.avgSpeedMps));
		push('Avg HR', formatHeartRate(s.avgHeartRateBpm));
		push('Max HR', formatHeartRate(s.maxHeartRateBpm));
		push('Avg power', formatPower(s.avgPowerW));
		push('Max power', formatPower(s.maxPowerW));
		if (s.normalizedPowerW != null) push('NP', formatPower(s.normalizedPowerW));
		if (s.avgCadence != null) push('Avg cadence', `${s.avgCadence}`);
		if (s.maxCadence != null) push('Max cadence', `${s.maxCadence}`);
		if (s.caloriesKcal != null) push('Calories', `${s.caloriesKcal} kcal`);
		if (s.elevationLossM != null) push('Descent', formatElevation(s.elevationLossM));
		if (s.maxElevationM != null) push('Max elevation', formatElevation(s.maxElevationM));
		if (s.tss != null) push('TSS', s.tss.toFixed(0));
		if (s.intensityFactor != null) push('Intensity', s.intensityFactor.toFixed(2));
		if (s.avgTemperatureC != null) push('Avg temp', formatTemp(s.avgTemperatureC));
		return out;
	});

	// Achievements summary from already-loaded data.
	const prCount = $derived(achievements.length);
	const segCount = $derived(segmentEfforts.length);
	const hasLocalKOM = $derived(segmentEfforts.some((effort) => effort.local_kom_position != null));
	const beCount = $derived(bestEfforts.length);
</script>

<section class="space-y-4 md:space-y-6">
	<header>
		<ActivityNav id={a.id} isOwner={data.isOwner} member={data.activityOwner} />
		<div class="surface mt-2 flex items-start gap-3 p-3 sm:gap-4 sm:p-5">
			<span
				class="flex size-11 shrink-0 items-center justify-center rounded-xl bg-accent-500/10 text-accent-300 sm:size-14"
				title={a.discipline || a.type}
			>
				<SportIcon type={a.discipline || a.type} size={30} />
			</span>
			<div class="min-w-0">
				<h1 class="page-title">
					{a.title || `${a.discipline || a.type} Activity`}
				</h1>
				<p class="mt-1 text-sm wrap-anywhere text-zinc-400">
					<span class="capitalize text-zinc-300">{a.discipline || a.type}</span>
					{#if a.customSubtype}· <span class="text-zinc-300">{a.customSubtype}</span>{/if}
					· {formatDate(a.startTime)}
					{#if a.startPlace}· {a.startPlace}{/if}
				</p>
				{#if data.isOwner && !a.gearId && a.gearSetBy !== 'user' && data.gear.length > 0}
					<GearPrompt activityId={a.id} type={a.type} gear={data.gear} />
				{/if}
				{#if a.gearId}
					<p class="mt-1 text-sm wrap-anywhere text-zinc-400">
						<a
							href={memberURL('/gear', data.activityOwner)}
							class="text-zinc-300 hover:text-accent-300">{a.gearName || m.gear_title()}</a
						>
						<span class="text-quiet">
							· {a.gearSetBy === 'user' ? m.gear_set_by_user() : m.gear_set_by_merge()}</span
						>
					</p>
				{/if}
				<p class="mt-0.5 text-xs text-quiet">{a.timezone} · {sourcesLabel}</p>
			</div>
		</div>
	</header>

	<MetricStrip
		label={m.activity_key_metrics()}
		metrics={[
			{
				label: m.detail_stat_distance(),
				value: formatDistance(a.summary.distanceM ?? null),
				icon: 'routes'
			},
			{
				label: m.detail_stat_moving(),
				shortLabel: m.detail_label_time(),
				value: formatDuration(a.movingDurationS),
				icon: 'clock'
			},
			{
				label: primaryPace.label,
				shortLabel: useSpeed ? m.metric_avg_speed_short() : m.metric_avg_pace_short(),
				value: primaryPace.value,
				icon: 'speed'
			},
			{
				label: m.detail_stat_elevation(),
				shortLabel: m.metric_elevation_gain_short(),
				value: formatElevation(a.summary.elevationGainM ?? null),
				icon: 'segments'
			}
		]}
	/>

	<!-- Achievements -->
	{#if beCount > 0 || segCount > 0}
		<div class="flex flex-wrap gap-2 text-xs">
			{#if prCount > 0}
				<button
					type="button"
					aria-expanded={showPRs}
					aria-controls="segment-prs"
					onclick={() => (showPRs = !showPRs)}
					class="action text-emerald-300"
				>
					<Icon name="records" size={16} />
					{prCount} segment PR{prCount === 1 ? '' : 's'}
				</button>
			{/if}
			{#if segCount > 0}
				<span class="inline-flex items-center rounded-lg bg-zinc-800 px-3 py-1 text-zinc-300">
					{segCount} segment effort{segCount === 1 ? '' : 's'}
				</span>
			{/if}
			{#if beCount > 0}
				<span class="inline-flex items-center rounded-lg bg-zinc-800 px-3 py-1 text-zinc-300">
					{beCount} best effort{beCount === 1 ? '' : 's'}
				</span>
			{/if}
		</div>
	{/if}

	{#if showPRs}
		<section id="segment-prs" aria-label={m.segment_personal_record()} class="surface p-3">
			<ul class="divide-y divide-zinc-800">
				{#each achievements as effort (effort.id)}
					<li
						class="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 px-1 py-2 text-sm"
					>
						<a
							href={memberURL(`/segment/${effort.segment_id}`, data.activityOwner)}
							class="min-w-0 truncate font-medium text-accent-300 hover:underline"
							title={effort.segment_name}>{effort.segment_name}</a
						>
						<span class="text-quiet">
							<MetricReadout
								value={`${formatMetricDistance(effort.segment_distance_m)} · ${formatDuration(effort.elapsed_s)} · ${formatSpeedKmh(effort.average_speed_kmh)}`}
							/>
							· {m.segment_personal_record()}{effort.personal_rank > 0
								? ` #${effort.personal_rank}`
								: ''}
						</span>
					</li>
				{/each}
			</ul>
		</section>
	{/if}

	<!-- Secondary stats: only those that carry a value. -->
	{#if secondaryStats.length > 0}
		<dl
			class="grid grid-cols-2 gap-x-3 gap-y-2 surface p-3 sm:grid-cols-3 sm:gap-x-6 sm:gap-y-3 sm:p-5 lg:grid-cols-4"
		>
			{#each secondaryStats as s (s.label)}
				<div
					class="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-0.5 border-b border-zinc-800/60 pb-1.5"
				>
					<dt class="text-xs text-zinc-500">{s.label}</dt>
					<dd class="metric-value text-sm font-medium text-zinc-200">{s.value}</dd>
				</div>
			{/each}
		</dl>
	{/if}

	{#if coordinates.length > 0}
		<section>
			<div class="mb-3 flex items-center justify-between">
				<h2 class="section-title">
					{m.detail_section_route()}
				</h2>
				<div class="flex items-center gap-4 text-sm text-accent-300">
					{#if a.type === 'ride' && track.length > 1}
						<a href={`/activities/${a.id}/replay`} class="hover:underline">{m.ride_replay()}</a>
					{/if}
					<a href={`/activities/${a.id}/map`} class="hover:underline">{m.ride_full_screen()} ↗</a>
				</div>
			</div>
			{#key a.id}
				<ActivityMap {coordinates} track={data.mapTrack} {hoverT} onHover={(t) => (hoverT = t)} />
			{/key}
		</section>
	{/if}

	{#if data.isOwner && a.type === 'ride' && !a.isEbike && !a.isVirtual && a.primaryStreamSourceId && track.length > 1 && a.summary.avgPowerW == null && a.summary.maxPowerW == null && a.summary.normalizedPowerW == null && !stream?.power && !data.mergedStream?.power}
		{#key `${a.id}/${a.gearId}`}
			<EstimatedPower
				sourceId={a.primaryStreamSourceId}
				otherSourceIds={a.sources
					.filter((source) => source.status === 'active' && !source.isPrimary)
					.map((source) => source.id)}
				activityId={a.id}
				userId={data.isOwner ? data.viewerId : null}
				{hoverT}
				onHover={(t) => (hoverT = t)}
			/>
		{/key}
	{/if}

	<!-- Photos / attachments -->
	<section>
		<div class="mb-2 flex items-center justify-between">
			<h2 class="text-sm font-medium uppercase tracking-wide text-zinc-500">Photos</h2>
			{#if data.isOwner}
				<button
					type="button"
					disabled={uploadingPhoto}
					onclick={() => photoInput?.click()}
					class="rounded border border-zinc-700 px-2.5 py-1 text-xs text-zinc-300 hover:border-accent-500 hover:text-accent-300 disabled:opacity-50"
				>
					{uploadingPhoto ? 'Uploading…' : '+ Add photos'}
				</button>
				<input
					bind:this={photoInput}
					type="file"
					accept="image/*"
					multiple
					class="hidden"
					onchange={(e) => uploadPhotos(e.currentTarget.files)}
				/>
			{/if}
		</div>
		{#if attachments.length > 0}
			<div class="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4">
				{#each attachments as att (att.id)}
					<div class="group relative overflow-hidden rounded-lg border border-zinc-800 bg-zinc-900">
						<img
							src={att.url}
							alt={att.caption || 'Activity photo'}
							loading="lazy"
							class="h-32 w-full object-cover"
						/>
						{#if data.isOwner}
							<button
								type="button"
								onclick={() => deleteAttachment(att.id)}
								title="Remove photo"
								aria-label="Remove photo"
								class="absolute right-1 top-1 hidden rounded bg-black/60 px-1.5 py-0.5 text-xs text-white hover:bg-red-600 group-hover:block"
							>
								✕
							</button>
						{/if}
					</div>
				{/each}
			</div>
		{:else}
			<p class="text-sm text-quiet">No photos yet.</p>
		{/if}
	</section>

	{#if series.length > 0}
		<section>
			<div class="mb-3 flex items-center justify-between">
				<h2 class="section-title">
					{m.detail_section_streams()}
				</h2>
				<a
					href={`/activities/${a.id}/streams`}
					class="text-xs text-accent-400 hover:text-accent-300"
				>
					Full screen ↗
				</a>
			</div>
			<div class="min-w-0 space-y-2" bind:clientWidth={streamChartWidth}>
				{#if stackStreams}
					{#each series as metric (metric.label)}
						<div class="surface min-w-0 px-1 pt-2">
							<h3 class="ml-2 text-xs font-semibold" style:color={metric.color}>{metric.label}</h3>
							<StreamChart
								{offsets}
								series={[metric]}
								height={120}
								{hoverT}
								syncKey={streamSyncKey}
								onHover={(t) => (hoverT = t)}
							/>
						</div>
					{/each}
				{:else}
					<StreamChart {offsets} {series} {hoverT} onHover={(t) => (hoverT = t)} />
				{/if}
			</div>
		</section>
	{/if}

	<!-- Explain a missing stream instead of silently dropping the sections. -->
	{#if series.length === 0 && coordinates.length === 0}
		{#if streamStatus === 'error'}
			<section
				class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-800/50 bg-amber-950/20 p-4 text-sm text-amber-200"
			>
				<span>Couldn't load this activity's streams.</span>
				<button
					type="button"
					onclick={() => refreshAll()}
					class="rounded border border-amber-700/50 px-3 py-1 text-xs hover:bg-amber-900/30"
				>
					Retry
				</button>
			</section>
		{:else if streamStatus === 'empty' || streamStatus === 'absent'}
			<section
				class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-dashed border-zinc-700 bg-zinc-900/30 p-4 text-sm text-zinc-400"
			>
				<span>
					No stream data for this activity{streamStatus === 'empty' ? ' yet' : ''} — no GPS track, elevation
					or sensor charts to show.
				</span>
				<a
					href={`/activities/${a.id}/manage`}
					class="rounded border border-zinc-700 px-3 py-1 text-xs text-zinc-300 hover:border-accent-500 hover:text-accent-300"
				>
					Re-fetch from provider
				</a>
			</section>
		{/if}
	{/if}

	{#if bestEfforts.length > 0}
		<section>
			<h2 class="section-title mb-3">Best Efforts</h2>
			<div class="table-scroll rounded-lg border border-zinc-800">
				<table class="data-table w-full text-sm">
					<thead class="bg-panel text-xs uppercase tracking-wide text-zinc-500">
						<tr>
							<th class="px-4 py-2 text-left">Metric</th>
							<th class="px-4 py-2 text-left">Window</th>
							<th class="px-4 py-2 text-right">Value</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each bestEfforts as b (b.id)}
							{@const bucket = {
								activity_type: a.type,
								metric: b.metric,
								window_kind: b.windowKind,
								window_value: b.windowValue
							}}
							<tr
								class="cursor-pointer bg-zinc-900/30 transition-colors hover:bg-zinc-800/60"
								onclick={() =>
									goto(
										memberURL(
											`/best-efforts/${a.type}/${b.metric}/${b.windowKind}/${b.windowValue}${effortHistoryQuery}`,
											data.activityOwner
										)
									)}
							>
								<td class="px-4 py-2 text-xs text-accent-400">
									{isCyclingDistanceRecord(bucket)
										? m.ride_elapsed()
										: recordMetric(b.metric, false)}
								</td>
								<td class="px-4 py-2 text-zinc-300">{recordWindow(b.windowKind, b.windowValue)}</td>
								<td class="px-4 py-2 text-right"
									><MetricReadout value={recordValue(bucket, b.achievedValue)} /> ↗</td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if laps.length > 1}
		<section>
			<h2 class="mb-3 text-sm font-medium text-zinc-300">
				Laps <span class="text-quiet">({laps.length})</span>
			</h2>
			<div class="table-scroll rounded-xl border border-zinc-800">
				<table class="data-table w-full text-sm">
					<thead class="bg-panel text-xs uppercase tracking-wide text-zinc-500">
						<tr>
							<th class="px-4 py-2 text-left font-medium">Lap</th>
							<th class="px-4 py-2 text-right font-medium">Distance</th>
							<th class="px-4 py-2 text-right font-medium">Time</th>
							<th class="px-4 py-2 text-right font-medium">Pace</th>
							<th class="px-4 py-2 text-right font-medium">HR</th>
							{#if anyLapPower}<th class="px-4 py-2 text-right font-medium">Power</th>{/if}
							<th class="px-4 py-2 text-right font-medium">Elev</th>
						</tr>
					</thead>
					<tbody>
						{#each laps as l, i (i)}
							<tr class="border-t border-zinc-800/70">
								<td class="px-4 py-2 font-medium text-zinc-200">{l.label || `Lap ${l.index}`}</td>
								<td class="numeric px-4 py-2 text-zinc-300">{formatDistance(l.distance_m)}</td>
								<td class="numeric px-4 py-2 text-zinc-300"
									>{formatDuration(l.moving_s || l.elapsed_s)}</td
								>
								<td class="numeric px-4 py-2 text-zinc-300">{formatPace(l.avg_speed_mps)}</td>
								<td class="numeric px-4 py-2 text-zinc-300">{formatHeartRate(l.avg_heart_rate)}</td>
								{#if anyLapPower}<td class="numeric px-4 py-2 text-zinc-300"
										>{formatPower(l.avg_power)}</td
									>{/if}
								<td class="numeric px-4 py-2 text-zinc-300"
									>{formatElevation(l.elevation_gain_m)}</td
								>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		</section>
	{/if}

	{#if zones?.decoupling}
		{@const dc = zones.decoupling}
		<section>
			<h2 class="mb-3 text-sm font-medium text-zinc-300">Aerobic decoupling</h2>
			<div class="flex flex-wrap items-center gap-4 surface p-4">
				<div
					class="text-3xl font-semibold tabular-nums {dc.coupled
						? 'text-emerald-400'
						: 'text-amber-400'}"
				>
					{dc.pct > 0 ? '+' : ''}{dc.pct.toFixed(1)}%
				</div>
				<div class="text-xs text-zinc-400">
					<div class="text-zinc-300">{dc.coupled ? 'Well coupled' : 'Notable drift'}</div>
					<div>
						{dc.basis === 'power' ? 'Power' : 'Pace'}:HR drift, 1st→2nd half. Lower is more
						aerobically efficient; under 5% is strong.
					</div>
				</div>
			</div>
		</section>
	{/if}

	{#if hasZones}
		<section>
			<h2 class="mb-3 text-sm font-medium text-zinc-300">Training zones</h2>
			<div class="grid gap-4 lg:grid-cols-2">
				{#each [{ key: 'hr', title: 'Heart rate', set: zones?.hr, unit: 'bpm' }, { key: 'power', title: 'Power', set: zones?.power, unit: 'W' }] as col (col.key)}
					{#if col.set && col.set.total_seconds > 0}
						{@const zset = col.set}
						<div class="surface p-4">
							<div class="mb-2 flex items-baseline justify-between">
								<h3 class="text-sm font-medium text-zinc-200">{col.title}</h3>
								{#if zset.reference}
									<span class="text-xs text-zinc-500">
										{zset.basis === 'lthr'
											? `vs LTHR ${zset.reference} bpm`
											: zset.basis === 'max'
												? `vs max HR ${zset.reference} bpm`
												: `vs FTP ${zset.reference} W`}
									</span>
								{/if}
							</div>
							<!-- stacked proportional bar -->
							<div class="mb-3 flex h-3 overflow-hidden rounded-full bg-zinc-800">
								{#each zset.zones as z, i (i)}
									{#if z.seconds > 0}
										<div
											style:width={`${zonePct(zset, z.seconds)}%`}
											style:background={ZONE_COLORS[i]}
											title={`${z.label}: ${formatDuration(z.seconds)}`}
										></div>
									{/if}
								{/each}
							</div>
							<ul class="space-y-1">
								{#each zset.zones as z, i (i)}
									<li class="flex items-center gap-2 text-xs">
										<span class="h-2.5 w-2.5 shrink-0 rounded-sm" style:background={ZONE_COLORS[i]}
										></span>
										<span class="w-28 shrink-0 text-zinc-300 max-md:w-20 max-md:truncate"
											>{z.label}</span
										>
										{#if zset.reference}
											<span class="metric-value w-24 shrink-0 text-zinc-500 max-md:hidden">
												{Math.round(z.low_pct * zset.reference)}{z.high_pct > 0
													? `–${Math.round(z.high_pct * zset.reference)}`
													: '+'}
												{col.unit}
											</span>
										{/if}
										<div class="h-1.5 flex-1 rounded-full bg-zinc-800">
											<div
												class="h-1.5 rounded-full"
												style:width={`${zonePct(zset, z.seconds)}%`}
												style:background={ZONE_COLORS[i]}
											></div>
										</div>
										<span class="metric-value min-w-16 shrink-0 text-right text-zinc-300"
											>{formatDuration(z.seconds)}</span
										>
										<span class="w-10 shrink-0 text-right tabular-nums text-zinc-500"
											>{Math.round(zonePct(zset, z.seconds))}%</span
										>
									</li>
								{/each}
							</ul>
						</div>
					{/if}
				{/each}
			</div>
		</section>
	{/if}

	{#if segmentEfforts.length > 0 || canRescan}
		<section>
			<div class="mb-3 flex flex-wrap items-center justify-between gap-2">
				<h2 class="section-title">
					{m.nav_segments()} <span class="text-quiet">({segmentEfforts.length})</span>
				</h2>
				{#if canRescan}
					<div class="flex items-center gap-3">
						{#if rescanQueued}
							<button
								type="button"
								class="text-sm text-accent-400 hover:underline"
								onclick={async () => {
									if (await loadSegmentEfforts()) segmentMessage = '';
								}}>{m.segment_refresh()}</button
							>
						{/if}
						<button
							type="button"
							class="text-sm text-accent-400 hover:underline disabled:opacity-50"
							disabled={rescanning}
							onclick={rescanSegments}>{m.segment_rescan()}</button
						>
					</div>
				{/if}
			</div>
			{#if segmentMessage}
				<p class="mb-3 text-sm text-quiet" role="status">{segmentMessage}</p>
			{/if}
			{#if segmentEfforts.length === 0}
				<p class="text-sm text-quiet">{m.segment_no_matches()}</p>
			{:else}
				<div class="table-scroll max-h-[28rem] overflow-y-auto rounded-lg border border-zinc-800">
					{#if hasElevation}
						<!-- Elevation profile pinned to the top of the scroll area; hovering a
					     row shades the segment's span on it. -->
						<div
							class="sticky top-0 z-10 border-b border-zinc-800 bg-zinc-950/95 px-2 pt-2 backdrop-blur"
						>
							<ElevationProfile {track} highlight={segHighlight} />
						</div>
					{/if}
					<table class="data-table w-full text-sm">
						<thead class="bg-panel text-xs uppercase tracking-wide text-zinc-500">
							<tr>
								{#each [{ column: 'segment_name', label: m.comparison_segment(), align: 'text-left' }, { column: 'segment_distance_m', label: m.col_distance() }, { column: 'elapsed_s', label: m.col_duration(), shortLabel: m.detail_label_time() }, { column: 'average_speed_kmh', label: m.segment_avg_speed(), shortLabel: m.segment_speed_column() }, ...(hasLocalKOM ? [{ column: 'local_kom_position', label: m.segment_local_kom() } as const] : []), { column: 'avg_heart_rate', label: m.col_avg_hr(), align: 'hidden md:table-cell text-right' }] satisfies { column: EffortColumn; label: string; shortLabel?: string; align?: string }[] as item (item.column)}
									{@const index = effortOrder.findIndex(
										(criterion) => criterion.column === item.column
									)}
									<SortHeader
										label={item.label}
										shortLabel={item.shortLabel}
										align={item.align ?? 'text-right'}
										{index}
										direction={effortOrder[index]?.direction}
										onclick={() => (effortOrder = promoteSort(effortOrder, item.column))}
									/>
								{/each}
							</tr>
						</thead>
						<tbody class="divide-y divide-zinc-800">
							{#each sortedEfforts as s (s.id)}
								<tr
									class="bg-zinc-900/30 transition-colors hover:bg-zinc-800/50"
									onmouseenter={() => (hoveredSeg = s)}
									onmouseleave={() => (hoveredSeg = null)}
								>
									<td class="name-cell px-4 py-2">
										<a
											href={memberURL(`/segment/${s.segment_id}`, data.activityOwner)}
											class="table-name text-accent-400 hover:text-accent-300 hover:underline"
											title={s.segment_name}
										>
											{s.segment_name}
										</a>
										{#if s.is_personal_record}
											<span
												class="ml-2 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-bold uppercase text-amber-300"
												title={m.segment_personal_record()}>PR</span
											>
										{/if}
										{#if s.climb_category}
											<span
												class="ml-1 rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] uppercase text-zinc-400"
											>
												{s.climb_category.replace('_', ' ')}
											</span>
										{/if}
									</td>
									<td class="numeric px-4 py-2 text-zinc-300">
										{formatMetricDistance(s.segment_distance_m)}
									</td>
									<td class="numeric px-4 py-2 text-zinc-100">
										{formatDuration(s.elapsed_s)}
									</td>
									<td class="numeric px-4 py-2 text-zinc-300">
										{formatSpeedKmh(s.average_speed_kmh)}
									</td>
									{#if hasLocalKOM}
										<td class="numeric px-4 py-2 text-zinc-300">
											{s.local_kom_position == null
												? m.placeholder_dash()
												: `#${s.local_kom_position}`}
										</td>
									{/if}
									<td class="numeric hidden px-4 py-2 text-zinc-300 md:table-cell">
										{s.avg_heart_rate ? formatHeartRate(s.avg_heart_rate) : m.placeholder_dash()}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>
	{/if}

	{#if a.description}
		<section>
			<h2 class="section-title mb-3">
				{m.detail_section_description()}
			</h2>
			<p class="text-sm wrap-anywhere whitespace-pre-line text-zinc-300">{a.description}</p>
		</section>
	{/if}

	{#if data.isOwner && a.sources.length > 0}
		<section>
			<h2 class="section-title mb-3">Sources</h2>
			<p class="mb-3 text-xs text-zinc-500">
				This activity is merged from {activeSources.length} source{activeSources.length === 1
					? ''
					: 's'}. Detach a source if it was matched here by mistake — the activity re-merges from
				the rest, or is deleted if it was the only one.
			</p>
			{#if sourceError}
				<div
					class="mb-2 rounded border border-red-700/50 bg-red-950/30 px-3 py-2 text-xs text-red-300"
				>
					{sourceError}
				</div>
			{/if}
			<ul class="divide-y divide-zinc-800 rounded-lg border border-zinc-800">
				{#each a.sources as s (s.id)}
					<li class="flex items-center justify-between gap-4 px-4 py-3 text-sm max-md:flex-wrap">
						<div class="min-w-0">
							<span class="font-medium text-zinc-200">{s.provider}</span>
							{#if s.isPrimary}
								<span
									class="ml-2 rounded bg-accent-500/15 px-1.5 py-0.5 text-xs text-accent-300"
									title="Maps and charts render from this source">primary</span
								>
							{/if}
							{#if s.status === 'detached'}
								<span class="ml-2 rounded bg-zinc-700/50 px-1.5 py-0.5 text-xs text-zinc-400"
									>detached</span
								>
							{:else if s.status !== 'active'}
								<span class="ml-2 rounded bg-amber-500/15 px-1.5 py-0.5 text-xs text-amber-300"
									>{humanizeStatus(s.status)}</span
								>
							{/if}
							{#if s.reimportStatus === 'update_available'}
								<span class="ml-2 rounded bg-sky-500/15 px-1.5 py-0.5 text-xs text-sky-300"
									>update available</span
								>
							{/if}
							<div class="mt-0.5 truncate text-xs text-quiet">
								{s.externalId ? `#${s.externalId} · ` : ''}imported {formatDate(
									s.importedAt
								)}{s.statusReason ? ` · ${humanizeStatus(s.statusReason)}` : ''}
							</div>
						</div>
						<div class="flex shrink-0 items-center gap-2">
							{#if s.hasRawBlob}
								<a
									href={`/api/activities/${a.id}/sources/${s.id}/download`}
									class="rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-400 hover:border-accent-500 hover:text-accent-300"
									title="Download the original imported file"
								>
									Download
								</a>
							{/if}
							{#if s.status !== 'detached'}
								<button
									type="button"
									disabled={detaching === s.id}
									onclick={() => detachSource(s)}
									class="rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-400 hover:border-red-600 hover:text-red-300 disabled:opacity-50"
								>
									{detaching === s.id ? 'Detaching…' : 'Detach'}
								</button>
							{/if}
						</div>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
</section>
