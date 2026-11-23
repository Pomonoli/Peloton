<script lang="ts">
	import { APP_NAME } from '$app/env/public';
	import WeeklyDistance from '#lib/components/WeeklyDistance.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { memberURL } from '#lib/member.js';
	import SportIcon from '#lib/components/SportIcon.svelte';
	import {
		intlLocale,
		formatDistance,
		formatDuration,
		formatElevation,
		formatMovingSpeed,
		formatRelativeDate
	} from '#lib/format.js';

	let { data } = $props();

	type WeekStat = { count: number; distance_m: number; moving_s: number; elevation_gain_m: number };
	type Overview = {
		totals: WeekStat;
		cycling_weeks: { start: string; distance_m: number }[];
		recent: Recent[];
		this_week?: WeekStat;
		last_week?: WeekStat;
	};

	type Recent = {
		id: string;
		title: string;
		type: string;
		start_time: string;
		distance_m: number | null;
		moving_duration_s: number;
		elevation_gain_m: number | null;
	};
	let failed = $state(false);
	let ov = $state<Overview | null>(null);

	// Deep-link a week's stat block into the activities view filtered to that
	// week. UTC, Monday-start, half-open [from, to) — mirrors the server's
	// this_week/last_week buckets in overview.go.
	function weekHref(weeksAgo: number): string {
		const now = new Date();
		const today = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
		const dow = (today.getUTCDay() + 6) % 7; // Mon=0 … Sun=6
		// eslint-disable-next-line svelte/prefer-svelte-reactivity -- local scratch date
		const monday = new Date(today);
		monday.setUTCDate(today.getUTCDate() - dow - weeksAgo * 7);
		// eslint-disable-next-line svelte/prefer-svelte-reactivity -- local scratch date
		const next = new Date(monday);
		next.setUTCDate(monday.getUTCDate() + 7);
		const iso = (d: Date) => d.toISOString().slice(0, 10);
		return memberURL(`/activities?from=${iso(monday)}&to=${iso(next)}`, data.member);
	}

	$effect(() => {
		if (!data.user) return;
		const url = memberURL('/api/overview', data.member);
		const controller = new AbortController();
		ov = null;
		failed = false;
		void (async () => {
			try {
				const response = await fetch(url, { signal: controller.signal });
				if (!response.ok) throw new Error();
				const result: Overview = await response.json();
				if (!controller.signal.aborted) ov = result;
			} catch {
				if (!controller.signal.aborted) failed = true;
			}
		})();
		return () => controller.abort();
	});

	const summary = $derived(
		ov
			? [
					{ label: 'Activities', value: ov.totals.count.toLocaleString(intlLocale()) },
					{ label: 'Distance', value: formatDistance(ov.totals.distance_m) },
					{ label: 'Moving time', value: formatDuration(ov.totals.moving_s) },
					{ label: 'Elevation', value: formatElevation(ov.totals.elevation_gain_m) }
				]
			: []
	);
</script>

<svelte:head><title>{APP_NAME}</title></svelte:head>

