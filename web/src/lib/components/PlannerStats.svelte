<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { routeDistance, routeElevation } from '#lib/recorded-routes.js';
	import type { ElevationStats } from '#lib/waas/types.js';
	import Icon from './Icon.svelte';

	let {
		distance,
		elevation
	}: {
		distance: number;
		elevation: ElevationStats | null;
	} = $props();
	const metrics = $derived([
		{ label: m.planner_distance(), icon: 'routes' as const, value: routeDistance(distance) },
		{
			label: m.planner_ascent(),
			icon: 'ascend' as const,
			value: routeElevation(elevation?.ascent ?? null)
		},
		{
			label: m.planner_descent(),
			icon: 'descend' as const,
			value: routeElevation(elevation?.descent ?? null)
		},
		{
			label: m.planner_highest(),
			icon: 'segments' as const,
			value: routeElevation(elevation?.highest ?? null)
		}
	]);
</script>

<section class="surface p-3 sm:p-4" aria-label={m.planner_summary()}>
	<h2 class="section-title mb-3">{m.planner_summary()}</h2>
	<dl class="grid grid-cols-2 gap-x-2 gap-y-3 max-lg:sm:grid-cols-4">
		{#each metrics as metric (metric.label)}
			<div class="min-w-0 rounded-lg bg-zinc-950 px-2.5 py-2 sm:px-3">
				<dt class="flex items-center gap-1.5 text-xs font-medium text-zinc-400">
					<Icon name={metric.icon} size={15} class="text-accent-300" />
					{metric.label}
				</dt>
				<dd class="mt-1 text-lg leading-tight font-bold tracking-tight tabular-nums text-zinc-100">
					{metric.value}
				</dd>
			</div>
		{/each}
	</dl>
</section>
