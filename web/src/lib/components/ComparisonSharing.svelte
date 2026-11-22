<script lang="ts">
	import { onMount } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';

	type Policy = Record<string, string[]>;
	const audiences = ['followers', 'public'] as const;
	const categories = $derived([
		{ key: 'segments', label: m.nav_segments() },
		{ key: 'best_efforts', label: m.comparison_records() },
		{ key: 'pace_speed', label: m.comparison_sharing_speed() },
		{ key: 'power', label: m.record_power() },
		{ key: 'hr', label: m.record_heart_rate() }
	]);
	let policy = $state<Policy | null>(null);
	let defaults = $state<Policy>({});
	let loading = $state(false);
	let saving = $state(false);
	let dirty = $state(false);
	let message = $state('');

	async function load() {
		if (policy || loading) return;
		loading = true;
		message = '';
		try {
			const response = await fetch('/api/profile/visibility');
			if (!response.ok) throw new Error();
			const data = (await response.json()) as { policy: Policy; default_policy: Policy };
			defaults = data.default_policy;
			policy = data.policy;
		} catch {
			message = m.comparison_sharing_load_failed();
		} finally {
			loading = false;
		}
	}

	function toggle(audience: string, category: string, enabled: boolean) {
		if (!policy) return;
		const current = policy[audience] ?? defaults[audience] ?? [];
		const next = enabled ? [...current, category] : current.filter((value) => value !== category);
		policy = { ...policy, [audience]: next };
		dirty = true;
		message = '';
	}

	async function save() {
		saving = true;
		message = '';
		try {
			const response = await fetch('/api/profile/visibility', {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ policy })
			});
			if (!response.ok) throw new Error();
			dirty = false;
			message = m.comparison_sharing_saved();
		} catch {
			message = m.comparison_sharing_save_failed();
		} finally {
			saving = false;
		}
	}

	onMount(() => {
		if (location.hash === '#comparison-sharing') void load();
	});
</script>

<section
	id="comparison-sharing"
	class="scroll-mt-20 rounded-lg border border-zinc-800 bg-panel p-5"
>
	<h2 class="text-sm font-medium">{m.comparison_sharing()}</h2>
	<p class="mt-2 text-sm text-quiet">{m.comparison_sharing_help()}</p>
	{#if policy}
		<div class="mt-4 grid gap-4 min-[420px]:grid-cols-2">
			{#each audiences as audience (audience)}
				<fieldset disabled={saving} class="space-y-2 text-sm">
					<legend class="mb-2 font-medium">
						{audience === 'followers'
							? m.comparison_privacy_followers()
							: m.comparison_privacy_public()}
					</legend>
					{#each categories as category (category.key)}
						<label class="flex items-center gap-2">
							<input
								type="checkbox"
								checked={(policy[audience] ?? defaults[audience] ?? []).includes(category.key)}
								onchange={(event) => toggle(audience, category.key, event.currentTarget.checked)}
							/>
							{category.label}
						</label>
					{/each}
				</fieldset>
			{/each}
		</div>
		<button
			type="button"
			onclick={save}
			disabled={!dirty || saving}
			class="mt-4 min-h-10 rounded-lg bg-accent-600 px-3 text-sm font-medium text-on-accent hover:bg-accent-500 disabled:opacity-50"
		>
			{m.comparison_sharing_save()}
		</button>
	{:else}
		<button
			type="button"
			onclick={load}
			disabled={loading}
			class="mt-2 min-h-10 text-sm text-accent-300 hover:underline disabled:opacity-50"
		>
			{loading ? m.ride_loading() : m.comparison_sharing_edit()}
		</button>
	{/if}
	{#if message}<p role="status" class="mt-2 text-sm text-quiet">{message}</p>{/if}
</section>
