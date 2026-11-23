<script lang="ts">
	import { onMount } from 'svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { page } from '$app/state';
	import ComparisonSharing from '#lib/components/ComparisonSharing.svelte';
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
	import { DateFormat, TimeFormat, UnitSystem } from '#lib/proto/cairn/v1/auth_pb.js';

	let { data } = $props();

	// Seeded from the shared store (which the layout filled from the user).
	let dateFormat = $state(prefs.dateFormat);
	let timeFormat = $state(prefs.timeFormat);
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
	function timeEnum(s: string): TimeFormat {
		return s === '24h'
			? TimeFormat.TIME_FORMAT_24H
			: s === '12h'
				? TimeFormat.TIME_FORMAT_12H
				: TimeFormat.TIME_FORMAT_UNSPECIFIED;
	}
	function unitEnum(s: string): UnitSystem {
		return s === 'imperial' ? UnitSystem.IMPERIAL : UnitSystem.METRIC;
	}

	// Persist + apply immediately site-wide on any change.
	async function save() {
		error = null;
		setPrefs({ dateFormat, timeFormat, units });
		try {
			const clients = connectClients(fetch, page.url.origin);
			await clients.auth.updateUserPreferences({
				dateFormat: dateEnum(dateFormat),
				timeFormat: timeEnum(timeFormat),
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
		<h1 class="text-2xl font-semibold tracking-tight">{m.settings_title()}</h1>
		<p class="mt-1 text-sm text-zinc-400">{m.settings_intro()}</p>
	</header>

	<ComparisonSharing />

	<section class="rounded-lg border border-zinc-800 bg-panel p-3 sm:p-5">
		<h2 class="mb-4 text-sm font-medium text-zinc-300">{m.settings_account_section()}</h2>
		<dl class="grid grid-cols-1 gap-3 text-sm sm:grid-cols-[160px_1fr]">
			<dt class="text-zinc-500">{m.settings_field_display_name()}</dt>
			<dd class="text-zinc-200">{data.user.displayName}</dd>
			<dt class="text-zinc-500">{m.settings_field_username()}</dt>
			<dd class="text-zinc-200">{data.user.username}</dd>
			<dt class="text-zinc-500">{m.settings_field_email()}</dt>
			<dd class="text-zinc-200">{data.user.email || m.placeholder_dash()}</dd>
			{#if quota && !quota.unlimited}
				<dt class="text-zinc-500">Activity quota</dt>
				<dd class="text-zinc-200">
					{quota.used} / {quota.limit}
					{#if quota.used >= quota.limit}<span class="ml-2 text-red-400">(limit reached)</span>{/if}
				</dd>
			{/if}
		</dl>
	</section>

	<a
		href="/connections"
		class="block rounded-lg border border-zinc-800 bg-panel p-3 sm:p-5 text-sm font-medium text-accent-300 hover:border-accent-400"
		>{m.nav_connections()}</a
	>

	<section class="rounded-lg border border-zinc-800 bg-panel p-3 sm:p-5">
		<h2 class="mb-1 text-sm font-medium text-zinc-300">{m.settings_appearance()}</h2>
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

	<section class="rounded-lg border border-zinc-800 bg-panel p-3 sm:p-5">
		<div class="mb-4 flex items-center gap-3">
			<h2 class="text-sm font-medium text-zinc-300">Format &amp; units</h2>
			{#if saved}<span class="text-xs text-emerald-400">saved</span>{/if}
			{#if error}<span class="text-xs text-red-400">{error}</span>{/if}
		</div>
		<p class="mb-4 text-xs text-zinc-500">Applied everywhere, on every device you sign in on.</p>

		<div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
			<div>
				<label for="date-fmt" class="mb-1 block text-xs text-zinc-500">Date format</label>
				<select
					id="date-fmt"
					bind:value={dateFormat}
					onchange={save}
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm focus:border-accent-400 focus:outline-none"
				>
					<option value="">Locale default</option>
					<option value="iso">ISO — 2026-06-02</option>
					<option value="us">US — 06/02/2026</option>
					<option value="eu">EU — 02.06.2026</option>
				</select>
			</div>
			<div>
				<label for="time-fmt" class="mb-1 block text-xs text-zinc-500">Time format</label>
				<select
					id="time-fmt"
					bind:value={timeFormat}
					onchange={save}
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm focus:border-accent-400 focus:outline-none"
				>
					<option value="">Locale default</option>
					<option value="24h">24-hour — 14:30</option>
					<option value="12h">12-hour — 2:30 PM</option>
				</select>
			</div>
			<div>
				<label for="unit-fmt" class="mb-1 block text-xs text-zinc-500">Units</label>
				<select
					id="unit-fmt"
					bind:value={units}
					onchange={save}
					class="w-full rounded border border-zinc-700 bg-zinc-900 px-2 py-1.5 text-sm focus:border-accent-400 focus:outline-none"
				>
					<option value="metric">Metric — km, m</option>
					<option value="imperial">Imperial — mi, ft</option>
				</select>
			</div>
		</div>

		<p class="mt-4 text-xs text-zinc-500">
			Preview: <span class="text-zinc-300">{formatDate(sampleIso)}</span> ·
			<span class="text-zinc-300">{formatDistance(10500)}</span> ·
			<span class="text-zinc-300">{formatElevation(420)}</span>
		</p>
	</section>

	{#if locales.length > 1}
		<section class="rounded-lg border border-zinc-800 bg-panel p-3 sm:p-5">
			<h2 class="mb-4 text-sm font-medium text-zinc-300">{m.settings_language_section()}</h2>
			<LocaleSwitcher />
		</section>
	{/if}

	<MergePolicyEditor />

	<PasskeyManager />

	<TokenManager />

	<AuthorizedApps />
</div>
