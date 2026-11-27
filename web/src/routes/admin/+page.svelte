<script lang="ts">
	import { m } from '#lib/paraglide/messages.js';
	import { formatDate } from '#lib/format.js';
	import WorkerOnboarding from '#lib/components/WorkerOnboarding.svelte';
	import WorkerPools from '#lib/components/WorkerPools.svelte';
	import ProviderRegistry from '#lib/components/ProviderRegistry.svelte';
	import InviteManager from '#lib/components/InviteManager.svelte';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();

	// Promote/demote a user (delegated administration: user → moderator → admin).
	async function setRole(id: string, role: string) {
		const res = await fetch(`/api/admin/users/${id}/role`, {
			method: 'PUT',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ role })
		});
		if (!res.ok) {
			alert('Failed to update role: ' + (await res.text()).trim());
		}
	}
</script>

<section class="space-y-6">
	<header class="flex items-baseline justify-between max-md:flex-col max-md:gap-3">
		<div>
			<h1 class="page-title">{m.admin_title()}</h1>
			<p class="mt-1 text-sm text-zinc-400">{m.admin_intro()}</p>
		</div>
		<div class="flex flex-wrap gap-2">
			<a href="/admin/moderation" class="action"> Moderation → </a>
			<a href="/admin/oidc" class="action"> Identity Providers → </a>
		</div>
	</header>

	<section class="surface">
		<header class="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
			<h2 class="section-title">{m.stat_users()}</h2>
			<span class="text-xs text-zinc-500">{data.users.length}</span>
		</header>
		<ul class="divide-y divide-zinc-800">
			{#each data.users as u (u.id)}
				<li
					class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-4 px-4 py-3 text-sm wrap-anywhere max-md:grid-cols-1 max-md:gap-2"
				>
					<div>
						<div class="flex flex-wrap items-center gap-x-2 gap-y-1 font-medium">
							<span class="min-w-0">{u.displayName || u.username}</span>
							{#if u.role === 'admin'}
								<span
									class="rounded bg-accent-500/20 px-1.5 py-0.5 text-[10px] font-bold uppercase text-accent-300"
								>
									admin
								</span>
							{/if}
						</div>
						<div class="text-xs text-zinc-500">{u.email}</div>
					</div>
					<div class="text-xs text-zinc-500">
						<div class="font-mono max-md:break-all">{u.id}</div>
						<div class="mt-0.5">{formatDate(u.createdAt)}</div>
					</div>
					<div class="flex flex-wrap items-center gap-2 text-xs text-zinc-500">
						<span class="metric-value">{m.gear_activity_count({ count: u.activityCount })}</span>
						<select
							value={u.role}
							onchange={(e) => setRole(u.id, (e.currentTarget as HTMLSelectElement).value)}
							class="field"
							aria-label="user role"
						>
							<option value="user">user</option>
							<option value="moderator">moderator</option>
							<option value="admin">admin</option>
						</select>
					</div>
				</li>
			{:else}
				<li class="px-5 py-6 text-center text-sm text-zinc-500">{m.admin_no_users()}</li>
			{/each}
		</ul>
	</section>

	<InviteManager />

	<WorkerOnboarding />
	<WorkerPools />
	<ProviderRegistry />
</section>
