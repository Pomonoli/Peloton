<script lang="ts">
	import { onDestroy } from 'svelte';
	import ActivityMap from '#lib/components/ActivityMap.svelte';
	import { formatDistance, formatDuration } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import { metricValue } from '#lib/ride-display.js';
	import { pointAtTime } from '#lib/ride-track.js';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	const track = $derived(data.replayTrack);
	const coordinates = $derived(track.map((p): [number, number] => [p.lon, p.lat]));
	const base = $derived(track[0]?.t ?? 0);
	const duration = $derived((track.at(-1)?.t ?? base) - base);
	let position = $state(0);
	let playing = $state(false);
	let rate = $state(10);
	let follow = $state(false);
	let terrain = $state(true);
	let terrainFailed = $state(false);
	const point = $derived(track[pointAtTime(track, base + position)]);
	let frame = 0;
	let previous = 0;

	function tick(now: number) {
		if (previous) position = Math.min(duration, position + ((now - previous) * rate) / 1000);
		previous = now;
		if (position >= duration) {
			pause();
			return;
		}
		frame = requestAnimationFrame(tick);
	}

	function play() {
		if (position >= duration) position = 0;
		previous = 0;
		playing = true;
		frame = requestAnimationFrame(tick);
	}

	function pause() {
		playing = false;
		if (frame) cancelAnimationFrame(frame);
		frame = 0;
	}

	function restart() {
		position = 0;
		previous = 0;
	}

	function failTerrain() {
		terrain = false;
		terrainFailed = true;
	}

	$effect(() => {
		void data.activity.id;
		pause();
		position = 0;
		follow = false;
		terrain = true;
		terrainFailed = false;
	});
	onDestroy(pause);
</script>

<svelte:head><title>{m.ride_replay()} · {data.activity.title}</title></svelte:head>

<section class="flex min-h-[calc(100dvh-8rem)] flex-col gap-3">
	<header>
		<a href={`/activities/${data.activity.id}`} class="text-sm text-accent-300 hover:underline">
			← {data.activity.title || m.ride_activity()}
		</a>
		<h1 class="page-title">{m.ride_replay()}</h1>
	</header>

	{#if point && duration > 0}
		{#key data.activity.id}
			<ActivityMap
				{coordinates}
				{track}
				heightClass="h-[min(55dvh,36rem)] min-h-64"
				replay={{
					point,
					terrain,
					follow,
					playing,
					onExplore: () => (follow = false),
					onTerrainError: failTerrain
				}}
			/>
		{/key}
		<div class="space-y-3 surface p-3">
			<label class="flex items-center gap-3">
				<span class="sr-only">{m.ride_replay_progress()}</span>
				<span class="shrink-0 text-sm tabular-nums" aria-hidden="true"
					>{formatDuration(position)}</span
				>
				<input
					type="range"
					min={0}
					max={duration}
					step="any"
					bind:value={position}
					aria-valuetext={formatDuration(position)}
					class="h-8 min-w-0 flex-1 accent-accent-500"
				/>
				<span class="shrink-0 text-sm tabular-nums" aria-hidden="true"
					>{formatDuration(duration)}</span
				>
			</label>
			<div class="flex flex-wrap items-center gap-2 text-sm">
				<button
					type="button"
					onclick={() => (playing ? pause() : play())}
					class="min-h-10 rounded-md bg-accent-500 px-4 font-medium text-on-accent"
				>
					{playing ? m.ride_pause() : m.ride_play()}
				</button>
				<button
					type="button"
					onclick={restart}
					class="min-h-10 rounded-md border border-zinc-700 px-3"
				>
					{m.ride_restart()}
				</button>
				<label class="flex items-center gap-2 px-1">
					{m.ride_playback_speed()}
					<select
						bind:value={rate}
						class="min-h-10 rounded-md border border-zinc-700 bg-panel px-2"
					>
						{#each [1, 2, 5, 10, 25, 50, 100] as speed (speed)}
							<option value={speed}>{speed}×</option>
						{/each}
					</select>
				</label>
				<label class="flex min-h-10 items-center gap-2 px-1">
					<input type="checkbox" bind:checked={follow} class="accent-accent-500" />
					{m.ride_follow()}
				</label>
				<label class="flex min-h-10 items-center gap-2 px-1">
					<input
						type="checkbox"
						bind:checked={terrain}
						disabled={terrainFailed}
						class="accent-accent-500"
					/>
					{m.ride_terrain()}
				</label>
			</div>
			<dl class="grid grid-cols-2 gap-3 text-sm tabular-nums sm:grid-cols-4">
				<div>
					<dt class="text-quiet">{m.ride_speed()}</dt>
					<dd>{point.speedKmh == null ? '—' : metricValue('speedKmh', point.speedKmh)}</dd>
				</div>
				<div>
					<dt class="text-quiet">{m.ride_distance()}</dt>
					<dd>{formatDistance(point.distM)}</dd>
				</div>
				<div>
					<dt class="text-quiet">{m.ride_elapsed()}</dt>
					<dd>{formatDuration(position)}</dd>
				</div>
				<div>
					<dt class="text-quiet">{m.ride_elevation()}</dt>
					<dd>{point.eleM == null ? '—' : metricValue('eleM', point.eleM)}</dd>
				</div>
			</dl>
		</div>
		<p class="text-xs text-quiet" role="status">
			{terrainFailed ? m.ride_terrain_fallback() : terrain ? m.ride_terrain_accuracy() : ''}
		</p>
	{:else}
		<p class="surface p-4 text-sm text-quiet">
			{m.ride_replay_no_gps()}
		</p>
	{/if}
</section>
