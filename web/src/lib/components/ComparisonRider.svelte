<script lang="ts">
	import { intlLocale } from '#lib/format.js';
	import { riderName, type Rider } from '#lib/head-to-head.js';
	import { memberProfile } from '#lib/member.js';
	import Icon from './Icon.svelte';

	let { rider }: { rider: Rider | undefined } = $props();
	let broken = $state('');
</script>

{#if rider}
	{@const name = riderName(rider)}
	<a href={memberProfile(rider)} aria-label={name} title={'@' + rider.username} class="avatar-link">
		<span class="avatar" aria-hidden="true">
			{#if rider.avatar_url && broken !== rider.avatar_url}
				<img
					src={rider.avatar_url}
					alt=""
					referrerpolicy="no-referrer"
					onerror={() => (broken = rider?.avatar_url ?? '')}
					class="size-full object-cover"
				/>
			{:else}
				{name.slice(0, 1).toLocaleUpperCase(intlLocale())}
			{/if}
		</span>
	</a>
{:else}
	<span class="avatar-link" aria-hidden="true"
		><span class="avatar"><Icon name="friends" size={15} /></span></span
	>
{/if}

<style>
	.avatar-link {
		display: grid;
		min-height: var(--target-size);
		width: clamp(1.35rem, 5.7vw, 2.75rem);
		flex: none;
		place-items: center;
		border-radius: 0.5rem;
	}

	a.avatar-link:hover {
		background: var(--rider-wash);
	}

	.avatar {
		display: grid;
		width: clamp(1.35rem, 5.7vw, 1.85rem);
		aspect-ratio: 1;
		place-items: center;
		overflow: hidden;
		border-radius: 50%;
		background: var(--rider-wash);
		color: var(--rider-color);
		font-size: 0.75rem;
		font-weight: 700;
	}
</style>