{#if !data.user}
	<div class="space-y-6">
		<header>
			<h1 class="text-3xl font-semibold tracking-tight max-md:text-2xl">{m.overview_title()}</h1>
			<p class="mt-1 text-sm text-zinc-400">{m.overview_intro()}</p>
		</header>
		<a
			href="/login"
			class="inline-block rounded border border-accent-500 bg-accent-500/20 px-4 py-2 text-sm text-accent-300 hover:bg-accent-500/30"
		>
			{m.menu_signin()}
		</a>
	</div>
{:else}
	<div class="space-y-6">
		<!-- Top header: welcome + summary strip -->
		<header class="rounded-xl border border-zinc-800 bg-panel p-5">
			<div class="flex flex-wrap items-end justify-between gap-4">
				<div>
					<h1 class="text-2xl font-semibold tracking-tight">
						{#if data.member}{data.member.display_name ||
								data.member.username}{:else}{m.overview_welcome({
								name: data.user.displayName || data.user.username
							})}{/if}
					</h1>
					{#if !data.member}<p class="mt-1 text-sm text-zinc-400">
							Your training at a glance.
						</p>{/if}
				</div>
				{#if summary.length}
					<dl class="flex flex-wrap gap-x-8 gap-y-2">
						{#each summary as s (s.label)}
							<div>
								<dt class="text-xs uppercase tracking-wide text-zinc-500">{s.label}</dt>
								<dd class="text-lg font-semibold tabular-nums text-zinc-100">{s.value}</dd>
							</div>
						{/each}
					</dl>
				{/if}
			</div>
		</header>

		{#if ov}
			<WeeklyDistance weeks={ov.cycling_weeks} />
		{:else if failed}
			<p role="alert" class="text-sm text-red-300">{m.overview_failed()}</p>
		{:else}
			<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
		{/if}

		{#snippet weekBlock(label: string, w: WeekStat | undefined, href: string)}
			<section class="rounded-xl border border-zinc-800 bg-panel p-4">
				<a
					{href}
					class="group mb-3 flex items-center justify-between text-xs font-medium uppercase tracking-wide text-zinc-500 hover:text-zinc-300"
				>
					<h2>{label}</h2>
					<span class="text-quiet transition group-hover:translate-x-0.5 group-hover:text-zinc-400"
						>→</span
					>
				</a>
				{#if w && w.count > 0}
					<dl class="grid grid-cols-2 gap-3">
						<div>
							<dt class="text-xs text-zinc-500">Activities</dt>
							<dd class="text-lg font-semibold tabular-nums text-zinc-100">{w.count}</dd>
						</div>
						<div>
							<dt class="text-xs text-zinc-500">Distance</dt>
							<dd class="text-lg font-semibold tabular-nums text-zinc-100">
								{formatDistance(w.distance_m)}
							</dd>
						</div>
						<div>
							<dt class="text-xs text-zinc-500">Moving time</dt>
							<dd class="text-lg font-semibold tabular-nums text-zinc-100">
								{formatDuration(w.moving_s)}
							</dd>
						</div>
						<div>
							<dt class="text-xs text-zinc-500">Elevation</dt>
							<dd class="text-lg font-semibold tabular-nums text-zinc-100">
								{formatElevation(w.elevation_gain_m)}
							</dd>
						</div>
					</dl>
				{:else}
					<p class="text-sm text-zinc-500">No activities.</p>
				{/if}
			</section>
		{/snippet}

		<div class="grid gap-6 lg:grid-cols-[20rem_1fr]">
			<!-- Left sidebar: weekly stats + stuff -->
			<aside class="space-y-4">
				{#if ov}
					{@render weekBlock('This week', ov?.this_week, weekHref(0))}
					{@render weekBlock('Last week', ov?.last_week, weekHref(1))}
				{/if}

				<nav class="rounded-xl border border-zinc-800 bg-panel p-4 text-sm">
					<h2 class="mb-2 text-xs font-medium uppercase tracking-wide text-zinc-500">Explore</h2>
					<div class="space-y-0.5">
						<a
							href={memberURL('/analysis', data.member)}
							class="block rounded px-2 py-1.5 text-zinc-300 hover:bg-zinc-800/60">Training load</a
						>
						<a
							href={memberURL('/stats', data.member)}
							class="block rounded px-2 py-1.5 text-zinc-300 hover:bg-zinc-800/60">Stats</a
						>
						<a
							href={memberURL('/segments', data.member)}
							class="block rounded px-2 py-1.5 text-zinc-300 hover:bg-zinc-800/60">Segments</a
						>
					</div>
				</nav>
			</aside>

			<section aria-labelledby="recent-activities">
				<h2 id="recent-activities" class="mb-3 text-sm font-medium">
					{m.member_recent_activities()}
				</h2>
				<div class="space-y-3">
					{#each ov?.recent ?? [] as activity (activity.id)}
						<article class="rounded-lg border border-zinc-800 bg-panel p-3 sm:p-4">
							<a
								href={`/activities/${activity.id}`}
								class="flex items-center gap-3 hover:text-accent-300"
							>
								<SportIcon type={activity.type} size={24} />
								<span class="min-w-0 flex-1 truncate font-medium"
									>{activity.title || m.ride_activity()}</span
								>
								<span class="text-xs text-quiet">{formatRelativeDate(activity.start_time)}</span>
							</a>
							<dl
								class="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 border-t border-zinc-800 pt-2 text-sm sm:grid-cols-4"
							>
								{#each [{ label: m.detail_stat_distance(), value: formatDistance(activity.distance_m) }, { label: m.col_moving(), value: activity.moving_duration_s > 0 ? formatDuration(activity.moving_duration_s) : '—' }, { label: m.col_elevation(), value: formatElevation(activity.elevation_gain_m) }, { label: m.col_average_speed(), value: formatMovingSpeed(activity.distance_m, activity.moving_duration_s) }] as stat (stat.label)}
									<div>
										<dt class="text-xs text-quiet">{stat.label}</dt>
										<dd class="font-semibold tabular-nums">{stat.value}</dd>
									</div>
								{/each}
							</dl>
						</article>
					{:else}
						{#if ov}<p class="rounded-lg border border-zinc-800 bg-panel p-4 text-sm text-quiet">
								{m.activities_empty()}
							</p>{/if}
					{/each}
				</div>
			</section>
		</div>
	</div>
{/if}
