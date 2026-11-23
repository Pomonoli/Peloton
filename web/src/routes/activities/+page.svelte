<script lang="ts">
	import FilterSheet from '#lib/components/FilterSheet.svelte';
	import { memberURL } from '#lib/member.js';
	import { onDestroy } from 'svelte';
	import { refreshAll } from '$app/navigation';
	import { page } from '$app/state';
	import { m } from '#lib/paraglide/messages.js';
	import SportIcon from '#lib/components/SportIcon.svelte';
	import ActivityFilterBar from '#lib/components/ActivityFilterBar.svelte';
	import { ActivityFilter, humanize, type ActivityFacets } from '#lib/activity-filter.svelte.js';
	import {
		formatDistance,
		formatDuration,
		formatElevation,
		formatRelativeDate,
		formatSpeed
	} from '#lib/format.js';

	let { data } = $props();

	type FeedActivity = {
		id: string;
		title: string;
		type: string;
		discipline: string;
		start_time: string;
		timezone: string;
		moving_duration_s: number;
		elapsed_duration_s: number;
		avg_speed_mps: number | null;
		distance_m: number | null;
		elevation_gain_m: number | null;
		tss: number | null;
		start_place: string;
		gear_unset: boolean;
	};
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
	<FilterSheet
		active={filter.active || filter.sort !== 'date_desc'}
		reset={() => {
			filter.clear();
			filter.sort = 'date_desc';
		}}
	>
		{#snippet toolbar()}
			<header class="flex items-center justify-between gap-4 max-md:flex-wrap">
				<div>
					<h1 class="text-3xl font-semibold tracking-tight max-md:text-2xl">
						{m.activities_title()}
					</h1>
					{#if feed}
						<p class="mt-1 text-sm text-zinc-400">
							{#if filter.active}
								<span class="font-medium text-zinc-200">{feed.matched}</span> of {feed.total} activities
							{:else}
								<span class="font-medium text-zinc-200">{feed.total}</span> activities
							{/if}
						</p>
					{/if}
				</div>
			</header>
		{/snippet}
		<ActivityFilterBar {filter} facets={feed?.facets ?? null} member={data.member?.username} />
	</FilterSheet>

	{#if !data.member}
		<div class="flex flex-wrap items-center gap-2">
			<a
				href="/activities/new"
				class="inline-flex min-h-10 items-center rounded-lg border border-zinc-800 bg-panel px-3 text-xs text-zinc-300 hover:border-accent-500 hover:text-accent-300"
			>
				New activity
			</a>
			<button
				type="button"
				disabled={uploading}
				onclick={() => fileInput?.click()}
				class="min-h-10 rounded-lg bg-accent-500 px-3 text-xs font-semibold text-on-accent hover:bg-accent-600 disabled:opacity-50"
			>
				{uploading ? 'Uploading…' : 'Upload files'}
			</button>
			<a
				href="/review"
				class="inline-flex min-h-10 items-center rounded-lg px-2 text-xs font-semibold text-accent-300 hover:underline"
				>Review queue</a
			>
		</div>
	{/if}
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
		<div
			class="rounded-lg border border-zinc-800 bg-panel p-8 text-center text-sm text-zinc-400 max-md:p-4"
		>
			{filter.active ? 'No activities match this filter.' : m.activities_empty()}
		</div>
	{:else}
		<ul
			class="space-y-3 transition-opacity duration-200 max-md:divide-y max-md:divide-zinc-800 max-md:space-y-0 max-md:rounded-2xl max-md:border max-md:border-zinc-800 max-md:bg-panel"
			class:opacity-50={loading}
		>
			{#each items as activity (activity.id)}
				<li>
					<a
						href={`/activities/${activity.id}`}
						class="flex flex-wrap items-center gap-5 rounded-lg border border-zinc-800 bg-panel px-5 py-4 max-md:gap-x-2.5 max-md:gap-y-1.5 max-md:rounded-none max-md:border-0 max-md:px-2.5 max-md:py-2.5 transition-colors hover:border-zinc-700 hover:bg-zinc-900/80"
					>
						<div
							class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-zinc-800 text-zinc-300 max-md:size-9 max-md:rounded-xl max-md:bg-accent-500/10 max-md:text-accent-300"
							title={activity.discipline || activity.type}
						>
							<SportIcon type={activity.discipline || activity.type} size={24} />
						</div>
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<span class="truncate font-medium">
									{activity.title || `${humanize(activity.discipline || activity.type)} Activity`}
								</span>
								{#if activity.gear_unset}
									<span
										class="shrink-0 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-amber-300"
										title={m.gear_prompt_list_hint()}>{m.gear_prompt_badge()}</span
									>
								{/if}
							</div>
							<div class="mt-0.5 text-xs text-zinc-500">
								{formatRelativeDate(activity.start_time)}{activity.start_place
									? ` · ${humanize(activity.discipline || activity.type)} from ${activity.start_place}`
									: ` · ${activity.timezone}`}
							</div>
						</div>
						<img
							src={`/api/activities/${activity.id}/map.png`}
							alt=""
							loading="lazy"
							class="hidden h-8 w-24 shrink-0 rounded border border-zinc-800 object-cover lg:block"
							onerror={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
						/>
						<dl
							class="grid grid-cols-3 gap-x-4 gap-y-2 text-xs text-zinc-400 max-md:ml-[2.875rem] max-md:w-full max-md:gap-x-1.5 md:grid-cols-5 md:text-right"
						>
							<div>
								<dt class="text-zinc-500">{m.col_distance()}</dt>
								<dd class="mt-0.5 text-sm tabular-nums text-zinc-100 max-md:text-xs">
									{formatDistance(activity.distance_m)}
								</dd>
							</div>
							<div>
								<dt class="text-zinc-500">{m.col_moving()}</dt>
								<dd class="mt-0.5 text-sm tabular-nums text-zinc-100 max-md:text-xs">
									{formatDuration(activity.moving_duration_s)}
								</dd>
							</div>
							<div>
								<dt class="text-zinc-500">{m.ride_elapsed()}</dt>
								<dd class="mt-0.5 text-sm tabular-nums text-zinc-100 max-md:text-xs">
									{formatDuration(activity.elapsed_duration_s)}
								</dd>
							</div>
							<div>
								<dt class="text-zinc-500">{m.col_elevation()}</dt>
								<dd class="mt-0.5 text-sm tabular-nums text-zinc-100 max-md:text-xs">
									{formatElevation(activity.elevation_gain_m)}
								</dd>
							</div>
							<div>
								<dt class="text-zinc-500">{m.col_average_speed()}</dt>
								<dd class="font-medium tabular-nums">
									{formatSpeed(activity.avg_speed_mps)}
								</dd>
							</div>
						</dl>
					</a>
				</li>
			{/each}
		</ul>

		{#if feed && totalPages > 1}
			<nav class="mt-6 flex items-center justify-center gap-1" aria-label="Pagination">
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
							class="min-w-9 rounded px-2.5 py-1.5 text-sm tabular-nums {p === pageNum
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
