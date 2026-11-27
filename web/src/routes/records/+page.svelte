<script lang="ts">
	import MetricReadout from '#lib/components/MetricReadout.svelte';
	import { SvelteURLSearchParams } from 'svelte/reactivity';
	import { memberURL } from '#lib/member.js';
	import { APP_NAME } from '$app/env/public';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages.js';
	import ExcludeDownhills from '#lib/components/ExcludeDownhills.svelte';
	import RecordLeaderboards from '#lib/components/RecordLeaderboards.svelte';
	import {
		compareRecordCategories,
		recordLabel,
		recordSport,
		recordValue,
		type PersonalRecord
	} from '#lib/records.js';
	import SportIcon from '#lib/components/SportIcon.svelte';
	import { formatDateOnly } from '#lib/format.js';

	let { data } = $props();

	const excludeDownhills = $derived(page.url.searchParams.get('exclude_downhills') === 'true');
	const boards = $derived(page.url.searchParams.get('tab') === 'leaderboards');
	let records = $state<PersonalRecord[]>([]);
	let loaded = $state(false);
	let error = $state<string | null>(null);

	$effect(() => {
		const url = memberURL(
			excludeDownhills ? '/api/records?exclude_downhills=true' : '/api/records',
			data.member
		);
		if (boards) return;
		const controller = new AbortController();
		records = [];
		loaded = false;
		error = null;
		void fetch(url, { signal: controller.signal })
			.then(async (response) => {
				if (!response.ok) throw new Error(await response.text());
				const body = await response.json();
				if (!controller.signal.aborted) records = body.records ?? [];
			})
			.catch((cause) => {
				if (!controller.signal.aborted) error = cause.message;
			})
			.finally(() => {
				if (!controller.signal.aborted) loaded = true;
			});
		return () => controller.abort();
	});

	function tabURL(leaderboards: boolean) {
		const query = new SvelteURLSearchParams();
		if (leaderboards) query.set('tab', 'leaderboards');
		if (excludeDownhills) query.set('exclude_downhills', 'true');
		return memberURL(query.size ? `/records?${query}` : '/records', data.member);
	}

	// Group by activity type, preserving a sensible type order.
	const TYPE_ORDER = ['ride', 'run', 'swim', 'walk', 'hike', 'workout'];
	let byType = $derived.by(() => {
		// eslint-disable-next-line svelte/prefer-svelte-reactivity -- rebuilt inside $derived, never mutated after
		const m = new Map<string, PersonalRecord[]>();
		for (const r of records) {
			if (!m.has(r.activity_type)) m.set(r.activity_type, []);
			m.get(r.activity_type)!.push(r);
		}
		for (const list of m.values()) list.sort(compareRecordCategories);
		return [...m.entries()].sort(
			(a, b) => typeRank(a[0]) - typeRank(b[0]) || a[0].localeCompare(b[0])
		);
	});
	function typeRank(t: string): number {
		const i = TYPE_ORDER.indexOf(t);
		return i === -1 ? 999 : i;
	}
</script>

<svelte:head><title>Records · {APP_NAME}</title></svelte:head>

<div class="mx-auto max-w-4xl space-y-3">
	<h1 class="page-title">{m.member_records()}</h1>
	{#snippet downhillFilter()}
		<ExcludeDownhills checked={excludeDownhills} />
	{/snippet}
	{#snippet recordTabs()}
		<nav class="inline-flex gap-1 surface p-1" aria-label={m.member_records()}>
			<a
				href={tabURL(false)}
				aria-current={!boards ? 'page' : undefined}
				class="flex min-h-10 items-center rounded-md px-3 text-sm font-semibold"
				class:bg-accent-500={!boards}
				class:text-on-accent={!boards}
			>
				{data.member ? m.record_member_records() : m.record_my_records()}
			</a>
			<a
				href={tabURL(true)}
				aria-current={boards ? 'page' : undefined}
				class="flex min-h-10 items-center rounded-md px-3 text-sm font-semibold"
				class:bg-accent-500={boards}
				class:text-on-accent={boards}
			>
				{m.record_leaderboards()}
			</a>
		</nav>
	{/snippet}
	{#if !boards}{@render recordTabs()}{/if}
	{#if !boards}{@render downhillFilter()}{/if}

	{#if boards}
		<RecordLeaderboards
			member={data.member}
			{excludeDownhills}
			toolbar={recordTabs}
			afterToolbar={downhillFilter}
		/>
	{:else if !loaded}
		<p class="text-sm text-quiet">Loading…</p>
	{:else if error}
		<p class="text-sm text-red-400">{error}</p>
	{:else if records.length === 0}
		<p class="text-sm text-zinc-500">
			{excludeDownhills
				? m.record_no_results()
				: 'No records yet. Records appear after importing activities with enough recorded data.'}
		</p>
	{:else}
		<div class="space-y-4">
			{#each byType as [type, list] (type)}
				<section>
					<h2 class="mb-2 flex items-center gap-2 text-sm font-semibold text-zinc-300">
						<SportIcon {type} size={16} />
						{recordSport(type)}
					</h2>
					<div class="divide-y divide-zinc-800 overflow-hidden surface">
						{#each list as r (r.metric + r.window_kind + r.window_value)}
							<a
								href={memberURL(
									`/activities/${r.activity_id}${excludeDownhills ? '?exclude_downhills=true' : ''}`,
									data.member
								)}
								class="grid min-h-12 grid-cols-[minmax(0,1fr)_auto] sm:grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-x-2 px-2.5 py-1.5 hover:bg-zinc-950 sm:gap-x-4 sm:px-4"
							>
								<div class="text-xs font-semibold text-zinc-300 sm:text-sm">
									{recordLabel(r, false)}
								</div>
								<div
									class="max-w-40 text-right text-sm font-bold tabular-nums sm:max-w-none sm:text-base"
								>
									<MetricReadout value={recordValue(r, r.achieved_value)} />
								</div>
								<div class="text-[11px] text-quiet sm:w-28 sm:text-right sm:text-xs">
									{formatDateOnly(r.timestamp)}
								</div>
							</a>
						{/each}
					</div>
				</section>
			{/each}
		</div>
	{/if}
</div>
