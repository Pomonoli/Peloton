<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import type { SortDirection } from '#lib/stacked-sort.js';

	let {
		label,
		index,
		direction,
		align = 'text-left',
		onclick
	}: {
		label: string;
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
		class="min-h-10 w-full px-3 py-2 whitespace-nowrap uppercase hover:text-zinc-200 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent-400 {align}"
	>
		{label}
		{#if direction}
			<span aria-hidden="true" class="ml-1 text-zinc-300"
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
