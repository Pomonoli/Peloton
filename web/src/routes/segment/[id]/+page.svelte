<script lang="ts">
	import { memberURL } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import { page } from '$app/state';
	import SegmentRivalries from '#lib/components/SegmentRivalries.svelte';
	import ActivityMap from '#lib/components/ActivityMap.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import MetricStrip from '#lib/components/MetricStrip.svelte';
	import SectionTabs from '#lib/components/SectionTabs.svelte';
	import StreamChart from '#lib/components/StreamChart.svelte';
	import { decodePolylineLonLat } from '#lib/polyline.js';
	import { alignEffortHistory, segmentSpeedRange } from '#lib/segment-rivalries.js';
	import {
		formatMetricDistance,
		formatDistanceEffort,
		segmentSpeedKmh,
		formatDate,
		formatDuration,
		formatSpeedKmh,
		intlLocale,
		formatElevation
	} from '#lib/format.js';

	let { data } = $props();

	type Segment = {
		id: string;
		name: string;
		source: string;
		distance_m: number;
		activity_type: string;
		climb_category: string | null;
		polyline: string;
		polyline_precision: number;
		elevation_gain_m: number | null;
		avg_grade: number | null;
		max_grade: number | null;
	};
	type Effort = {
		id: string;
		activity_id: string;
		start_time: string;
		elapsed_s: number;
		moving_s: number;
		avg_heart_rate: number | null;
		avg_power: number | null;
		personal_rank: number;
		is_personal_record: boolean;
	};

	let segment = $state<Segment | null>(null);
	let efforts = $state<Effort[]>([]);
	let loading = $state(true);
	let failure = $state<'not-found' | 'request' | null>(null);
	const tabs = $derived([
		{ key: 'overview', label: m.segment_overview() },
		{ key: 'leaderboard', label: m.segment_leaderboard_tab() },
		{ key: 'attempts', label: m.segment_attempts() }
	] as const);
	let tab = $state<'overview' | 'leaderboard' | 'attempts'>('overview');
	let hovered = $state<number | null>(null);
	const attemptPageSize = 25;
	// The detail endpoint returns at most this many attempts.
	const detailAttemptLimit = 1000;
	let visibleAttempts = $state(attemptPageSize);
	let retry = $state(0);
	const segmentId = $derived(page.params.id);
	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 1 }));
	const detailURL = $derived(segmentId && memberURL(`/api/segments/${segmentId}`, data.member));

	async function loadSegment(url: string, signal: AbortSignal) {
		try {
			const res = await fetch(url, { signal });
			if (res.status === 404) {
				if (!signal.aborted) failure = 'not-found';
				return;
			}
			if (!res.ok) throw new Error();
			const result = await res.json();
			if (signal.aborted) return;
			segment = result.segment;
			efforts = result.efforts ?? [];
		} catch {
			if (!signal.aborted) failure = 'request';
		} finally {
			if (!signal.aborted) loading = false;
		}
	}

	$effect(() => {
		const url = detailURL;
		if (!url) return;
		void retry;
		loading = true;
		failure = null;
		segment = null;
		efforts = [];
		tab = 'overview';
		visibleAttempts = attemptPageSize;
		hovered = null;
		const controller = new AbortController();
		void loadSegment(url, controller.signal);
		return () => controller.abort();
	});

	const coordinates = $derived(
		segment?.polyline ? decodePolylineLonLat(segment.polyline, segment.polyline_precision || 5) : []
	);

	const best = $derived(
		efforts.find((attempt) => attempt.is_personal_record && attempt.elapsed_s > 0) ??
			efforts.find((attempt) => attempt.elapsed_s > 0)
	);
	const bestLabel = $derived(
		data.member
			? m.segment_rider_best({ rider: data.member.display_name || data.member.username })
			: m.segment_your_best()
	);
	const trend = $derived(alignEffortHistory([efforts], segment?.distance_m ?? 0));
	const trendSeries = $derived([
		{ label: m.segment_speed_column(), color: 'var(--plot-speed)', values: trend.values[0] ?? [] }
	]);
	const inspected = $derived(
		hovered === null ? undefined : trend.times[0]?.[trend.offsets.indexOf(hovered)]
	);

	const trace = $derived.by(() => {
		if (coordinates.length < 2) return '';
		const first = coordinates[0]!;
		const latitudeScale = Math.cos((first[1] * Math.PI) / 180);
		const points = coordinates.map(([lon, lat]) => {
			const longitudeDelta = ((lon - first[0] + 540) % 360) - 180;
			return [longitudeDelta * latitudeScale, first[1] - lat] as const;
		});
		let left = 0,
			right = 0,
			top = 0,
			bottom = 0;
		for (const [x, y] of points) {
			left = Math.min(left, x);
			right = Math.max(right, x);
			top = Math.min(top, y);
			bottom = Math.max(bottom, y);
		}
		const scale = Math.min(240 / (right - left || 1e-9), 104 / (bottom - top || 1e-9));
		return points
			.map(
				([x, y]) =>
					`${140 + (x - (left + right) / 2) * scale},${64 + (y - (top + bottom) / 2) * scale}`
			)
			.join(' ');
	});
