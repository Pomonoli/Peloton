<script lang="ts">
	import FilterSheet from './FilterSheet.svelte';
	import { m } from '#lib/paraglide/messages.js';
	import {
		filterComparison,
		type Column,
		type ComparisonRow,
		type SortKey,
		type Winner
	} from '#lib/head-to-head.js';

	let {
		rows,
		columns,
		defaultSort = 'name',
		descendingResults = false,
		first,
		opponent,
		searchLabel,
		empty
	}: {
		rows: ComparisonRow[];
		columns: Column[];
		defaultSort?: SortKey;
		descendingResults?: boolean;
		first: string;
		opponent: string;
		searchLabel: string;
		empty: string;
	} = $props();

	let query = $state('');
	let winner = $state<Winner | ''>('');
	let selectedSort = $state<SortKey>();
	const sort = $derived(selectedSort ?? defaultSort);
	let descending = $state(false);
	const shown = $derived(filterComparison(rows, query, winner, sort, descending));

	function sortBy(key: SortKey) {
		descending =
			key === sort ? !descending : descendingResults && (key === 'you' || key === 'opponent');
		selectedSort = key;
	}
</script>

<div class="mb-2">
	<FilterSheet
		active={!!winner || sort !== defaultSort || descending}
		reset={() => {
			winner = '';
			selectedSort = undefined;
			descending = false;
		}}
	>
		{#snippet toolbar()}
			<label class="min-w-0">
				<span class="sr-only">{searchLabel}</span>
				<input
					type="search"
					bind:value={query}
					placeholder={searchLabel}
					class="min-h-10 w-full rounded-lg border border-zinc-700 bg-panel px-3 text-sm"
				/>
			</label>
		{/snippet}
		<div class="grid gap-2 sm:grid-cols-2">
			<select
				aria-label={m.comparison_filter()}
				bind:value={winner}
				class="min-h-10 w-full rounded-lg border border-zinc-700 bg-panel px-2 text-sm"
			>
				<option value="">{m.comparison_all_results()}</option>
				<option value="you">{first}</option>
				<option value="opponent">{opponent}</option>
				<option value="tie">{m.comparison_ties()}</option>
			</select>
			<div class="flex min-w-0 gap-1">
				<select
					aria-label={m.comparison_sort_by()}
					value={sort}
					onchange={(event) => sortBy(event.currentTarget.value as SortKey)}
					class="min-h-10 min-w-0 flex-1 rounded-lg border border-zinc-700 bg-panel px-2 text-sm"
				>
					{#if defaultSort === 'category'}
						<option value="category">{m.comparison_record_order()}</option>
					{/if}
					{#each columns as col (col.key)}
						<option value={col.key}>{m.comparison_sort_option({ column: col.label })}</option>
					{/each}
				</select>
				<button
					type="button"
					onclick={() => (descending = !descending)}
					class="min-h-10 min-w-10 rounded-lg border border-zinc-700 bg-panel hover:border-accent-500"
					aria-label={descending ? m.segment_sort_descending() : m.segment_sort_ascending()}
				>
					{descending ? '↓' : '↑'}
				</button>
			</div>
		</div>
	</FilterSheet>
</div>

{#if shown.length === 0}
	<p role="status" class="rounded-lg border border-zinc-800 bg-panel p-4 text-sm text-quiet">
		{query.trim() || winner ? m.comparison_no_matches() : empty}
	</p>
{:else}
	<div class="overflow-hidden rounded-xl border border-zinc-800 bg-panel">
		<table class="comparison-table w-full text-left text-sm">
			<thead class="bg-zinc-950/70">
				<tr>
					{#each columns as col (col.key)}
						<th
							scope="col"
							class="px-3 py-1 text-quiet"
							class:distance-cell={col.key === 'distance'}
							aria-sort={sort === col.key ? (descending ? 'descending' : 'ascending') : undefined}
						>
							<button
								type="button"
								title={col.label}
								aria-label={col.label}
								onclick={() => sortBy(col.key)}
								class="min-h-10 text-left font-semibold hover:text-accent-300"
							>
								{col.label}
								{sort === col.key ? (descending ? ' ↓' : ' ↑') : ''}
							</button>
						</th>
					{/each}
				</tr>
			</thead>
			<tbody class="divide-y divide-zinc-800">
				{#each shown as row (row.id)}
					<tr>
						{#each columns as col (col.key)}
							{@const cell = row.cells[col.key]}
							{@const won =
								(row.winner === 'you' || row.winner === 'opponent') && col.key === row.winner}
							<td
								class="min-w-0 px-3 py-2.5 align-middle tabular-nums"
								class:distance-cell={col.key === 'distance'}
							>
								{#if col.key === 'winner'}
									<span
										title={cell?.text}
										class="winner-label inline-block rounded-md px-1.5 py-0.5 text-xs font-semibold"
										class:first-win={row.winner === 'you'}
										class:second-win={row.winner === 'opponent'}>{cell?.text}</span
									>
								{:else if cell?.href}
									<a
										href={cell.href}
										class="text-accent-300 hover:underline"
										class:font-bold={won || col.key === 'name' || descendingResults}>{cell.text}</a
									>
								{:else}
									<span class:font-semibold={col.key === 'name'}>{cell?.text}</span>
								{/if}
								{#if col.key === 'name' && row.cells.distance}
									<span class="inline-distance text-xs text-quiet">{row.cells.distance.text}</span>
								{/if}
								{#if cell?.detail}<span class="cell-detail block text-xs text-quiet"
										>{cell.detail}</span
									>{/if}
							</td>
						{/each}
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}

<style>
	.inline-distance {
		display: none;
	}
	td {
		overflow-wrap: anywhere;
	}
	.winner-label {
		background: var(--surface-muted);
		color: var(--text-secondary);
	}
	.first-win {
		background: var(--success-surface);
		color: var(--success-text);
	}
	.second-win {
		background: var(--warning-surface);
		color: var(--warning-text);
	}

	@media (width < 64rem) {
		.comparison-table {
			table-layout: fixed;
		}
		.comparison-table :is(th, td) {
			padding-inline: 0.3125rem;
			font-size: 0.75rem;
			line-height: 1.125rem;
		}
		.comparison-table th:first-child {
			width: 30%;
		}
		.comparison-table th:last-child {
			width: 16%;
		}
		.comparison-table th button {
			max-width: 100%;
			font-size: 0.6875rem;
			overflow-wrap: anywhere;
		}
		.distance-cell {
			display: none;
		}
		.inline-distance {
			display: block;
		}
		.cell-detail,
		.inline-distance,
		.winner-label {
			font-size: 0.6875rem;
			line-height: 1rem;
		}
		.winner-label {
			max-width: 100%;
			padding-inline: 0.25rem;
		}
	}
</style>
