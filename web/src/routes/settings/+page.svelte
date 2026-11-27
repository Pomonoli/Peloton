<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { invalidateAll } from '$app/navigation';
	import { m } from '#lib/paraglide/messages.js';
	import { page } from '$app/state';
	import PrivacyZonesSettings from '#lib/components/PrivacyZonesSettings.svelte';
	import LocaleSwitcher from '#lib/components/LocaleSwitcher.svelte';
	import { locales } from '#lib/paraglide/runtime.js';
	import { appearance, setAppearance } from '#lib/appearance.js';
	import PasskeyManager from '#lib/components/PasskeyManager.svelte';
	import TokenManager from '#lib/components/TokenManager.svelte';
	import AuthorizedApps from '#lib/components/AuthorizedApps.svelte';

	import MergePolicyEditor from '#lib/components/MergePolicyEditor.svelte';
	import { prefs, setPrefs } from '#lib/prefs.svelte.js';
	import { formatDate, formatDistance, formatElevation } from '#lib/format.js';
	import { connectClients } from '#lib/connect.js';
	import { DateFormat, UnitSystem } from '#lib/proto/cairn/v1/auth_pb.js';

	let { data } = $props();

	let displayName = $state(untrack(() => data.user.displayName));
	let savingDisplayName = $state(false);
	let displayNameFeedback = $state<'required' | 'saved' | 'error' | null>(null);

	async function saveDisplayName(event: SubmitEvent) {
		event.preventDefault();
		if (savingDisplayName) return;
		const name = displayName.trim();
		if (!name) {
			displayNameFeedback = 'required';
			return;
		}

		displayNameFeedback = null;
		savingDisplayName = true;
		try {
			const { user } = await connectClients(fetch, page.url.origin).auth.updateProfile({
				fieldMask: ['display_name'],
				displayName: name
			});
			displayName = user?.displayName ?? name;
			await invalidateAll();
			displayNameFeedback = 'saved';
		} catch {
			displayNameFeedback = 'error';
		} finally {
			savingDisplayName = false;
		}
	}

	// Seeded from the shared store (which the layout filled from the user).
	let dateFormat = $state(prefs.dateFormat);
	let units = $state(prefs.units);
	let saved = $state(false);
	let error = $state<string | null>(null);

	let quota = $state<{ used: number; limit: number; unlimited: boolean } | null>(null);
	onMount(async () => {
		try {
			const q = await fetch('/api/quota');
			if (q.ok) {
				const b = await q.json();
				quota = { used: b.activities_used, limit: b.activities_limit, unlimited: b.unlimited };
			}
		} catch {
			/* ignore */
		}
	});

	function dateEnum(s: string): DateFormat {
		return s === 'iso'
			? DateFormat.ISO
			: s === 'us'
				? DateFormat.US
				: s === 'eu'
					? DateFormat.EU
					: DateFormat.UNSPECIFIED;
	}
	function unitEnum(s: string): UnitSystem {
		return s === 'imperial' ? UnitSystem.IMPERIAL : UnitSystem.METRIC;
	}

	// Persist + apply immediately site-wide on any change.
	async function save() {
		error = null;
		setPrefs({ dateFormat, units });
		try {
			const clients = connectClients(fetch, page.url.origin);
			await clients.auth.updateUserPreferences({
				dateFormat: dateEnum(dateFormat),
				timeFormat: data.user.timeFormat,
				units: unitEnum(units)
			});
			saved = true;
			setTimeout(() => (saved = false), 1500);
		} catch (e) {
			error = (e as Error).message;
		}
	}

	const sampleIso = '2026-06-02T14:30:00Z';
	const appearanceOptions = [
		{ value: 'system', label: m.settings_appearance_system() },
		{ value: 'light', label: m.settings_appearance_light() },
		{ value: 'dark', label: m.settings_appearance_dark() }
	] as const;
</script>

