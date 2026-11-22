<script lang="ts">
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import ComparisonRider from '#lib/components/ComparisonRider.svelte';
	import ComparisonTable from '#lib/components/ComparisonTable.svelte';
	import { intlLocale } from '#lib/format.js';
	import {
		recordRows,
		riderName,
		segmentRows,
		type Comparison,
		type Column,
		type Rider
	} from '#lib/head-to-head.js';
	import { memberURL } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import type { PageData } from './$types';

	let { data }: { data: PageData } = $props();
	let members = $state<Rider[]>([]);
	let directoryLoading = $state(true);
	let directoryError = $state(false);
	let comparison = $state<Comparison | null>(null);
	let loading = $state(false);
	let error = $state('');
	let retry: (() => void) | undefined;
	let tab = $state<'segments' | 'records'>('segments');

	const you = $derived(
		members.find((member) => member.id === data.user.id) ?? {
			id: data.user.id,
			username: data.user.username,
			display_name: data.user.displayName,
			avatar_url: data.user.avatarUrl ?? ''
		}
	);
	const opponents = $derived(members.filter((member) => member.id !== you.id));
	const listed = $derived(
		opponents.find((member) => member.username === data.member || member.id === data.member)
	);
	const opponent = $derived(comparison?.opponent ?? listed);
	const opponentName = $derived(opponent ? riderName(opponent) : m.comparison_opponent());
	const score = $derived(tab === 'segments' ? comparison?.segment_score : comparison?.record_score);
	const segments = $derived(segmentRows(comparison?.segments ?? [], opponentName));
	const records = $derived(recordRows(comparison?.records ?? [], opponentName));
	const performanceColumns = $derived<Column[]>([
		{ key: 'you', label: m.comparison_you() },
		{ key: 'opponent', label: opponentName },
		{ key: 'difference', label: m.comparison_difference() },
		{ key: 'winner', label: m.comparison_winner() }
	]);
	const segmentColumns = $derived<Column[]>([
		{ key: 'name', label: m.comparison_segment() },
		{ key: 'distance', label: m.comparison_distance() },
		...performanceColumns
	]);
	const recordColumns = $derived<Column[]>([
		{ key: 'name', label: m.comparison_category() },
		...performanceColumns
	]);
	const tabs = $derived([
		{ key: 'segments', label: m.nav_segments() },
		{ key: 'records', label: m.comparison_records() }
	] as const);
	const number = $derived(new Intl.NumberFormat(intlLocale()));
	let directoryRequest: AbortController | undefined;

	async function loadDirectory() {
		directoryRequest?.abort();
		const request = new AbortController();
		directoryRequest = request;
		directoryLoading = true;
		directoryError = false;
		try {
			const response = await fetch('/api/friends', { signal: request.signal });
			if (!response.ok) throw new Error();
			const body = (await response.json()) as { members: Rider[] };
			if (!request.signal.aborted) members = body.members;
		} catch {
			if (!request.signal.aborted) directoryError = true;
		} finally {
			if (!request.signal.aborted) directoryLoading = false;
		}
	}

	onMount(() => {
		void loadDirectory();
		return () => directoryRequest?.abort();
	});

	$effect(() => {
		const member = data.member;
		const controller = new AbortController();
		comparison = null;
		error = '';
		loading = !!member;
		if (!member) return;

		const { signal } = controller;
		let pending = false;
		async function refresh() {
			if (pending || signal.aborted) return;
			pending = true;
			try {
				const response = await fetch(`/api/head-to-head?member=${encodeURIComponent(member)}`, {
					signal
				});
				if (!response.ok) {
					if (!signal.aborted) {
						comparison = null;
						error =
							response.status === 404 || response.status === 400
								? m.comparison_member_unavailable()
								: m.comparison_failed();
					}
					return;
				}
				const next = (await response.json()) as Comparison;
				if (!signal.aborted) {
					comparison = next;
					error = '';
				}
			} catch {
				if (!signal.aborted) {
					comparison = null;
					error = m.comparison_failed();
				}
			} finally {
				pending = false;
				if (!signal.aborted) loading = false;
			}
		}
		retry = () => void refresh();
		void refresh();
		function refreshVisible() {
			if (document.visibilityState === 'visible') void refresh();
		}
		const timer = window.setInterval(refreshVisible, 60000);
		window.addEventListener('focus', refreshVisible);
		document.addEventListener('visibilitychange', refreshVisible);
		return () => {
			controller.abort();
			retry = undefined;
			window.clearInterval(timer);
			window.removeEventListener('focus', refreshVisible);
			document.removeEventListener('visibilitychange', refreshVisible);
		};
	});

	function selectMember(username: string) {
		const member = opponents.find((rider) => rider.username === username);
		void goto(memberURL('/head-to-head', member), { reset: false });
	}

	function moveTab(event: KeyboardEvent) {
		if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
		event.preventDefault();
		tab =
			event.key === 'Home'
				? 'segments'
				: event.key === 'End'
					? 'records'
					: tab === 'segments'
						? 'records'
						: 'segments';
		document.getElementById(`comparison-tab-${tab}`)?.focus();
	}
