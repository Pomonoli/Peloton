<script lang="ts">
	import Icon from './Icon.svelte';
	import { page } from '$app/state';
	import { memberURL, type MemberView } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';

	// Shared header of the activity pages: back link + sub-page buttons. On a
	// sub-page the back link returns to the activity, on the activity to the list.
	let {
		id,
		title = '',
		isOwner = true,
		member = null
	}: { id: string; title?: string; isOwner?: boolean; member?: MemberView | null } = $props();

	const base = $derived(`/activities/${id}`);
	const links = $derived([
		{ href: `${base}/similar`, label: m.member_similar_routes(), ownerOnly: false },
		{ href: `${base}/edit`, label: 'Edit', ownerOnly: true },
		{
			href: `${base}/manage`,
			label: isOwner ? 'Manage & insights' : m.member_activity_insights(),
			ownerOnly: false
		}
	]);
	const onSubpage = $derived(page.url.pathname !== base);
</script>

<div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
	{#if onSubpage}
		<a
			href={base}
			class="inline-flex min-h-11 items-center gap-1 rounded-md text-xs font-semibold text-accent-300 hover:underline"
		>
			<Icon name="back" size={17} />
			{title || m.ride_activity()}
		</a>
	{:else}
		<a
			href={memberURL('/activities', member)}
			class="inline-flex min-h-11 items-center gap-1 rounded-md text-xs font-semibold text-accent-300 hover:underline"
			><Icon name="back" size={17} />{m.nav_activities()}</a
		>
	{/if}
	<nav class="flex flex-wrap items-center gap-1.5" aria-label={m.activity_views()}>
		{#each links.filter((link) => isOwner || !link.ownerOnly) as l (l.href)}
			{@const active = page.url.pathname === l.href}
			<a
				href={l.href}
				aria-current={active ? 'page' : undefined}
				class="action px-2.5 text-xs aria-[current=page]:bg-accent-500/10 aria-[current=page]:text-accent-300"
			>
				{l.label}
			</a>
		{/each}
	</nav>
</div>
