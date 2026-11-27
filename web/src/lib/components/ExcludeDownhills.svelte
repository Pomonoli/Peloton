<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages.js';

	let { checked }: { checked: boolean } = $props();

	function setDownhill(exclude: boolean) {
		const url = new URL(page.url.href);
		if (exclude) url.searchParams.set('exclude_downhills', 'true');
		else url.searchParams.delete('exclude_downhills');
		void goto(url, { reset: false });
	}
</script>

<label class="inline-flex min-h-11 items-center gap-2 text-sm font-medium">
	<input type="checkbox" {checked} onchange={(event) => setDownhill(event.currentTarget.checked)} />
	{m.records_exclude_downhills()}
</label>
{#if checked}<p class="text-xs text-quiet">{m.records_downhill_help()}</p>{/if}
