<script lang="ts">
	let { value }: { value: string } = $props();
	const parts = $derived(value.split(' · '));
</script>

{#each parts as part, index (index)}
	{#if index > 0}<wbr />{/if}<span class="metric-value">
		{index > 0 ? ` · ${part}` : part}
	</span>
{/each}
