<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	let {
		label,
		view,
		onchange
	}: { label: string; view: 'list' | 'map'; onchange: (view: 'list' | 'map') => void } = $props();

	const views = ['list', 'map'] as const;
</script>

<div class="surface inline-flex p-0.5" role="group" aria-label={label}>
	{#each views as option (option)}
		<button
			type="button"
			aria-pressed={view === option}
			onclick={() => onchange(option)}
			class="min-h-10 rounded px-2.5 text-xs font-semibold sm:px-4 sm:text-sm {view === option
				? 'bg-accent-500/15 text-accent-200'
				: 'text-zinc-400 hover:text-zinc-200'}"
		>
			{option === 'list' ? m.segment_view_list() : m.segment_view_map()}
		</button>
	{/each}
</div>
