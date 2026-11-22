<script lang="ts">
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

<div class="flex items-center justify-between gap-4 max-md:flex-wrap">
	{#if onSubpage}
		<a href={base} class="text-xs text-accent-400 hover:text-accent-300">
			← {title || 'Activity'}
		</a>
	{:else}
		<a href={memberURL('/activities', member)} class="text-xs text-accent-400 hover:text-accent-300"
			>{m.detail_back()}</a
		>
	{/if}
	<nav class="flex items-center gap-2 max-md:flex-wrap">
		{#each links.filter((link) => isOwner || !link.ownerOnly) as l (l.href)}
			{@const active = page.url.pathname === l.href}
			<a
				href={l.href}
				aria-current={active ? 'page' : undefined}
				class="rounded border px-2.5 py-1 text-xs {active
					? 'border-accent-500 text-accent-300'
					: 'border-zinc-700 text-zinc-300 hover:border-accent-500 hover:text-accent-300'}"
			>
				{l.label}
			</a>
		{/each}
	</nav>
</div>
