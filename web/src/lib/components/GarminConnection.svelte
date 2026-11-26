<script lang="ts">
	import { onMount } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { formatDate } from '#lib/format.js';

	type Status = {
		connected: boolean;
		needs_key: boolean;
		busy: boolean;
		history_complete: boolean;
		history_rides: number;
		summary_only: number;
		skipped: number;
		last_sync: string | null;
		next_run: string | null;
		error: string;
	};

	const endpoint = '/api/connections/garmin';
	const keyPage = 'https://connect.garmin.com/app/settings/personalApiToken';

	let status = $state<Status | null>(null);
	let pending = $state(false);
	let showKey = $state(false);
	let key = $state('');
	let statusError = $state(false);
	let actionError = $state('');
	let queued = $state(false);
	let generation = 0;

	function errorMessage(code: string): string {
		switch (code) {
			case 'invalid_key':
				return m.garmin_invalid_key();
			case 'key_expired':
				return m.garmin_key_expired();
			case 'different_account':
				return m.garmin_different_account();
			case 'rate_limit':
				return m.garmin_rate_limit();
			case 'quota':
				return m.garmin_quota();
			case 'garmin_unavailable':
				return m.garmin_unavailable();
			case 'disconnect_failed':
				return m.garmin_disconnect_failed();
			case 'sync_failed':
				return m.garmin_sync_failed();
			case 'queue_failed':
				return m.garmin_queue_failed();
			default:
				return m.garmin_request_failed();
		}
	}

	async function refresh(signal?: AbortSignal) {
		const request = ++generation;
		try {
			const res = await fetch(endpoint, { signal: signal ?? null });
			if (!res.ok) throw new Error();
			const update: Status = await res.json();
			if (request !== generation) return;
			status = update;
			statusError = false;
			if (
				update.busy ||
				update.error ||
				!update.next_run ||
				Date.parse(update.next_run) > Date.now()
			) {
				queued = false;
			}
		} catch {
			if (request === generation && !signal?.aborted) statusError = true;
		}
	}

	onMount(() => {
		const controller = new AbortController();
		void refresh(controller.signal);
		const timer = window.setInterval(() => {
			if (!pending && document.visibilityState === 'visible') void refresh(controller.signal);
		}, 5000);
		return () => {
			controller.abort();
			window.clearInterval(timer);
		};
	});

	async function change(action: 'connect' | 'sync' | 'disconnect') {
		pending = true;
		generation++;
		actionError = '';
		const method = action === 'connect' ? 'PUT' : action === 'sync' ? 'POST' : 'DELETE';
		try {
			const res = await fetch(action === 'sync' ? endpoint + '/sync' : endpoint, {
				method,
				headers: { 'Content-Type': 'application/json' },
				body: action === 'connect' ? JSON.stringify({ key }) : null
			});
			if (!res.ok) {
				actionError = errorMessage((await res.text()).trim());
				await refresh();
				return;
			}
			showKey = false;
			queued = action === 'sync';
			await refresh();
		} catch {
			actionError = m.garmin_request_failed();
		} finally {
			key = '';
			pending = false;
		}
	}
</script>

<section id="garmin-connect" class="space-y-3 surface p-5">
	<div class="flex flex-wrap items-center justify-between gap-2">
		<h2 class="text-base font-medium">{m.garmin_title()}</h2>
		{#if status?.connected}
			<p class="text-sm text-zinc-400" role="status">
				{#if status.needs_key}
					{m.garmin_reconnect_required()}
				{:else if status.busy}
					{m.garmin_syncing()}
				{:else if queued}
					{m.garmin_queued()}
				{:else}
					{m.garmin_connected()}
				{/if}
			</p>
		{/if}
	</div>

	{#if statusError}
		<p class="text-sm text-red-300" role="alert">{m.garmin_status_failed()}</p>
	{:else if !status}
		<p class="text-sm text-zinc-400" role="status">{m.garmin_loading()}</p>
	{/if}
	{#if actionError}
		<p class="text-sm text-red-300" role="alert">{actionError}</p>
	{/if}

	{#if status?.connected}
		<div class="space-y-1 text-sm text-zinc-400">
			<p>
				{status.history_complete
					? m.garmin_history_complete({ count: status.history_rides })
					: m.garmin_history_running({ count: status.history_rides })}
			</p>
			{#if status.summary_only}
				<p>{m.garmin_summary_only({ count: status.summary_only })}</p>
			{/if}
			{#if status.skipped}
				<p>{m.garmin_skipped({ count: status.skipped })}</p>
			{/if}
			<p>
				{status.last_sync
					? m.garmin_last_sync({ date: formatDate(status.last_sync) })
					: m.garmin_no_sync()}
			</p>
			{#if status.error}
				<p class="text-red-300">{errorMessage(status.error)}</p>
			{/if}
			{#if status.next_run && !status.busy && (status.history_complete || status.error)}
				<p>{m.garmin_next_sync({ date: formatDate(status.next_run) })}</p>
			{/if}
		</div>
		<div class="flex flex-wrap gap-2">
			<button
				type="button"
				class="rounded bg-accent-500 px-3 py-1.5 text-sm font-medium text-on-accent hover:bg-accent-600 disabled:opacity-50"
				disabled={pending || status.busy || status.needs_key || queued}
				onclick={() => change('sync')}
			>
				{m.garmin_sync_now()}
			</button>
			<button
				type="button"
				class="rounded border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:border-accent-500 disabled:opacity-50"
				disabled={pending}
				onclick={() => (showKey = !showKey)}
			>
				{status.needs_key ? m.garmin_reconnect() : m.garmin_replace_key()}
			</button>
			<button
				type="button"
				class="rounded border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:border-red-600 disabled:opacity-50"
				disabled={pending}
				onclick={() => change('disconnect')}
			>
				{m.garmin_disconnect()}
			</button>
		</div>
	{/if}

	{#if status && (!status.connected || showKey)}
		<form
			class="space-y-2"
			onsubmit={(event) => {
				event.preventDefault();
				void change('connect');
			}}
		>
			<p class="text-sm text-zinc-400">
				{m.garmin_intro()}
				<a href={keyPage} target="_blank" rel="noreferrer" class="text-accent-300 underline">
					{m.garmin_create_key()}
				</a>
			</p>
			<label for="garmin-personal-key" class="block text-sm text-zinc-300"
				>{m.garmin_key_label()}</label
			>
			<div class="flex flex-wrap gap-2">
				<input
					id="garmin-personal-key"
					type="password"
					bind:value={key}
					autocomplete="off"
					maxlength="2048"
					required
					disabled={pending}
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm focus:border-accent-400 focus:outline-none sm:max-w-sm"
				/>
				<button
					type="submit"
					class="rounded bg-accent-500 px-3 py-1.5 text-sm font-medium text-on-accent hover:bg-accent-600 disabled:opacity-50"
					disabled={pending || !key.trim()}
				>
					{pending
						? m.garmin_connecting()
						: status.connected
							? m.garmin_save_key()
							: m.garmin_connect()}
				</button>
			</div>
		</form>
	{/if}
</section>
