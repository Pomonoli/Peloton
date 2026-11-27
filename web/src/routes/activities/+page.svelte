<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import FilterSheet from '#lib/components/FilterSheet.svelte';
	import ActivityList from '#lib/components/ActivityList.svelte';
	import type { FeedActivity } from '#lib/activity-feed.js';
	import { memberURL } from '#lib/member.js';
	import { onDestroy } from 'svelte';
	import { refreshAll } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages.js';
	import ActivityFilterBar from '#lib/components/ActivityFilterBar.svelte';
	import { ActivityFilter, type ActivityFacets } from '#lib/activity-filter.svelte.js';

	let { data } = $props();

	type Feed = {
		total: number;
		matched: number;
		facets: ActivityFacets;
		activities: FeedActivity[];
		has_more: boolean;
	};
	const PAGE_SIZE = 50;

	// The standard filter (shared with /heatmap), seeded from ?from=&to= etc.
	const filter = new ActivityFilter(page.url.searchParams);

	// Debounce free-text numeric/date input so we don't re-query per keystroke.
	// The effect also runs once on mount — skip that, or the initial load
	// double-fetches (once immediately, once when the timer fires).
	let debounced = $state(0);
	let debounceTimer: ReturnType<typeof setTimeout> | undefined;
	let debounceInit = false;
	$effect(() => {
		void filter.textKey;
		if (!debounceInit) {
			debounceInit = true;
			return;
		}
		clearTimeout(debounceTimer);
		debounceTimer = setTimeout(() => (debounced += 1), 350);
	});

	let feed = $state<Feed | null>(null);
	let items = $state<FeedActivity[]>([]);
	let pageNum = $state(1);
	let loading = $state(false);

	const totalPages = $derived(feed ? Math.max(1, Math.ceil(feed.matched / PAGE_SIZE)) : 1);

	// Windowed pager: first, last, current ±1, with ellipsis gaps.
	const pageList = $derived.by(() => {
		const wanted = [1, pageNum - 1, pageNum, pageNum + 1, totalPages];
		const pages = [...new Set(wanted)]
			.filter((p) => p >= 1 && p <= totalPages)
			.sort((a, b) => a - b);
		const out: (number | '…')[] = [];
		let prev = 0;
		for (const p of pages) {
			if (p - prev > 1) out.push('…');
			out.push(p);
			prev = p;
		}
		return out;
	});

	// Filter changes can fire faster than responses arrive; abort the in-flight
	// request and drop stale responses so the list never flashes old results.
	let feedSeq = 0;
	let feedAbort: AbortController | null = null;

	// Takes the target page explicitly (not pageNum) so the filter $effect
	// doesn't track pageNum and re-fire on page navigation.
	async function loadFeed(p: number) {
		const seq = ++feedSeq;
		feedAbort?.abort();
		const ctrl = new AbortController();
		feedAbort = ctrl;
		loading = true;
		try {
			const q = filter.params();
			q.set('limit', String(PAGE_SIZE));
			q.set('offset', String((p - 1) * PAGE_SIZE));
			const res = await fetch(memberURL(`/api/activities/feed?${q}`, data.member), {
				signal: ctrl.signal
			});
			if (res.ok && seq === feedSeq) {
				const data: Feed = await res.json();
				items = data.activities;
				feed = data;
			}
		} catch {
			/* ignore (includes aborts) */
		} finally {
			if (seq === feedSeq) loading = false;
		}
	}

	onDestroy(() => {
		clearTimeout(debounceTimer);
		feedAbort?.abort();
	});

	function goToPage(p: number) {
		if (p < 1 || p > totalPages || p === pageNum) return;
		pageNum = p;
		loadFeed(p);
		window.scrollTo({ top: 0, behavior: 'smooth' });
	}

	// Re-query (reset to page 1) whenever a filter or sort changes. Text-input
	// filters (dates, ranges) arrive via the debounced counter.
	$effect(() => {
		void filter.immediateKey;
		void debounced;
		pageNum = 1;
		loadFeed(1);
	});

	// ---- upload ----
	let dragOver = $state(false);
	let uploading = $state(false);
	let uploadError = $state<string | null>(null);
	let uploadOk = $state<string | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);

	async function uploadFiles(files: FileList | null) {
		if (!files || files.length === 0) return;
		uploading = true;
		uploadError = null;
		uploadOk = null;
		let imported = 0;
		try {
			for (const file of Array.from(files)) {
				const fd = new FormData();
				fd.append('file', file);
				const res = await fetch('/api/activities/upload', { method: 'POST', body: fd });
				if (!res.ok) {
					uploadError = `${file.name}: ${(await res.text()).trim() || res.statusText}`;
					break;
				}
				imported++;
			}
			if (imported > 0) {
				uploadOk = `Imported ${imported} file${imported > 1 ? 's' : ''}.`;
				pageNum = 1;
				await loadFeed(1);
				await refreshAll();
			}
		} catch (e) {
			uploadError = (e as Error).message;
		} finally {
			uploading = false;
		}
	}

	function onDrop(e: DragEvent) {
		e.preventDefault();
		dragOver = false;
		uploadFiles(e.dataTransfer?.files ?? null);
	}
