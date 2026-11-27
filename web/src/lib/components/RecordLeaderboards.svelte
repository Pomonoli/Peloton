<script lang="ts">
	import MetricReadout from '#lib/components/MetricReadout.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import { intlLocale } from '#lib/format.js';
	import { memberURL, type MemberView } from '#lib/member.js';
	import {
		filterCommunityRecords,
		recordLabel,
		recordMetric,
		recordPath,
		recordSport,
		recordValue,
		type CommunityRecord,
		type RecordOrder
	} from '#lib/records.js';
	import type { Snippet } from 'svelte';
	import FilterSheet from './FilterSheet.svelte';
	import SportIcon from './SportIcon.svelte';

	let {
		member,
		excludeDownhills,
		toolbar,
		afterToolbar
	}: {
		member: MemberView | null;
		excludeDownhills: boolean;
		toolbar: Snippet;
		afterToolbar: Snippet;
	} = $props();
	let rows = $state<CommunityRecord[]>([]);
	let loading = $state(true);
	let failed = $state(false);
	let sport = $state('');
	let metric = $state('');
	let showEmpty = $state(false);
	let order = $state<RecordOrder>('category');
	const sports = $derived([...new Set(rows.map((row) => row.activity_type))]);
	const metrics = $derived([...new Set(rows.map((row) => row.metric))]);
	const grouped = $derived.by(() => {
		const filtered = filterCommunityRecords(rows, sport, metric, showEmpty, order);
		const bySport: Record<string, CommunityRecord[]> = {};
		for (const row of filtered) (bySport[row.activity_type] ??= []).push(row);
		return Object.entries(bySport);
	});

	$effect(() => {
		const url = excludeDownhills
			? '/api/records/leaderboards?exclude_downhills=true'
			: '/api/records/leaderboards';
		const controller = new AbortController();
		loading = true;
		failed = false;
		void fetch(url, { signal: controller.signal })
			.then(async (response) => {
				if (!response.ok) throw new Error();
				const body = await response.json();
				if (!controller.signal.aborted) rows = body.records;
			})
			.catch(() => {
				if (!controller.signal.aborted) failed = true;
			})
			.finally(() => {
				if (!controller.signal.aborted) loading = false;
			});
		return () => controller.abort();
	});

	const control = 'min-h-10 rounded border border-zinc-700 bg-panel px-2';
</script>

<FilterSheet
	{toolbar}
	belowToolbar={afterToolbar}
	active={!!sport || !!metric || showEmpty || order !== 'category'}
	reset={() => {
		sport = '';
		metric = '';
		showEmpty = false;
		order = 'category';
	}}
>
	<div class="mt-1 flex flex-wrap items-end gap-3 text-sm">
		<label class="flex min-w-0 flex-col gap-1 max-md:w-full">
			{m.record_sport()}
			<select bind:value={sport} class={control}>
				<option value="">{m.record_all_sports()}</option>
				{#each sports as option (option)}
					<option value={option}>{recordSport(option)}</option>
				{/each}
			</select>
		</label>
		<label class="flex min-w-0 flex-col gap-1 max-md:w-full">
			{m.record_metric()}
			<select bind:value={metric} class={control}>
				<option value="">{m.record_all_metrics()}</option>
				{#each metrics as option (option)}
					<option value={option}>{recordMetric(option)}</option>
				{/each}
			</select>
		</label>
		<label class="flex min-w-0 flex-col gap-1 max-md:w-full">
			{m.record_sort()}
			<select bind:value={order} class={control}>
				<option value="category">{m.record_sort_category()}</option>
				<option value="members">{m.record_sort_members()}</option>
				<option value="holder">{m.record_sort_holder()}</option>
			</select>
		</label>
		<label class="inline-flex min-h-10 items-center gap-2">
			<input type="checkbox" bind:checked={showEmpty} />
			{m.record_show_empty()}
		</label>
	</div>
</FilterSheet>

{#if loading}
	<p class="text-sm text-quiet" role="status">{m.community_loading()}</p>
{:else if failed}
	<p class="text-sm text-red-400" role="status">{m.record_board_failed()}</p>
{:else}
	{#if grouped.length === 0}
		<p class="text-sm text-quiet">{m.record_no_results()}</p>
	{:else}
		{#each grouped as [type, records] (type)}
			<section class="mb-4">
				<h2 class="mb-2 flex items-center gap-2 text-sm font-semibold">
					<SportIcon {type} size={16} />{recordSport(type)}
				</h2>
				<div class="divide-y divide-zinc-800 overflow-hidden surface">
					{#each records as row (recordPath(row))}
						<a
							href={memberURL(
								recordPath(row) + (excludeDownhills ? '?exclude_downhills=true' : ''),
								member
							)}
							class="grid min-h-12 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 px-3 py-2 hover:bg-zinc-950 sm:px-4"
						>
							<div class="text-sm font-medium">
								{recordLabel(row)}
							</div>
							{#if row.result}
								<div class="text-right text-sm font-bold tabular-nums sm:text-base">
									<MetricReadout value={recordValue(row, row.result.achieved_value)} />
								</div>
								<div class="truncate text-xs text-quiet" title={row.result.display_name}>
									{row.result.display_name}
								</div>
							{:else}
								<div class="mt-1 text-sm text-quiet">{m.record_no_result()}</div>
							{/if}
							<div class="text-right text-xs text-quiet">
								{m.record_member_count({
									formatted: row.member_count.toLocaleString(intlLocale())
								})}
							</div>
						</a>
					{/each}
				</div>
			</section>
		{/each}
	{/if}
{/if}
