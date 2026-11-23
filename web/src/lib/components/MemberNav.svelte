<script lang="ts">
	import { page } from '$app/state';
	import { memberLinks, type MemberView } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';

	let { member = null }: { member?: MemberView | null } = $props();
	const links = $derived(memberLinks(member));
</script>

<nav
	class="flex gap-x-4 gap-y-1 max-md:overflow-x-auto max-md:px-0.5 md:flex-wrap"
	aria-label={m.member_cycling_data()}
>
	{#each links as link (link.href)}
		<a
			href={link.url}
			aria-current={page.url.pathname === link.href ? 'page' : undefined}
			class="inline-flex min-h-9 shrink-0 items-center text-sm hover:underline {page.url
				.pathname === link.href
				? 'font-semibold text-accent-400'
				: 'text-zinc-300'}">{link.label}</a
		>
	{/each}
</nav>
