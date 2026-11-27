<script lang="ts">
	import type { FeedActivity } from '#lib/activity-feed.js';
	import { humanize } from '#lib/activity-filter.svelte.js';
	import {
		formatDistance,
		formatDuration,
		formatElevation,
		formatRelativeDate,
		formatSpeed
	} from '#lib/format.js';
	import { memberURL, type MemberView } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import SportIcon from './SportIcon.svelte';

	let {
		items,
		loading = false,
		member,
		riderName,
		selected,
		onselect
	}: {
		items: FeedActivity[];
		loading?: boolean;
		member?: MemberView | null;
		riderName?: (activity: FeedActivity) => string | undefined;
		selected?: string;
		onselect?: (activity: FeedActivity) => void;
	} = $props();
</script>

<ul
	class="activity-list surface divide-y divide-zinc-800 transition-opacity duration-200"
	class:opacity-50={loading}
>
	{#each items as activity (activity.id)}
		<li class:selected={selected === activity.id}>
			<a href={memberURL(`/activities/${activity.id}`, member)} class="activity-row">
				<div
					class="grid size-9 shrink-0 place-items-center rounded-full bg-accent-500/10 text-accent-300 sm:size-11"
					title={activity.discipline || activity.type}
				>
					<SportIcon type={activity.discipline || activity.type} size={24} />
				</div>
				<div class="min-w-0 flex-1">
					<div class="flex items-center gap-2">
						<span class="truncate font-semibold">
							{activity.title || `${humanize(activity.discipline || activity.type)} Activity`}
						</span>
						{#if activity.gear_unset}
							<span
								class="shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-amber-300"
								title={m.gear_prompt_list_hint()}>{m.gear_prompt_badge()}</span
							>
						{/if}
					</div>
					{#if riderName?.(activity)}
						<p class="text-xs text-zinc-400">{riderName(activity)}</p>
					{/if}
					<div class="mt-0.5 text-xs text-zinc-500">
						{formatRelativeDate(activity.start_time)}{activity.start_place
							? ` · ${humanize(activity.discipline || activity.type)} from ${activity.start_place}`
							: ` · ${activity.timezone}`}
					</div>
				</div>
				<img
					src={`/api/activities/${activity.id}/map.png`}
					alt=""
					loading="lazy"
					class="activity-map h-9 w-24 shrink-0 rounded-md border border-zinc-800 object-cover"
					onerror={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
				/>
				<dl
					class="activity-metrics grid grid-cols-3 gap-x-2 gap-y-1.5 text-xs text-quiet tabular-nums"
				>
					<div>
						<dt class="text-zinc-500">{m.col_distance()}</dt>
						<dd class="mt-0.5 text-sm tabular-nums text-zinc-100 max-md:text-xs">
							{formatDistance(activity.distance_m)}
						</dd>
					</div>
					<div>
						<dt class="text-zinc-500">{m.col_moving()}</dt>
						<dd class="mt-0.5 text-sm tabular-nums text-zinc-100 max-md:text-xs">
							{formatDuration(activity.moving_duration_s)}
						</dd>
					</div>
					<div>
						<dt class="text-zinc-500">{m.ride_elapsed()}</dt>
						<dd class="mt-0.5 text-sm tabular-nums text-zinc-100 max-md:text-xs">
							{formatDuration(activity.elapsed_duration_s)}
						</dd>
					</div>
					<div>
						<dt class="text-zinc-500">{m.col_elevation()}</dt>
						<dd class="mt-0.5 text-sm tabular-nums text-zinc-100 max-md:text-xs">
							{formatElevation(activity.elevation_gain_m)}
						</dd>
					</div>
					<div>
						<dt class="text-zinc-500">{m.col_average_speed()}</dt>
						<dd class="mt-0.5 font-bold text-accent-300 tabular-nums">
							{formatSpeed(activity.avg_speed_mps)}
						</dd>
					</div>
				</dl>
			</a>
			{#if onselect}
				<div class="flex justify-end px-3 pb-1">
					<button
						type="button"
						class="min-h-10 rounded px-2 text-sm font-medium text-accent-300 hover:underline"
						aria-pressed={selected === activity.id}
						aria-label={m.map_highlight_named({
							name: activity.title || humanize(activity.discipline || activity.type)
						})}
						onclick={() => onselect(activity)}>{m.map_highlight_track()}</button
					>
				</div>
			{/if}
		</li>
	{/each}
</ul>

<style>
	.activity-list {
		container-type: inline-size;
	}

	.activity-row {
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		align-items: center;
		gap: 0.55rem;
		border-radius: var(--radius-panel);
		padding: 0.7rem;
	}

	.selected {
		background: var(--surface-hover);
	}

	.activity-row:hover {
		background: var(--surface-hover);
	}

	.activity-metrics {
		grid-column: 2;
	}

	.activity-map {
		display: none;
	}

	@container (width >= 43rem) {
		.activity-row {
			grid-template-columns: auto minmax(9.5rem, 1fr) minmax(0, 1.5fr);
			gap: 0.9rem;
			padding: 0.85rem 1rem;
		}

		.activity-metrics {
			grid-column: auto;
			grid-template-columns: repeat(5, minmax(0, 1fr));
			text-align: right;
		}
	}

	@container (width >= 62rem) {
		.activity-row {
			grid-template-columns: auto minmax(9.5rem, 1fr) auto minmax(0, 1.35fr);
		}

		.activity-map {
			display: block;
		}
	}
</style>
