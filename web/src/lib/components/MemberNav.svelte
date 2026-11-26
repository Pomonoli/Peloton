<script lang="ts">
	import { page } from '$app/state';
	import { memberLinks, type MemberView } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';

	let { member = null }: { member?: MemberView | null } = $props();
	const links = $derived(memberLinks(member));
</script>

<nav class="flex flex-wrap gap-x-1 gap-y-1" aria-label={m.member_cycling_data()}>
	{#each links as link (link.href)}
		<a
			href={link.url}
			aria-current={page.url.pathname === link.href ? 'page' : undefined}
			class="tab-item rounded-t-md">{link.label}</a
		>
	{/each}
</nav>