</script>

<svelte:head><title>{segment?.name || m.segment_page_title()}</title></svelte:head>

<div class="space-y-3 md:space-y-4">
	<a href={memberURL('/segments', data.member)} class="back-link">
		<Icon name="back" size={18} />{m.nav_segments()}
	</a>

	{#if loading}
		<p role="status" class="surface state-panel">{m.ride_loading()}</p>
	{:else if failure || !segment}
		<div role="status" class="surface state-panel">
			{failure === 'request' ? m.segment_detail_failed() : m.segment_not_found()}
			{#if failure === 'request'}
				<button type="button" class="action ml-2" onclick={() => (retry += 1)}
					>{m.community_retry()}</button
				>
			{/if}
		</div>
	{:else}
		<header class="segment-hero">
			{#if trace}
				<svg viewBox="0 0 280 128" aria-hidden="true" class="segment-trace">
					<polyline points={trace} class="trace-casing" />
					<polyline points={trace} class="trace-line" />
				</svg>
			{/if}
			<div class="relative grid gap-2">
				<p class="flex items-center gap-1.5 text-xs font-semibold">
					<Icon name="segments" size={16} />{m.segment_page_title()}
				</p>
				<h1 class="page-title">{segment.name || m.segment_page_title()}</h1>
				<ul class="flex flex-wrap gap-x-2.5 gap-y-1 text-xs font-semibold tabular-nums">
					<li>{formatMetricDistance(segment.distance_m)}</li>
					{#if segment.avg_grade != null}<li>
							{m.segment_gradient_average({ grade: number.format(segment.avg_grade) })}
						</li>{/if}
					{#if segment.max_grade != null}<li>
							{m.segment_gradient_maximum({ grade: number.format(segment.max_grade) })}
						</li>{/if}
					{#if segment.elevation_gain_m != null}<li>
							{formatElevation(segment.elevation_gain_m)}
						</li>{/if}
					{#if segment.climb_category}<li>
							{m.segment_climb_category({ category: segment.climb_category })}
						</li>{/if}
					{#if segment.source === 'strava_catalog'}<li>{m.segment_strava_source()}</li>{/if}
				</ul>
			</div>
		</header>

		<SectionTabs
			{tabs}
			active={tab}
			id="segment"
			label={m.segment_page_title()}
			onchange={(next) => (tab = next)}
		/>

		{#key segment.id}
			<SegmentRivalries id={segment.id} distanceM={segment.distance_m} member={data.member}>
				{#snippet children(leaderboard, history)}
					<div
						role="tabpanel"
						id="segment-panel-overview"
						aria-labelledby="segment-tab-overview"
						hidden={tab !== 'overview'}
						class="space-y-3 md:space-y-4"
					>
						<MetricStrip
							label={bestLabel}
							metrics={[
								{
									label: bestLabel,
									value: formatDuration(best?.elapsed_s),
									icon: 'records',
									href: best ? memberURL(`/activities/${best.activity_id}`, data.member) : undefined
								},
								{
									label: m.segment_avg_speed(),
									value: formatSpeedKmh(segmentSpeedKmh(segment?.distance_m, best?.elapsed_s)),
									icon: 'speed'
								},
								{
									label: m.segment_attempts(),
									value:
										number.format(efforts.length) +
										(efforts.length >= detailAttemptLimit ? '+' : ''),
									icon: 'activities'
								}
							]}
						/>
						<div class="overview-grid">
							<div class="space-y-3 md:space-y-4">
								{#if tab === 'overview'}
									{@render leaderboard(true, () => {
										tab = 'leaderboard';
										document.getElementById('segment-tab-leaderboard')?.focus();
									})}
									{@render history()}
								{/if}
							</div>
							{#if coordinates.length > 1}
								<section class="surface min-w-0 overflow-hidden">
									<h2 class="section-title px-3 py-2 sm:px-4">{m.segment_route()}</h2>
									<div class="h-52 md:h-72"><ActivityMap {coordinates} /></div>
								</section>
							{/if}
						</div>
					</div>

					<div
						role="tabpanel"
						id="segment-panel-leaderboard"
						aria-labelledby="segment-tab-leaderboard"
						hidden={tab !== 'leaderboard'}
					>
						{#if tab === 'leaderboard'}{@render leaderboard(false, () => {})}{/if}
					</div>
				{/snippet}
			</SegmentRivalries>
		{/key}

		<div
			role="tabpanel"
			id="segment-panel-attempts"
			aria-labelledby="segment-tab-attempts"
			hidden={tab !== 'attempts'}
			class="space-y-3 md:space-y-4"
		>
			{#if tab === 'attempts' && trend.offsets.length}
				<section class="surface p-3 sm:p-4">
					<h2 class="section-title mb-2">{m.segment_performance_trend()}</h2>
					<StreamChart
						offsets={trend.offsets}
						series={trendSeries}
						height={200}
						calendar
						legend={false}
						yAxis={{
							label: m.segment_speed_axis(),
							format: (value) => number.format(value),
							range: segmentSpeedRange
						}}
						onHover={(value) => (hovered = value)}
					/>
					{#if hovered !== null && inspected != null}
						<p class="mt-1 text-xs text-quiet">
							{formatDate(new Date(hovered * 1000).toISOString())} · {formatDistanceEffort(
								segment.distance_m,
								inspected
							)}
						</p>
					{/if}
				</section>
			{/if}
			<section class="surface min-w-0 overflow-hidden">
				<h2 class="section-title px-3 py-2.5 sm:px-4">
					{data.member ? m.member_segment_efforts() : m.segment_leaderboard_only_yours()}
				</h2>
				{#if !efforts.length}
					<p class="state-panel">
						{data.member ? m.member_segment_no_efforts() : m.segment_no_personal_efforts()}
					</p>
				{:else}
					<table class="attempts-table w-full text-sm">
						<thead class="text-xs text-quiet">
							<tr>
								<th scope="col" class="text-left">{m.segment_leaderboard_date()}</th>
								<th scope="col">{m.detail_label_time()}</th>
								<th scope="col">{m.segment_leaderboard_rank()}</th>
								<th scope="col">{m.segment_heart_rate()}</th>
								<th scope="col">{m.segment_power()}</th>
							</tr>
						</thead>
						<tbody class="divide-y divide-zinc-800">
							{#each efforts.slice(0, visibleAttempts) as effort (effort.id)}
								<tr>
									<th scope="row" class="text-left font-semibold">
										<a
											href={memberURL(`/activities/${effort.activity_id}`, data.member)}
											class="text-accent-300 hover:underline"
										>
											{formatDate(effort.start_time)}
										</a>
									</th>
									<td>
										{formatDuration(effort.elapsed_s)}
										<span class="block text-xs text-quiet"
											>{formatSpeedKmh(segmentSpeedKmh(segment.distance_m, effort.elapsed_s))}</span
										>
									</td>
									<td>
										{effort.personal_rank > 0 ? effort.personal_rank : '—'}
										{#if effort.is_personal_record}<span
												title={m.segment_personal_record()}
												class="attempt-pr">PR</span
											>{/if}
									</td>
									<td>{effort.avg_heart_rate ?? '—'}</td>
									<td>{effort.avg_power ?? '—'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
					{#if visibleAttempts < efforts.length}
						<button
							type="button"
							class="action m-3"
							onclick={() => (visibleAttempts += attemptPageSize)}
							>{m.segment_more_attempts()}</button
						>
					{/if}
				{/if}
			</section>
		</div>
	{/if}
</div>

<style>
	.back-link {
		display: inline-flex;
		min-height: var(--target-size);
		align-items: center;
		gap: 0.3rem;
		border-radius: var(--radius-control);
		color: var(--accent-text);
		font-size: 0.8125rem;
		font-weight: 650;
	}

	.back-link:hover {
		text-decoration: underline;
	}

	.segment-hero {
		position: relative;
		isolation: isolate;
		display: flex;
		min-height: 8.25rem;
		align-items: flex-end;
		overflow: hidden;
		border-radius: var(--radius-panel);
		padding: clamp(0.9rem, 3vw, 1.5rem);
		background: #162e59;
		color: #e1edff;
	}

	.segment-hero h1 {
		color: #fff;
	}

	.segment-trace {
		position: absolute;
		z-index: -1;
		inset: 0 0 0 auto;
		width: min(62%, 26rem);
		height: 100%;
		opacity: 0.55;
	}

	.segment-trace polyline {
		fill: none;
		stroke-linecap: round;
		stroke-linejoin: round;
	}

	.trace-casing {
		stroke: #2d5393;
		stroke-width: 9;
	}

	.trace-line {
		stroke: #82b0ff;
		stroke-width: 3;
	}

	.segment-hero li + li::before {
		margin-inline-end: 0.5rem;
		content: '·';
	}

	.overview-grid {
		display: grid;
		gap: 0.85rem;
	}

	.attempts-table {
		table-layout: fixed;
	}

	.attempts-table :is(th, td) {
		padding: 0.55rem 0.3rem;
		text-align: right;
		overflow-wrap: anywhere;
	}

	.attempts-table th:first-child {
		width: 29%;
		padding-inline-start: 0.7rem;
		text-align: left;
	}

	.attempts-table :is(th, td):last-child {
		padding-inline-end: 0.7rem;
	}

	.attempt-pr {
		display: block;
		color: var(--warning-strong);
		font-size: 0.625rem;
		font-weight: 750;
	}

	@media (width >= 69rem) {
		.segment-hero {
			min-height: 9.5rem;
		}

		.overview-grid {
			grid-template-columns: minmax(0, 1.45fr) minmax(0, 1fr);
			align-items: start;
		}
	}
</style>
