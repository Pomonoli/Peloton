<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import ActivityMap from '#lib/components/ActivityMap.svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	const a = $derived(data.activity);
	const coordinates = $derived(data.stream?.coordinates ?? []);
	const track = $derived(data.mapTrack);
</script>

<section class="flex h-[calc(100dvh-4rem)] max-md:h-[calc(100dvh-7.5rem)] flex-col gap-3">
	<header class="flex items-center justify-between gap-4">
		<div class="min-w-0">
			<a href={`/activities/${a.id}`} class="text-xs text-accent-400 hover:text-accent-300">
				← {a.title || m.ride_activity()}
			</a>
			<h1 class="truncate text-xl font-semibold tracking-tight">{m.detail_section_route()}</h1>
		</div>
		{#if a.type === 'ride' && track.length > 1}
			<a class="text-sm text-accent-300 hover:underline" href={`/activities/${a.id}/replay`}
				>{m.ride_replay()}</a
			>
		{/if}
	</header>

	{#if coordinates.length > 0}
		<div class="min-h-0 flex-1">
			{#key a.id}
				<ActivityMap {coordinates} {track} heightClass="h-full" />
			{/key}
		</div>
	{:else}
		<div
			class="flex flex-1 items-center justify-center rounded-lg border border-dashed border-zinc-700 bg-zinc-900/30 text-sm text-zinc-400"
		>
			{m.detail_map_no_track()}
		</div>
	{/if}
</section>