</script>

<section class="space-y-4 max-md:space-y-3">
	<header class="flex items-center justify-between gap-4 max-md:flex-wrap">
		<div>
			<h1 class="page-title">
				{m.activities_title()}
			</h1>
			{#if feed}
				<p class="mt-1 text-sm text-zinc-400">
					{#if filter.active}
						{m.activity_list_filtered({ matched: feed.matched, total: feed.total })}
					{:else}
						{feed.total === 1
							? m.overview_one_activity()
							: m.overview_activity_count({ count: feed.total })}
					{/if}
				</p>
			{/if}
		</div>
		{#if !data.member}
			<a
				href="/review"
				class="inline-flex min-h-11 items-center gap-1 text-xs font-semibold text-accent-300 hover:underline"
			>
				{m.activity_review_queue()}
				<Icon name="chevron" size={14} />
			</a>
		{/if}
	</header>
	<FilterSheet
		active={filter.active || filter.sort !== 'date_desc'}
		reset={() => {
			filter.clear();
			filter.sort = 'date_desc';
		}}
	>
		{#snippet toolbar()}
			{#if !data.member}
				<div class="flex flex-wrap items-center gap-2">
					<a href="/activities/new" class="action">
						{m.activity_new()}
					</a>
					<button
						type="button"
						disabled={uploading}
						onclick={() => fileInput?.click()}
						class="action action-primary"
					>
						<Icon name="upload" size={16} />{uploading
							? m.activity_uploading()
							: m.activity_upload_files()}
					</button>
				</div>
			{/if}
		{/snippet}
		<ActivityFilterBar {filter} facets={feed?.facets ?? null} member={data.member?.username} />
	</FilterSheet>

	{#if !data.member}
		<div
			role="button"
			tabindex="0"
			ondragover={(e) => {
				e.preventDefault();
				dragOver = true;
			}}
			ondragleave={() => (dragOver = false)}
			ondrop={onDrop}
			onclick={() => fileInput?.click()}
			onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && fileInput?.click()}
			class="cursor-pointer rounded-lg max-md:hidden border-2 border-dashed px-5 py-6 text-center text-sm transition-colors {dragOver
				? 'border-accent-500 bg-accent-500/10 text-accent-200'
				: 'border-zinc-700 bg-zinc-900/30 text-zinc-400 hover:border-zinc-600'}"
		>
			Drag &amp; drop <code>.gpx</code>, <code>.tcx</code> or <code>.fit</code> files here (multiple
			supported), or click to browse.
			<input
				bind:this={fileInput}
				type="file"
				accept=".gpx,.tcx,.fit"
				multiple
				class="hidden"
				onchange={(e) => uploadFiles(e.currentTarget.files)}
			/>
		</div>

		{#if uploadError}
			<div class="rounded border border-red-700/50 bg-red-950/30 px-3 py-2 text-xs text-red-300">
				{uploadError}
			</div>
		{/if}
		{#if uploadOk}
			<div
				class="rounded border border-emerald-700/50 bg-emerald-950/30 px-3 py-2 text-xs text-emerald-300"
			>
				{uploadOk}
			</div>
		{/if}
	{/if}

	{#if items.length === 0 && !loading}
		<div class="surface p-8 text-center text-sm text-zinc-400 max-md:p-4">
			{filter.active ? 'No activities match this filter.' : m.activities_empty()}
		</div>
	{:else}
		<ActivityList {items} {loading} member={data.member} />

		{#if feed && totalPages > 1}
			<nav class="mt-3 flex flex-wrap items-center justify-center gap-1" aria-label="Pagination">
				<button
					type="button"
					onclick={() => goToPage(pageNum - 1)}
					disabled={pageNum === 1}
					class="rounded px-3 py-1.5 text-sm text-zinc-300 hover:text-accent-300 disabled:opacity-40 disabled:hover:text-zinc-300"
				>
					‹ Prev
				</button>
				{#each pageList as p, i (i)}
					{#if p === '…'}
						<span class="px-2 text-sm text-quiet">…</span>
					{:else}
						<button
							type="button"
							onclick={() => goToPage(p)}
							aria-current={p === pageNum ? 'page' : undefined}
							class="min-h-10 min-w-9 rounded-lg px-2 py-1.5 text-sm tabular-nums {p === pageNum
								? 'bg-accent-500 font-medium text-on-accent'
								: 'text-zinc-300 hover:bg-zinc-800'}"
						>
							{p}
						</button>
					{/if}
				{/each}
				<button
					type="button"
					onclick={() => goToPage(pageNum + 1)}
					disabled={pageNum === totalPages}
					class="rounded px-3 py-1.5 text-sm text-zinc-300 hover:text-accent-300 disabled:opacity-40 disabled:hover:text-zinc-300"
				>
					Next ›
				</button>
			</nav>
			<p class="mt-2 text-center text-xs text-zinc-500">
				{(pageNum - 1) * PAGE_SIZE + 1}–{Math.min(pageNum * PAGE_SIZE, feed.matched)} of {feed.matched}
			</p>
		{/if}
	{/if}
</section>
