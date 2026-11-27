<script lang="ts">
	import MetricReadout from '#lib/components/MetricReadout.svelte';
	import { formatDateOnly } from '#lib/format.js';
	import { memberURL } from '#lib/member.js';
	import { m } from '#lib/paraglide/messages.js';
	import { recordLabel, recordSport, recordValue } from '#lib/records.js';

	let { data } = $props();
	const { bucket, records } = $derived(data);
</script>

<svelte:head><title>{recordLabel(bucket)} · {m.record_leaderboards()}</title></svelte:head>

<section class="space-y-5">
	<header>
		<a
			href={memberURL(
				'/records?tab=leaderboards' + (data.excludeDownhills ? '&exclude_downhills=true' : ''),
				data.member
			)}
			class="text-sm text-accent-400 hover:underline"
		>
			← {m.record_leaderboards()}
		</a>
		<h1 class="mt-2 page-title">
			{recordLabel(bucket)}
		</h1>
		<p class="mt-1 text-sm text-quiet">{recordSport(bucket.activity_type)}</p>
		{#if data.excludeDownhills && bucket.activity_type === 'ride' && ['speed', 'pace'].includes(bucket.metric)}<p
				class="text-xs text-quiet"
			>
				{m.records_downhill_help()}
			</p>{/if}
	</header>
	{#if records.length === 0}
		<p class="surface p-4 text-sm text-quiet">
			{m.record_no_results()}
		</p>
	{:else}
		<div class="table-scroll rounded-lg border border-zinc-800">
			<table class="data-table w-full text-left text-sm">
				<thead class="bg-panel text-xs text-quiet">
					<tr>
						<th scope="col" class="px-3 py-2">{m.record_rank()}</th>
						<th scope="col" class="px-3 py-2">{m.record_rider()}</th>
						<th scope="col" class="px-3 py-2 text-right">{m.record_personal_best()}</th>
						<th scope="col" class="px-3 py-2">{m.record_date_activity()}</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each records as row (row.result?.username)}
						{#if row.result}
							<tr>
								<td class="numeric px-3 py-3">{row.result.rank}</td>
								<td class="name-cell px-3 py-3"
									><a
										class="table-name text-accent-400 hover:underline"
										title={row.result.display_name}
										href={`/u/${encodeURIComponent(row.result.username)}`}
										>{row.result.display_name}</a
									></td
								>
								<td class="px-3 py-3 text-right font-semibold"
									><MetricReadout value={recordValue(row, row.result.achieved_value)} /></td
								>
								<td class="px-3 py-3"
									><a
										class="text-accent-400 hover:underline"
										href={`/activities/${row.result.activity_id}`}
										>{formatDateOnly(row.result.timestamp)}</a
									></td
								>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>
