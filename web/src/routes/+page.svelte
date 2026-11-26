<script lang="ts">
	import { onMount } from 'svelte';
	import { APP_NAME } from '$app/env/public';
	import CyclingWeather from '#lib/components/CyclingWeather.svelte';
	import WeeklyDistance from '#lib/components/WeeklyDistance.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { memberURL } from '#lib/member.js';
	import Icon from '#lib/components/Icon.svelte';
	import MetricStrip from '#lib/components/MetricStrip.svelte';
	import { activityMetrics, type ActivityTotals } from '#lib/dashboard-metrics.js';
	import type { IconName } from '#lib/icons.js';
	import SportIcon from '#lib/components/SportIcon.svelte';
	import {
		formatDistance,
		formatDuration,
		formatElevation,
		formatRelativeDate,
		formatSpeed
	} from '#lib/format.js';

	let { data } = $props();

	type Overview = {
		cycling_weeks: { start: string; distance_m: number }[];
		recent: Recent[];
		totals: ActivityTotals;
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
	const name = $derived(
		data.member?.display_name ||
			data.member?.username ||
			data.user?.displayName ||
			data.user?.username
	);
	let hour = $state<number>();
	onMount(() => {
		const update = () => (hour = new Date().getHours());
		update();
		const timer = setInterval(update, 60_000);
		return () => clearInterval(timer);
	});
	const greeting = $derived(
		hour === undefined
			? ''
			: hour < 12
				? m.overview_morning()
				: hour < 18
					? m.overview_afternoon()
					: m.overview_evening()
	);
	const shortcuts = $derived([
		...(!data.member
			? [{ href: '/routes/waasland', label: m.nav_route_planner(), icon: 'planner' as const }]
			: []),
		{ href: '/stats', label: m.nav_stats(), icon: 'stats' },
		{ href: '/segments', label: m.nav_segments(), icon: 'segments' },
		{ href: '/analysis', label: m.nav_analysis(), icon: 'analysis' }
	] satisfies { href: string; label: string; icon: IconName }[]);

	let failed = $state(false);
	let ov = $state<Overview | null>(null);
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
</script>

<svelte:head><title>{APP_NAME}</title></svelte:head>

{#if !data.user}
	<section class="surface mx-auto max-w-xl space-y-4 p-6 sm:p-10">
		<h1 class="page-title">{APP_NAME}</h1>
		<p class="text-sm text-quiet">{m.overview_public_summary()}</p>
		<a href="/login" class="action action-primary">{m.menu_signin()}</a>
	</section>
{:else}
	<div class="home">
		<header class="home-header">
			<div class="min-w-0">
				<h1 class="page-title home-title">
					{#if !data.member && greeting}<span>{greeting}</span>{/if}
					{name}
				</h1>
			</div>
			<CyclingWeather />
		</header>

		{#if ov?.totals}
			<MetricStrip metrics={activityMetrics(ov.totals)} label={m.overview_all_time()} />
		{/if}

		<div class="home-grid">
			<div class="grid min-w-0 content-start gap-3 md:gap-4">
				{#if ov}
					<WeeklyDistance weeks={ov.cycling_weeks} />
				{/if}
				<nav aria-label={m.overview_shortcuts()} class="home-shortcuts">
					{#each shortcuts as link (link.href)}
						<a
							href={memberURL(link.href, data.member)}
							class="action flex-col text-center {link.href === '/routes/waasland'
								? 'action-primary'
								: ''}"
						>
							<Icon name={link.icon} size={17} />
							{link.label}
						</a>
					{/each}
				</nav>
			</div>

			{#if ov}
				<section class="min-w-0" aria-labelledby="recent-activities">
					<div class="mb-1 flex items-center justify-between gap-2">
						<h2 id="recent-activities" class="section-title">{m.member_recent_activities()}</h2>
						<a
							href={memberURL('/activities', data.member)}
							class="inline-flex min-h-10 items-center gap-0.5 text-xs font-semibold text-accent-300 hover:underline"
							>{m.overview_view_all()} <Icon name="chevron" size={14} /></a
						>
					</div>
					<ul class="surface divide-y divide-zinc-800">
						{#each ov.recent as activity (activity.id)}
							<li>
								<a
									href={memberURL(`/activities/${activity.id}`, data.member)}
									class="recent-activity group"
								>
									<span class="recent-icon"><SportIcon type={activity.type} size={19} /></span>
									<div class="min-w-0">
										<p class="truncate text-sm font-semibold group-hover:text-accent-300">
											{activity.title || m.ride_activity()}
										</p>
										<dl
											class="mt-0.5 flex flex-wrap gap-x-2 gap-y-0.5 text-xs text-quiet tabular-nums"
										>
											{#each [{ label: m.detail_stat_distance(), value: formatDistance(activity.distance_m) }, { label: m.col_moving(), value: formatDuration(activity.moving_duration_s) }, { label: m.col_elevation(), value: formatElevation(activity.elevation_gain_m) }] as stat (stat.label)}
												<div>
													<dt class="sr-only">{stat.label}</dt>
													<dd>{stat.value}</dd>
												</div>
											{/each}
										</dl>
									</div>
									<div class="text-right">
										{#if activity.avg_speed_mps != null}
											<dl>
												<dt class="sr-only">{m.col_average_speed()}</dt>
												<dd class="text-xs font-bold text-accent-300 tabular-nums">
													{formatSpeed(activity.avg_speed_mps)}
												</dd>
											</dl>
										{/if}
										<p class="mt-0.5 text-[0.6875rem] text-quiet">
											{formatRelativeDate(activity.start_time)}
										</p>
									</div>
									<Icon name="chevron" size={14} class="text-accent-300 max-[22rem]:hidden" />
								</a>
							</li>
						{:else}
							<li class="state-panel">{m.activities_empty()}</li>
						{/each}
					</ul>
				</section>
			{:else if failed}
				<p role="alert" class="surface state-panel">{m.overview_failed()}</p>
			{:else}
				<p role="status" class="surface state-panel">{m.ride_loading()}</p>
			{/if}
		</div>
	</div>
{/if}

<style>
	.home,
	.home-grid {
		display: grid;
		gap: 0.875rem;
	}

	.home-header {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		align-items: center;
		gap: 0.5rem;
	}

	.home-title span {
		display: block;
	}

	.home-shortcuts {
		display: grid;
		grid-auto-columns: minmax(0, 1fr);
		grid-auto-flow: column;
		gap: 0.4rem;
	}

	.home-shortcuts a {
		min-height: 3.3rem;
		padding-inline: 0.2rem;
		font-size: clamp(0.65rem, 2.7vw, 0.8125rem);
	}

	.home-shortcuts a:not(.action-primary) :global(svg) {
		color: var(--accent-text);
	}

	.recent-activity {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr) auto auto;
		min-height: 3.625rem;
		align-items: center;
		gap: 0.45rem;
		padding: 0.55rem 0.65rem;
		border-radius: var(--radius-panel);
	}

	.recent-activity:hover {
		background: var(--surface-hover);
	}

	.recent-icon {
		display: grid;
		width: 2rem;
		height: 2rem;
		place-items: center;
		border-radius: 50%;
		background: var(--surface-active);
		color: var(--accent-text);
	}

	@media (width >= 70rem) {
		.home,
		.home-grid {
			gap: 1.25rem;
		}

		.home-grid {
			grid-template-columns: minmax(0, 1.25fr) minmax(0, 1fr);
			align-items: start;
		}

		.home-title span {
			display: inline;
		}

		.home-header {
			min-height: 5rem;
		}

		.home-shortcuts a {
			min-height: 3.6rem;
		}
	}
</style>
