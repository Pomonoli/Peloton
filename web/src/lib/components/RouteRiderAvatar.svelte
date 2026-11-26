<script lang="ts">
	import { riderName, type RouteRider } from '#lib/recorded-routes.js';

	let { rider, size = 'size-8' }: { rider: RouteRider; size?: string } = $props();
	let broken = $state('');
	const name = $derived(riderName(rider));
	const initials = $derived(
		name
			.split(/\s+/)
			.slice(0, 2)
			.map((part) => part[0]?.toUpperCase())
			.join('')
	);
</script>

<span
	class="inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-accent-500/15 text-xs font-semibold text-accent-300 {size}"
	aria-hidden="true"
>
	{#if rider.avatar_url && rider.avatar_url !== broken}
		<img
			src={rider.avatar_url}
			alt=""
			loading="lazy"
			class="size-full object-cover"
			onerror={() => (broken = rider.avatar_url)}
		/>
	{:else}
		{initials}
	{/if}
</span>
