<script lang="ts">
	import { SvelteURLSearchParams } from 'svelte/reactivity';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import ComparisonRider from '#lib/components/ComparisonRider.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import SectionTabs from '#lib/components/SectionTabs.svelte';
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

	const self = $derived(
		members.find((member) => member.id === data.user.id) ?? {
			id: data.user.id,
			username: data.user.username,
			display_name: data.user.displayName,
			avatar_url: data.user.avatarUrl ?? ''
		}
	);
	const firstListed = $derived(
		data.first
			? members.find((member) => member.username === data.first || member.id === data.first)
			: self
	);
	const you = $derived(comparison?.first ?? firstListed ?? self);
	const opponents = $derived(members.filter((member) => member.id !== firstListed?.id));
	const listed = $derived(
		opponents.find((member) => member.username === data.member || member.id === data.member)
	);
	const opponent = $derived(comparison?.opponent ?? listed);
	const firstName = $derived(riderName(you));
	const opponentName = $derived(opponent ? riderName(opponent) : m.comparison_opponent());
	const score = $derived(tab === 'segments' ? comparison?.segment_score : comparison?.record_score);
	const segments = $derived(segmentRows(comparison?.segments ?? [], opponentName, firstName));
	const records = $derived(recordRows(comparison?.records ?? [], opponentName, firstName));
	const performanceColumns = $derived<Column[]>([
		{ key: 'you', label: firstName },
		{ key: 'opponent', label: opponentName },
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
			const response = await fetch('/api/riders', { signal: request.signal });
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
		const { first, member } = data;
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
				const response = await fetch(
					`/api/head-to-head?${new URLSearchParams({ first, second: member })}`,
					{
						signal
					}
				);
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
		return () => {
			controller.abort();
			retry = undefined;
		};
	});

	function selectPair(first: string, second: string) {
		const query = new SvelteURLSearchParams();
		if (first && first !== self.username) query.set('first', first);
		if (second) query.set('member', second);
		if (data.viewedMember) query.set('viewing', data.viewedMember.username);
		void goto(query.size ? `/head-to-head?${query}` : '/head-to-head', { reset: false });
	}
</script>

<svelte:head><title>{m.comparison_title()}</title></svelte:head>

