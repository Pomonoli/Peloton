<script lang="ts">
	import { intlLocale } from '#lib/format.js';
	import { riderName, type Rider } from '#lib/head-to-head.js';
	import { memberProfile } from '#lib/member.js';

	let { rider, label }: { rider: Rider; label: string } = $props();
	let broken = $state('');
	const name = $derived(riderName(rider));
</script>

<a href={memberProfile(rider)} class="flex min-w-0 items-center gap-2 hover:underline sm:gap-3">
	<span
		aria-hidden="true"
		class="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-full bg-zinc-800 font-medium sm:size-11"
	>
		{#if rider.avatar_url && broken !== rider.avatar_url}
			<img
				src={rider.avatar_url}
				alt=""
				referrerpolicy="no-referrer"
				onerror={() => (broken = rider.avatar_url)}
				class="size-full object-cover"
			/>
		{:else}
			{name.slice(0, 1).toLocaleUpperCase(intlLocale())}
		{/if}
	</span>
	<span class="min-w-0">
		<span class="block text-xs text-quiet">{label}</span>
		<span class="block break-words text-sm font-semibold">{name}</span>
	</span>
</a>