</script>

<svelte:head><title>{m.comparison_title()}</title></svelte:head>

<section class="mx-auto min-w-0 max-w-6xl space-y-4">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<h1 class="text-2xl font-semibold tracking-tight">{m.comparison_title()}</h1>
		<div class="min-w-0 w-full sm:w-64">
			<label for="comparison-member" class="mb-1 block text-xs text-quiet"
				>{m.comparison_select_member()}</label
			>
			<select
				id="comparison-member"
				value={listed?.username ?? ''}
				onchange={(event) => selectMember(event.currentTarget.value)}
				disabled={directoryLoading || directoryError}
				class="min-h-11 w-full rounded-lg border border-zinc-700 bg-panel px-3 text-sm"
			>
				<option value="">{m.comparison_choose_member()}</option>
				{#each opponents as rider (rider.id)}
					<option value={rider.username}>{riderName(rider)} (@{rider.username})</option>
				{/each}
			</select>
		</div>
	</header>

	{#if directoryError}
		<p role="status" class="text-sm text-quiet">{m.friends_failed()}</p>
		<button
			type="button"
			onclick={loadDirectory}
			class="min-h-10 text-sm text-accent-300 hover:underline">{m.community_retry()}</button
		>
	{/if}

	{#if opponent}
		<div class="grid grid-cols-2 gap-3 rounded-lg border border-zinc-800 bg-panel p-3 sm:p-4">
			<ComparisonRider rider={you} label={m.comparison_you()} />
			<ComparisonRider rider={opponent} label={m.comparison_opponent()} />
		</div>
	{/if}

	{#if loading || (directoryLoading && !comparison)}
		<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
	{:else if error}
		<p role="status" class="rounded-lg border border-zinc-800 bg-panel p-4 text-sm text-quiet">
			{error}
		</p>
		<button
			type="button"
			onclick={() => retry?.()}
			class="min-h-10 text-sm text-accent-300 hover:underline"
		>
			{m.community_retry()}
		</button>
	{:else if !comparison && !directoryError}
		<p class="rounded-lg border border-zinc-800 bg-panel p-4 text-sm text-quiet">
			{opponents.length ? m.comparison_intro() : m.comparison_no_members()}
		</p>
	{/if}

	{#if comparison && score}
		<div
			role="tablist"
			aria-label={m.comparison_title()}
			class="flex gap-2 border-b border-zinc-800"
		>
			{#each tabs as item (item.key)}
				<button
					type="button"
					role="tab"
					id={`comparison-tab-${item.key}`}
					aria-selected={tab === item.key}
					aria-controls={`comparison-panel-${item.key}`}
					tabindex={tab === item.key ? 0 : -1}
					onclick={() => (tab = item.key)}
					onkeydown={moveTab}
					class="min-h-11 border-b-2 px-3 text-sm font-medium {tab === item.key
						? 'border-accent-500 text-accent-300'
						: 'border-transparent text-quiet hover:text-accent-300'}"
				>
					{item.label}
				</button>
			{/each}
		</div>

		<dl
			class="grid grid-cols-2 gap-y-3 rounded-lg border border-zinc-800 bg-panel p-3 sm:grid-cols-4 sm:p-4"
			aria-live="polite"
		>
			{#each [{ label: m.comparison_wins(), value: score.wins }, { label: m.comparison_losses(), value: score.losses }, { label: m.comparison_ties(), value: score.ties }, { label: m.comparison_comparable(), value: score.comparable }] as stat (stat.label)}
				<div>
					<dt class="text-xs text-quiet">{stat.label}</dt>
					<dd class="text-xl font-semibold tabular-nums">{number.format(stat.value)}</dd>
				</div>
			{/each}
		</dl>

		<div
			role="tabpanel"
			id="comparison-panel-segments"
			aria-labelledby="comparison-tab-segments"
			hidden={tab !== 'segments'}
		>
			<ComparisonTable
				rows={segments}
				columns={segmentColumns}
				opponent={opponentName}
				searchLabel={m.comparison_search_segments()}
				empty={m.comparison_no_segments()}
				extraSort={[
					{ key: 'youSpeed', label: m.comparison_rider_speed({ rider: m.comparison_you() }) },
					{ key: 'opponentSpeed', label: m.comparison_rider_speed({ rider: opponentName }) },
					{ key: 'speedGap', label: m.comparison_speed_difference() }
				]}
			/>
		</div>
		<div
			role="tabpanel"
			id="comparison-panel-records"
			aria-labelledby="comparison-tab-records"
			hidden={tab !== 'records'}
		>
			{#if score.comparable === 0 && tab === 'records'}
				<p class="mb-3 text-sm text-quiet">{m.comparison_no_records()}</p>
			{/if}
			<ComparisonTable
				rows={records}
				columns={recordColumns}
				opponent={opponentName}
				searchLabel={m.comparison_search_records()}
				empty={m.comparison_no_records()}
				extraSort={[{ key: 'percentGap', label: m.comparison_percent_difference() }]}
			/>
		</div>
	{/if}
</section>
