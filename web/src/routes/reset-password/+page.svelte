<script lang="ts">
	import BrandMark from '#lib/components/BrandMark.svelte';
	import { APP_NAME } from '$app/env/public';
	import { page } from '$app/state';
	import { refreshAll, goto } from '$app/navigation';

	const code = $derived(page.url.searchParams.get('code') ?? '');

	let password = $state('');
	let confirm = $state('');
	let busy = $state(false);
	let error = $state<string | null>(null);

	async function submit(e: Event) {
		e.preventDefault();
		error = null;
		if (password.length < 8) {
			error = 'Password must be at least 8 characters.';
			return;
		}
		if (password !== confirm) {
			error = 'Passwords do not match.';
			return;
		}
		busy = true;
		try {
			const res = await fetch('/auth/password/reset', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ code, password })
			});
			if (!res.ok) {
				error = (await res.text()).trim() || 'Reset failed.';
				return;
			}
			const body = await res.json().catch(() => ({}));
			if (body.logged_in) {
				await refreshAll();
				await goto('/');
			} else {
				await goto('/login?reset=1');
			}
		} catch (err) {
			error = (err as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<div class="mx-auto flex min-h-[60vh] max-w-md flex-col justify-center space-y-6">
	<header class="text-center">
		<div
			class="flex items-center justify-center gap-2 text-xs uppercase tracking-widest text-accent-300"
		>
			<BrandMark class="h-5 w-5" />
			{APP_NAME}
		</div>
		<h1 class="mt-2 page-title">Choose a new password</h1>
	</header>

	{#if !code}
		<div class="rounded-lg border border-red-700/50 bg-red-950/30 px-4 py-3 text-sm text-red-300">
			This reset link is missing its code. Request a new one.
		</div>
		<a href="/forgot-password" class="text-center text-xs text-zinc-500 hover:text-accent-300"
			>Request a reset link</a
		>
	{:else}
		<form onsubmit={submit} class="surface space-y-3 p-4 sm:p-5">
			{#if error}
				<div class="rounded border border-red-700/50 bg-red-950/30 px-3 py-2 text-xs text-red-300">
					{error}
				</div>
			{/if}
			<label class="block">
				<span class="text-xs text-zinc-400">New password</span>
				<input
					bind:value={password}
					type="password"
					autocomplete="new-password"
					required
					class="mt-1 field w-full"
				/>
			</label>
			<label class="block">
				<span class="text-xs text-zinc-400">Confirm password</span>
				<input
					bind:value={confirm}
					type="password"
					autocomplete="new-password"
					required
					class="mt-1 field w-full"
				/>
			</label>
			<button type="submit" disabled={busy} class="action action-primary w-full">
				{busy ? 'Updating…' : 'Update password'}
			</button>
		</form>
	{/if}
</div>
