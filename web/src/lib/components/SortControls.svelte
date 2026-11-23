<script lang="ts" generics="Key extends string">
	import { m } from '#lib/paraglide/messages.js';
	import { promoteSort, type SortCriterion } from '#lib/stacked-sort.js';

	let {
		columns,
		order,
		onchange
	}: {
		columns: { key: Key; label: string }[];
		order: SortCriterion<Key>[];
		onchange: (next: SortCriterion<Key>[]) => void;
	} = $props();
	const id = $props.id();
</script>

<div class="space-y-2 md:hidden">
	<label for={id} class="text-xs font-semibold text-quiet">{m.segment_sort_stack()}</label>
	<select
		{id}
		class="min-h-11 w-full rounded-lg border border-zinc-800 bg-panel px-2 text-base"
		value=""
		onchange={(event) => {
			onchange(promoteSort(order, event.currentTarget.value as Key));
			event.currentTarget.value = '';
		}}
	>
		<option value="" disabled>{m.segment_sort_add()}</option>
		{#each columns as column (column.key)}
			<option value={column.key}>{column.label}</option>
		{/each}
	</select>
	<ol class="flex flex-wrap gap-1.5">
		{#each order as item, i (item.column)}
			<li>
				<button
					type="button"
					onclick={() => onchange(promoteSort(order, item.column))}
					class="min-h-10 rounded-lg bg-zinc-800/65 px-2 text-xs font-semibold"
				>
					{i + 1}. {columns.find((column) => column.key === item.column)?.label}
					{item.direction === 'asc' ? '↑' : '↓'}
				</button>
			</li>
		{/each}
	</ol>
</div>
