<script lang="ts">
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
		extraSort,
		first,
		opponent,
		searchLabel,
		empty
	}: {
		rows: ComparisonRow[];
		columns: Column[];
		extraSort: { key: SortKey; label: string }[];
		first: string;
		opponent: string;
		searchLabel: string;
		empty: string;
	} = $props();

	let query = $state('');
	let winner = $state<Winner | ''>('');
	let sort = $state<SortKey>('name');
	let descending = $state(false);
	const shown = $derived(filterComparison(rows, query, winner, sort, descending));

	function sortBy(key: SortKey) {
		descending = key === sort ? !descending : key === 'difference';
		sort = key;
	}
</script>

<div class="mb-3 grid gap-2 sm:grid-cols-[minmax(10rem,1fr)_auto_auto]">
	<label class="min-w-0">
		<span class="sr-only">{searchLabel}</span>
		<input
			type="search"
			bind:value={query}
			placeholder={searchLabel}
			class="min-h-10 w-full rounded-lg border border-zinc-700 bg-panel px-3 text-sm"
		/>
	</label>
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
			bind:value={sort}
			class="min-h-10 min-w-0 flex-1 rounded-lg border border-zinc-700 bg-panel px-2 text-sm"
		>
			{#each [...columns, ...extraSort] as col (col.key)}
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

{#if shown.length === 0}
	<p role="status" class="rounded-lg border border-zinc-800 bg-panel p-4 text-sm text-quiet">
		{query.trim() || winner ? m.comparison_no_matches() : empty}
	</p>
{:else}
	<!-- Explicit table roles preserve its semantics when mobile CSS changes display. -->
	<!-- svelte-ignore a11y_no_redundant_roles -->
	<table role="table" class="comparison-table w-full text-left text-sm">
		<!-- svelte-ignore a11y_no_redundant_roles -->
		<thead role="rowgroup">
			<!-- svelte-ignore a11y_no_redundant_roles -->
			<tr role="row">
				{#each columns as col (col.key)}
					<th
						role="columnheader"
						scope="col"
						class="px-3 py-2 text-quiet"
						aria-sort={sort === col.key ? (descending ? 'descending' : 'ascending') : undefined}
					>
						<button
							type="button"
							onclick={() => sortBy(col.key)}
							class="font-medium hover:text-accent-300"
						>
							{col.label}
							{sort === col.key ? (descending ? '↓' : '↑') : ''}
						</button>
					</th>
				{/each}
			</tr>
		</thead>
		<!-- svelte-ignore a11y_no_redundant_roles -->
		<tbody role="rowgroup">
			{#each shown as row (row.id)}
				<!-- svelte-ignore a11y_no_redundant_roles -->
				<tr role="row" class="border border-zinc-800 bg-panel">
					{#each columns as col (col.key)}
						{@const cell = row.cells[col.key]}
						<td
							role="cell"
							class="min-w-0 break-words px-3 py-2.5 align-top tabular-nums"
							class:row-title={col.key === 'name'}
							class:distance-cell={col.key === 'distance'}
						>
							{#if col.key !== 'name'}<span
									class="mobile-label text-xs text-quiet"
									aria-hidden="true">{col.label}</span
								>{/if}
							{#if cell?.href}
								<a href={cell.href} class="text-accent-300 hover:underline">{cell.text}</a>
							{:else}
								<span
									class:font-semibold={col.key === 'name' || (col.key === 'winner' && !!row.winner)}
									>{cell?.text}</span
								>
							{/if}
							{#if cell?.detail}<span class="block text-xs text-quiet">{cell.detail}</span>{/if}
						</td>
					{/each}
				</tr>
			{/each}
		</tbody>
	</table>
{/if}

<style>
	.mobile-label {
		display: none;
	}
	@media (max-width: 900px) {
		.comparison-table,
		tbody {
			display: block;
		}
		thead {
			position: absolute;
			width: 1px;
			height: 1px;
			overflow: hidden;
			clip-path: inset(50%);
		}
		thead button {
			visibility: hidden;
		}
		tbody tr {
			display: grid;
			grid-template-columns: repeat(2, minmax(0, 1fr));
			border-radius: 0.5rem;
			margin-bottom: 0.5rem;
		}
		.distance-cell,
		.row-title {
			grid-column: 1 / -1;
		}
		.row-title {
			border-bottom: 1px solid var(--color-zinc-800);
		}
		.mobile-label {
			display: block;
		}
	}
</style>
