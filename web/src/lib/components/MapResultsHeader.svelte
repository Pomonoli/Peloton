<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';

	let { title, onclear }: { title: string; onclear: () => void } = $props();
</script>

<header class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
	<h2 class="min-w-0 font-semibold text-zinc-100" aria-live="polite">{title}</h2>
	<button
		type="button"
		class="min-h-10 rounded border border-zinc-700 px-3 text-sm font-medium text-accent-300 hover:border-accent-500"
		onclick={onclear}>{m.map_clear_location()}</button
	>
</header>
