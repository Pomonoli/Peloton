<script lang="ts">
	import BrandMark from '#lib/components/BrandMark.svelte';
	import { APP_NAME } from '$app/env/public';
	let email = $state('');
	let busy = $state(false);
	let done = $state(false);
	let message = $state<string | null>(null);

	async function submit(e: Event) {
		e.preventDefault();
		busy = true;
		try {
			const res = await fetch('/auth/password/forgot', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ email: email.trim() })
			});
			const body = await res.json().catch(() => ({}));
			message = body.message ?? 'If that email belongs to an account, a reset link is on its way.';
			done = true;
		} catch {
			// Even on error, show the generic message (no enumeration).
			message = 'If that email belongs to an account, a reset link is on its way.';
			done = true;
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
		<h1 class="mt-2 page-title">Reset your password</h1>
		<p class="mt-1 text-sm text-zinc-400">We'll email you a link to choose a new one.</p>
	</header>

	{#if done}
		<div
			class="rounded-lg border border-emerald-700/50 bg-emerald-950/30 px-4 py-3 text-sm text-emerald-300"
		>
			{message}
		</div>
		<a href="/login" class="text-center text-xs text-zinc-500 hover:text-accent-300"
			>Back to sign in</a
		>
	{:else}
		<form onsubmit={submit} class="surface space-y-3 p-4 sm:p-5">
			<label class="block">
				<span class="text-xs text-zinc-400">Email</span>
				<input
					bind:value={email}
					type="email"
					autocomplete="email"
					required
					class="mt-1 field w-full"
				/>
			</label>
			<button type="submit" disabled={busy || !email.trim()} class="action action-primary w-full">
				{busy ? 'Sending…' : 'Send reset link'}
			</button>
			<div class="text-center">
				<a href="/login" class="text-xs text-zinc-500 hover:text-accent-300">Back to sign in</a>
			</div>
		</form>
	{/if}
</div>