<div class="space-y-4 md:space-y-6">
	<header>
		<h1 class="page-title">{m.settings_title()}</h1>
		<p class="mt-1 text-sm text-zinc-400">{m.settings_intro()}</p>
	</header>

	<div class="settings-grid">
		<div class="space-y-3 md:space-y-4">
			<section class="surface p-3 sm:p-5">
				<h2 class="section-title mb-3">{m.settings_account_section()}</h2>
				<form
					class="mb-3 grid grid-cols-1 gap-2 text-sm sm:grid-cols-[160px_minmax(0,1fr)] sm:gap-3"
					novalidate
					onsubmit={saveDisplayName}
				>
					<label for="display-name" class="text-zinc-500 sm:pt-2">
						{m.settings_field_display_name()}
					</label>
					<div class="space-y-2">
						<input
							id="display-name"
							name="display-name"
							type="text"
							autocomplete="nickname"
							class="field w-full"
							bind:value={displayName}
							oninput={() => (displayNameFeedback = null)}
							aria-invalid={displayNameFeedback === 'required'}
							aria-describedby="display-name-feedback"
							required
							readonly={savingDisplayName}
						/>
						<button type="submit" class="action action-primary" disabled={savingDisplayName}>
							{savingDisplayName
								? m.settings_saving_display_name()
								: m.settings_save_display_name()}
						</button>
						<div
							id="display-name-feedback"
							role="status"
							class="text-xs"
							class:text-emerald-400={displayNameFeedback === 'saved'}
							class:text-red-400={displayNameFeedback !== 'saved'}
						>
							{#if displayNameFeedback === 'required'}
								{m.settings_display_name_required()}
							{:else if displayNameFeedback === 'saved'}
								{m.settings_display_name_saved()}
							{:else if displayNameFeedback === 'error'}
								{m.settings_display_name_save_failed()}
							{/if}
						</div>
					</div>
				</form>
				<dl
					class="grid grid-cols-1 gap-x-3 gap-y-1 text-sm wrap-anywhere sm:grid-cols-[160px_minmax(0,1fr)] sm:gap-y-3"
				>
					<dt class="text-zinc-500">{m.settings_field_username()}</dt>
					<dd class="text-zinc-200">{data.user.username}</dd>
					<dt class="text-zinc-500">{m.settings_field_email()}</dt>
					<dd class="text-zinc-200">{data.user.email || m.placeholder_dash()}</dd>
					{#if quota && !quota.unlimited}
						<dt class="text-zinc-500">Activity quota</dt>
						<dd class="text-zinc-200">
							{quota.used} / {quota.limit}
							{#if quota.used >= quota.limit}<span class="ml-2 text-red-400">(limit reached)</span
								>{/if}
						</dd>
					{/if}
				</dl>
			</section>

			<a
				href="/connections"
				class="block surface p-3 sm:p-5 text-sm font-medium text-accent-300 hover:border-accent-400"
				>{m.nav_connections()}</a
			>

			<section class="surface p-3 sm:p-5">
				<h2 class="section-title mb-1">{m.settings_appearance()}</h2>
				<p id="appearance-description" class="mb-3 text-xs text-zinc-500">
					{m.settings_appearance_description()}
				</p>
				<fieldset class="inline-flex gap-0.5 rounded-lg border border-zinc-800 bg-zinc-950 p-1">
					<legend class="sr-only">{m.settings_appearance()}</legend>
					{#each appearanceOptions as option (option.value)}
						<label class="cursor-pointer">
							<input
								type="radio"
								name="appearance"
								value={option.value}
								checked={$appearance.preference === option.value}
								aria-describedby="appearance-description"
								onchange={() => setAppearance(option.value)}
								class="peer sr-only"
							/>
							<span
								class="block rounded-md px-3 py-2 text-sm text-zinc-400 hover:bg-zinc-800 peer-checked:bg-accent-500/15 peer-checked:font-medium peer-checked:text-accent-300 peer-focus-visible:outline-2 peer-focus-visible:-outline-offset-2 peer-focus-visible:outline-accent-500"
							>
								{option.label}
							</span>
						</label>
					{/each}
				</fieldset>
			</section>

			<section class="surface p-3 sm:p-5">
				<div class="mb-4 flex items-center gap-3">
					<h2 class="text-sm font-medium text-zinc-300">Format &amp; units</h2>
					{#if saved}<span class="text-xs text-emerald-400">saved</span>{/if}
					{#if error}<span class="text-xs text-red-400">{error}</span>{/if}
				</div>
				<p class="mb-4 text-xs text-zinc-500">
					Applied everywhere, on every device you sign in on.
				</p>

				<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
					<div>
						<label for="date-fmt" class="mb-1 block text-xs text-zinc-500">Date format</label>
						<select id="date-fmt" bind:value={dateFormat} onchange={save} class="field w-full">
							<option value="">Locale default</option>
							<option value="iso">ISO — 2026-06-02</option>
							<option value="us">US — 06/02/2026</option>
							<option value="eu">EU — 02.06.2026</option>
						</select>
					</div>
					<div>
						<label for="unit-fmt" class="mb-1 block text-xs text-zinc-500">Units</label>
						<select id="unit-fmt" bind:value={units} onchange={save} class="field w-full">
							<option value="metric">Metric — km, m</option>
							<option value="imperial">Imperial — mi, ft</option>
						</select>
					</div>
				</div>

				<p class="mt-4 text-xs text-zinc-500">
					Preview: <span class="text-zinc-300">{formatDate(sampleIso)}</span> ·
					<span class="metric-value text-zinc-300">{formatDistance(10500)}</span> ·
					<span class="metric-value text-zinc-300">{formatElevation(420)}</span>
				</p>
			</section>

			{#if locales.length > 1}
				<section class="surface p-3 sm:p-5">
					<h2 class="section-title mb-3">{m.settings_language_section()}</h2>
					<LocaleSwitcher />
				</section>
			{/if}

			<a
				href="https://github.com/Pomonoli/Peloton"
				class="inline-flex min-h-10 items-center text-sm text-accent-300 hover:underline"
				>{m.settings_source_code()}</a
			>
		</div>
		<div class="space-y-3 md:space-y-4">
			<PrivacyZonesSettings />
			<MergePolicyEditor />
			<PasskeyManager />
			<TokenManager />
			<AuthorizedApps />
		</div>
	</div>
</div>

<style>
	.settings-grid {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: 0.75rem;
		align-items: start;
	}

	@media (width >= 68rem) {
		.settings-grid {
			grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.2fr);
			gap: 1rem;
		}
	}
</style>
