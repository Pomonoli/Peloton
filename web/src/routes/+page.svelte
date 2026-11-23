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
		formatRelativeDate,
		formatSpeed
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
		avg_speed_mps: number | null;
		elevation_gain_m: number | null;
	};
	let failed = $state(false);
	let ov = $state<Overview | null>(null);
	let selectedWeek = $state(0);
	const weeks = $derived([
		{ label: m.overview_this_week(), stats: ov?.this_week },
		{ label: m.overview_last_week(), stats: ov?.last_week }
	]);
	const week = $derived(weeks[selectedWeek]?.stats);

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

	function metrics(totals: WeekStat) {
		return [
			{ label: 'Activities', value: totals.count.toLocaleString(intlLocale()) },
			{ label: 'Distance', value: formatDistance(totals.distance_m) },
			{ label: m.col_moving(), value: formatDuration(totals.moving_s) },
			{ label: 'Elevation', value: formatElevation(totals.elevation_gain_m) }
		];
	}
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
	<div class="space-y-4 md:space-y-5">
		{#if data.member}
			<h1 class="text-2xl font-bold tracking-tight">
				{data.member.display_name || data.member.username}
			</h1>
		{:else}
			<header
				class="overview-hero flex min-h-28 items-center overflow-hidden rounded-2xl px-4 py-5 text-white sm:min-h-36 sm:px-7"
			>
				<h1
					class="max-w-[75%] text-[1.65rem] wrap-break-word leading-tight font-bold tracking-tight sm:text-3xl"
				>
					{m.overview_welcome({ name: data.user.displayName || data.user.username })}
				</h1>
			</header>
		{/if}

		{#if ov}
			<div class="grid items-stretch gap-3 lg:grid-cols-[minmax(17rem,0.85fr)_minmax(0,1.5fr)]">
				<div class="flex min-w-0 flex-col rounded-2xl border border-zinc-800 bg-panel p-3 sm:p-4">
					<div class="flex items-center justify-between gap-2">
						<div
							role="group"
							aria-label={m.overview_weeks()}
							class="inline-flex rounded-xl bg-zinc-800/55 p-0.5"
						>
							{#each weeks as item, i (item.label)}
								<button
									type="button"
									aria-pressed={selectedWeek === i}
									onclick={() => (selectedWeek = i)}
									class="min-h-10 rounded-[10px] px-3 text-xs font-semibold text-quiet aria-pressed:bg-panel aria-pressed:text-accent-300"
								>
									{item.label}
								</button>
							{/each}
						</div>
						<a
							href={weekHref(selectedWeek)}
							class="inline-flex min-h-11 shrink-0 items-center gap-1 rounded-lg text-xs font-semibold text-accent-300"
						>
							{week?.count === 1
								? m.overview_one_activity()
								: week
									? m.overview_activity_count({ count: week.count.toLocaleString(intlLocale()) })
									: m.nav_activities()}
							<span aria-hidden="true">›</span>
						</a>
					</div>
					{#if week && week.count > 0}
						<dl
							class="mt-2 grid grid-cols-3 gap-px overflow-hidden rounded-xl border border-zinc-800 bg-zinc-800"
						>
							{#each metrics(week).slice(1) as stat (stat.label)}
								<div class="bg-zinc-950 px-2.5 py-2.5">
									<dt class="text-xs text-quiet">{stat.label}</dt>
									<dd class="text-base leading-snug font-bold break-words tabular-nums sm:text-lg">
										{stat.value}
									</dd>
								</div>
							{/each}
						</dl>
					{:else}
						<p class="my-3 text-sm text-quiet">{m.activities_empty()}</p>
					{/if}
					<details class="mt-auto pt-1.5">
						<summary
							class="cursor-pointer rounded-lg py-2 text-xs font-semibold text-quiet hover:text-accent-300"
							>{m.overview_all_time()}</summary
						>
						<dl class="grid grid-cols-2 gap-2 border-t border-zinc-800 pt-2">
							{#each metrics(ov.totals) as stat (stat.label)}
								<div>
									<dt class="text-xs text-quiet">{stat.label}</dt>
									<dd class="text-sm font-semibold tabular-nums">{stat.value}</dd>
								</div>
							{/each}
						</dl>
					</details>
				</div>
				<WeeklyDistance weeks={ov.cycling_weeks} />
			</div>

			<section aria-labelledby="recent-activities">
				<div class="flex items-center justify-between gap-2">
					<h2 id="recent-activities" class="text-base font-bold tracking-tight">
						{m.member_recent_activities()}
					</h2>
					<a
						href={memberURL('/activities', data.member)}
						class="inline-flex min-h-11 items-center gap-1 rounded-lg text-xs font-semibold text-accent-300"
					>
						{m.overview_view_all()} <span aria-hidden="true">›</span>
					</a>
				</div>
				<ul
					class="divide-y divide-zinc-800 overflow-hidden rounded-2xl border border-zinc-800 bg-panel"
				>
					{#each ov.recent as activity (activity.id)}
						<li>
							<a
								href={`/activities/${activity.id}`}
								aria-label={activity.title || m.ride_activity()}
								class="group grid min-h-15 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-2.5 px-2.5 py-2.5 hover:bg-zinc-950 sm:px-4"
							>
								<span
									class="grid size-9 place-items-center rounded-xl bg-accent-500/10 text-accent-300"
									><SportIcon type={activity.type} size={20} /></span
								>
								<div class="min-w-0">
									<p class="truncate text-sm font-semibold group-hover:text-accent-300">
										{activity.title || m.ride_activity()}
									</p>
									<dl
										class="mt-0.5 flex flex-wrap gap-x-2.5 gap-y-0.5 text-xs text-quiet tabular-nums"
									>
										{#each [{ label: m.detail_stat_distance(), value: formatDistance(activity.distance_m) }, { label: m.col_moving(), value: formatDuration(activity.moving_duration_s) }, { label: m.col_elevation(), value: formatElevation(activity.elevation_gain_m) }, { label: m.col_average_speed(), value: formatSpeed(activity.avg_speed_mps) }] as stat (stat.label)}
											<div>
												<dt class="sr-only">{stat.label}</dt>
												<dd>{stat.value}</dd>
											</div>
										{/each}
									</dl>
								</div>
								<div class="flex items-center gap-1.5 text-right text-xs text-quiet">
									<span>{formatRelativeDate(activity.start_time)}</span><span
										class="text-accent-300"
										aria-hidden="true">›</span
									>
								</div>
							</a>
						</li>
					{:else}
						<li class="p-3 text-sm text-quiet">{m.activities_empty()}</li>
					{/each}
				</ul>
			</section>
		{:else if failed}
			<p role="alert" class="text-sm text-red-300">{m.overview_failed()}</p>
		{:else}
			<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
		{/if}

		<nav aria-label={m.overview_shortcuts()}>
			<div class="grid grid-cols-3 gap-2">
				{#each [{ href: '/analysis', label: m.nav_analysis(), icon: 'M3 3v18h18 M7 14l4-4 3 3 5-6' }, { href: '/stats', label: m.nav_stats(), icon: 'M6 20v-8 M12 20V6 M18 20v-5' }, { href: '/segments', label: m.nav_segments(), icon: 'm8 3 4 8 5-5 5 15H2L8 3' }] as link (link.href)}
					<a
						href={memberURL(link.href, data.member)}
						class="flex min-h-11 items-center justify-center gap-2 rounded-xl border border-zinc-800 bg-panel px-2 text-xs font-semibold text-zinc-300 hover:text-accent-300"
					>
						<svg
							aria-hidden="true"
							class="size-4 shrink-0 text-accent-300"
							viewBox="0 0 24 24"
							fill="none"
							stroke="currentColor"
							stroke-width="1.8"
							stroke-linecap="round"
							stroke-linejoin="round"><path d={link.icon} /></svg
						>
						{link.label}
					</a>
				{/each}
			</div>
		</nav>
	</div>
{/if}

<style>
	.overview-hero {
		background-color: #0b1f3b;
		background-image:
			linear-gradient(90deg, rgb(11 31 59 / 0.93), rgb(11 31 59 / 0.58) 55%, transparent),
			url('/cycling-hero.webp');
		background-position:
			center,
			center 50%;
		background-size: cover;
	}
</style>
