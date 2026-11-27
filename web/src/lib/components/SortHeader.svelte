<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import type { SortDirection } from '#lib/stacked-sort.js';

	let {
		label,
		shortLabel,
		index,
		direction,
		align = 'text-left',
		onclick
	}: {
		label: string;
		shortLabel?: string | undefined;
		index: number;
		direction: SortDirection | undefined;
		align?: string;
		onclick: () => void;
	} = $props();
</script>

<th
	scope="col"
	class={align}
	aria-sort={index === 0 ? (direction === 'asc' ? 'ascending' : 'descending') : undefined}
>
	<button
		type="button"
		{onclick}
		title={shortLabel ? label : undefined}
		class="sort-heading min-h-10 w-full px-3 py-2 uppercase hover:text-zinc-200 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent-400 {align}"
	>
		{#if shortLabel}
			<span class="sr-only">{label}</span>
			<span class="md:hidden" aria-hidden="true">{shortLabel}</span>
			<span class="hidden md:inline" aria-hidden="true">{label}</span>
		{:else}
			{label}
		{/if}
		{#if direction}
			<span aria-hidden="true" class="ml-0.5 inline-block text-zinc-300 sm:ml-1"
				>{direction === 'asc' ? '↑' : '↓'}{index + 1}</span
			>
			<span class="sr-only">
				{m.segment_sort_priority({
					position: index + 1,
					direction: direction === 'asc' ? m.segment_sort_ascending() : m.segment_sort_descending()
				})}
			</span>
		{/if}
	</button>
</th>

<style>
	@media (width < 64rem) {
		.sort-heading {
			padding-inline: 0;
			white-space: normal;
			text-transform: none;
		}
	}
</style>