<section class="mx-auto min-w-0 max-w-6xl space-y-3 md:space-y-4">
	<header>
		<h1 class="page-title">{m.comparison_title()}</h1>
		<p class="mt-1 text-sm text-quiet">{m.comparison_pair_intro()}</p>
	</header>
	<div class="comparison-pickers">
		<div class="surface rider-picker first">
			<label for="comparison-first">{m.comparison_first_rider()}</label>
			<div class="flex min-w-0 items-center gap-1">
				<ComparisonRider rider={you} />
				<select
					id="comparison-first"
					value={firstListed?.username ?? ''}
					onchange={(event) => selectPair(event.currentTarget.value, data.member)}
					disabled={directoryLoading || directoryError}
					class="field w-full"
				>
					{#each members as rider (rider.id)}
						<option value={rider.username} disabled={rider.id === listed?.id}
							>{riderName(rider)}</option
						>
					{/each}
				</select>
			</div>
		</div>
		<button
			type="button"
			class="action swap-riders"
			aria-label={m.comparison_swap()}
			title={m.comparison_swap()}
			disabled={!listed || !firstListed || directoryLoading || directoryError}
			onclick={() => {
				if (listed && firstListed) selectPair(listed.username, firstListed.username);
			}}><Icon name="swap" size={18} /></button
		>
		<div class="surface rider-picker second">
			<label for="comparison-member">{m.comparison_second_rider()}</label>
			<div class="flex min-w-0 items-center gap-1">
				<ComparisonRider rider={opponent} />
				<select
					id="comparison-member"
					value={listed?.username ?? ''}
					onchange={(event) => selectPair(firstListed?.username ?? '', event.currentTarget.value)}
					disabled={directoryLoading || directoryError}
					class="field w-full"
				>
					<option value="">{m.comparison_choose_member()}</option>
					{#each opponents as rider (rider.id)}
						<option value={rider.username}>{riderName(rider)}</option>
					{/each}
				</select>
			</div>
		</div>
	</div>

	{#if directoryError}
		<p role="status" class="text-sm text-quiet">{m.friends_failed()}</p>
		<button
			type="button"
			onclick={loadDirectory}
			class="min-h-10 text-sm text-accent-300 hover:underline">{m.community_retry()}</button
		>
	{/if}

	{#if loading || (directoryLoading && !comparison)}
		<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
	{:else if error}
		<p role="status" class="surface p-4 text-sm text-quiet">
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
		<p class="surface p-4 text-sm text-quiet">
			{opponents.length ? m.comparison_pair_intro() : m.comparison_no_members()}
		</p>
	{/if}

	{#if comparison && score}
		<div class="surface comparison-score">
			<dl class="score-grid" aria-live="polite">
				{#each [{ label: m.comparison_rider_wins( { rider: firstName } ), value: score.wins, key: 'first' }, { label: m.comparison_ties(), value: score.ties, key: 'tie' }, { label: m.comparison_rider_wins( { rider: opponentName } ), value: score.losses, key: 'second' }] as stat (stat.key)}
					<div class={stat.key}>
						<dt>{stat.label}</dt>
						<dd>{number.format(stat.value)}</dd>
					</div>
				{/each}
				<div class="score-total">
					<dt>{m.comparison_total()}</dt>
					<dd>{number.format(score.comparable)}</dd>
				</div>
			</dl>
			<div class="score-bar" aria-hidden="true">
				{#if score.comparable > 0}
					<span class="first" style:flex-grow={score.wins}></span>
					<span class="tie" style:flex-grow={score.ties}></span>
					<span class="second" style:flex-grow={score.losses}></span>
				{/if}
			</div>
		</div>
		<SectionTabs
			{tabs}
			active={tab}
			id="comparison"
			label={m.comparison_title()}
			onchange={(next) => (tab = next)}
		/>

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
				first={firstName}
				searchLabel={m.comparison_search_segments()}
				empty={m.comparison_no_segments()}
				descendingResults
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
				first={firstName}
				searchLabel={m.comparison_search_records()}
				empty={m.comparison_no_records()}
				defaultSort="category"
			/>
		</div>
	{/if}
</section>

<style>
	.comparison-pickers {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
		align-items: center;
		gap: clamp(0.25rem, 1vw, 0.75rem);
		max-width: 44rem;
	}

	.rider-picker {
		min-width: 0;
		padding: 0.5rem;
		color: var(--rider-color);
	}

	.rider-picker label {
		display: block;
		padding-inline: 0.2rem;
		font-size: 0.6875rem;
		font-weight: 650;
	}

	.rider-picker select {
		padding-inline: 0.25rem;
		border-color: transparent;
		background: transparent;
		font-size: 0.8125rem;
		font-weight: 650;
		text-overflow: ellipsis;
	}

	.swap-riders {
		width: 2.75rem;
		padding: 0;
	}

	.first {
		--rider-color: var(--rider-first);
		--rider-wash: var(--rider-first-wash);
	}

	.second {
		--rider-color: var(--rider-second);
		--rider-wash: var(--rider-second-wash);
	}

	.tie {
		--rider-color: var(--rider-tie);
	}

	.comparison-score {
		display: grid;
		gap: 0.45rem;
		padding: 0.75rem;
	}

	.score-grid {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		row-gap: 0.4rem;
		text-align: center;
	}

	.score-grid > div + div:not(.score-total) {
		border-inline-start: 1px solid var(--line-subtle);
	}

	.score-grid dt {
		color: var(--text-muted);
		font-size: 0.6875rem;
		font-weight: 600;
		overflow-wrap: anywhere;
	}

	.score-grid dd {
		font-size: clamp(1.125rem, 4vw, 1.5rem);
		font-weight: 750;
		font-variant-numeric: tabular-nums;
		letter-spacing: -0.035em;
	}

	.score-grid :is(.first, .second) dd {
		color: var(--rider-color);
	}

	.score-grid .score-total {
		display: flex;
		grid-column: 1 / -1;
		justify-content: flex-end;
		align-items: baseline;
		gap: 0.3rem;
		color: var(--text-muted);
	}

	.score-total dd {
		font-size: 0.6875rem;
	}

	.score-bar {
		display: flex;
		height: 0.5rem;
		overflow: hidden;
		border-radius: 2rem;
		background: var(--surface-muted);
	}

	.score-bar span {
		flex-basis: 0;
		background: var(--rider-color);
	}

	@media (width >= 48rem) {
		.rider-picker,
		.comparison-score {
			padding: 0.875rem;
		}

		.score-grid dt {
			font-size: 0.75rem;
		}
	}
</style>
