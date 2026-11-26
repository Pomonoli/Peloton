<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { MediaQuery, SvelteURLSearchParams } from 'svelte/reactivity';
	import { m } from '#lib/paraglide/messages.js';
	import { intlLocale } from '#lib/format.js';
	import type { MemberView } from '#lib/member.js';
	import FilterSheet from '#lib/components/FilterSheet.svelte';
	import RecordedRouteCard from '#lib/components/RecordedRouteCard.svelte';
	import RecordedRouteDetail from '#lib/components/RecordedRouteDetail.svelte';
	import {
		recordedRoutePath,
		riderName,
		routeData,
		type RecordedRoute,
		type RouteDetail
	} from '#lib/recorded-routes.js';

	const desktop = new MediaQuery('width >= 64rem');
	let search = $state('');
	let rider = $state('');
	let distance = $state('');
	let kind = $state('');
	let sort = $state('rides');
	let unridden = $state(false);
	let members = $state<MemberView[]>([]);
	let membersFailed = $state(false);
	let routes = $state<RecordedRoute[]>([]);
	let total = $state(0);
	let loaded = $state(false);
	let busy = $state(true);
	let failed = $state(false);
	let selected = $state('');
	let detail = $state<RouteDetail>();
	let detailBusy = $state(false);
	let detailFailed = $state(false);
	let retry = $state(0);
	let detailRetry = $state(0);
	let offset = $state(0);
	let pageRequest: AbortController | undefined;
	let dialog = $state<HTMLDialogElement>();
	const active = $derived(!!(search.trim() || rider || distance || kind || unridden));
	const filters = $derived.by(() => {
		const params = new SvelteURLSearchParams({
			q: search.trim(),
			rider,
			type: kind,
			sort: sort === 'rider_rides' && !rider ? 'rides' : sort,
			unridden: String(unridden)
		});
		if (distance) {
			const [min, max] = distance.split('-');
			if (min) params.set('min_km', min);
			if (max) params.set('max_km', max);
		}
		return params.toString();
	});
	const count = $derived(new Intl.NumberFormat(intlLocale()).format(total));
	const control =
		'min-h-10 w-full min-w-0 surface px-2.5 text-sm text-zinc-100 focus:border-accent-500 focus:outline-none';

	let memberRequest: AbortController | undefined;
	async function loadMembers() {
		memberRequest?.abort();
		const controller = new AbortController();
		memberRequest = controller;
		membersFailed = false;
		try {
			const results = await routeData<{ members: MemberView[] }>('/api/riders', controller.signal);
			if (!controller.signal.aborted) members = results.members;
		} catch {
			if (!controller.signal.aborted) membersFailed = true;
		}
	}

	onMount(() => {
		void loadMembers();
		return () => memberRequest?.abort();
	});

	async function loadPage(query: string, append = false) {
		pageRequest?.abort();
		const controller = new AbortController();
		pageRequest = controller;
		busy = true;
		failed = false;
		try {
			const result = await routeData<{ routes: RecordedRoute[]; total: number }>(
				'/api/routes?' + query + '&offset=' + (append ? offset : 0),
				controller.signal
			);
			if (controller.signal.aborted) return;
			total = result.total;
			offset = (append ? offset : 0) + result.routes.length;
			if (append) {
				const seen = new Set(routes.map((route) => route.id));
				routes = [...routes, ...result.routes.filter((route) => !seen.has(route.id))];
			} else {
				routes = result.routes;
			}
			loaded = true;
			if (!routes.some((route) => route.id === selected)) {
				selected = desktop.current ? (routes[0]?.id ?? '') : '';
			}
		} catch {
			if (!controller.signal.aborted) failed = true;
		} finally {
			if (!controller.signal.aborted) busy = false;
		}
	}

	$effect(() => {
		void retry;
		const query = filters;
		pageRequest?.abort();
		busy = true;
		failed = false;
		const timer = window.setTimeout(() => void loadPage(query), untrack(() => loaded) ? 170 : 0);
		return () => {
			window.clearTimeout(timer);
			pageRequest?.abort();
		};
	});

	$effect(() => {
		if (desktop.current && !selected && routes.length) selected = routes[0]!.id;
	});

	$effect(() => {
		void detailRetry;
		const id = selected;
		detail = undefined;
		detailFailed = false;
		detailBusy = !!id;
		if (!id) return;
		const controller = new AbortController();
		void routeData<RouteDetail>(recordedRoutePath(id), controller.signal)
			.then((result) => {
				if (!controller.signal.aborted) detail = result;
			})
			.catch(() => {
				if (!controller.signal.aborted) detailFailed = true;
			})
			.finally(() => {
				if (!controller.signal.aborted) detailBusy = false;
			});
		return () => controller.abort();
	});

	$effect(() => {
		if (dialog && selected && !desktop.current && !dialog.open) dialog.showModal();
	});

	function reset() {
		search = rider = distance = kind = '';
		sort = 'rides';
		unridden = false;
	}

	function filterChanged() {
		if (!rider && sort === 'rider_rides') sort = 'rides';
	}
</script>

<svelte:head><title>{m.routes_title()}</title></svelte:head>

<div class="catalog-layout grid min-w-0 gap-4 xl:gap-5">
	<section class="min-w-0">
		<header class="mb-4">
			<h1 class="page-title text-zinc-100">{m.routes_title()}</h1>
			<p class="mt-1 text-sm text-quiet sm:text-[0.94rem]">{m.routes_intro()}</p>
		</header>
		<div class="hidden space-y-2 md:block">
			<div class="flex flex-wrap gap-2">
				<div class="min-w-28 flex-1">{@render riderFilter()}</div>
				<div class="min-w-32 flex-1">{@render sortFilter()}</div>
				<div class="min-w-32 flex-1">{@render kindFilter()}</div>
				<div class="min-w-32 flex-1">{@render distanceFilter()}</div>
				<div class="min-w-36 flex-[1.2]">{@render searchBox()}</div>
			</div>
			{@render unriddenFilter()}
		</div>
		<div class="space-y-2 md:hidden">
			<FilterSheet active={!!distance || unridden} {reset}>
				{#snippet toolbar()}{@render searchBox()}{/snippet}
				<div class="space-y-3">
					{@render distanceFilter()}
					{@render unriddenFilter()}
				</div>
			</FilterSheet>
			<div class="grid min-w-0 grid-cols-3 gap-1.5">
				{@render riderFilter()}
				{@render sortFilter()}
				{@render kindFilter()}
			</div>
		</div>
		{#if membersFailed}
			<button
				type="button"
				class="mt-2 text-xs text-accent-300 hover:underline"
				onclick={loadMembers}
			>
				{m.routes_rider_loading_failed()}
			</button>
		{/if}

		<div class="mt-4" aria-busy={busy}>
			{#if busy && !loaded}
				<p role="status" class="surface p-4 text-sm text-quiet">
					{m.routes_loading()}
				</p>
			{:else if failed}
				<p role="alert" class="surface p-4 text-sm">
					{m.routes_load_failed()}
					<button class="ml-1 text-accent-300 underline" onclick={() => retry++}
						>{m.routes_retry()}</button
					>
				</p>
			{:else}
				<p role="status" class="mb-2.5 text-sm text-quiet">
					{total === 1 ? m.routes_count_one() : m.routes_count({ count })}
				</p>
				{#if !routes.length}
					<div class="surface p-4 text-sm text-quiet">
						{active ? m.routes_empty() : m.routes_empty_catalog()}
						{#if active}
							<button class="ml-1 text-accent-300 underline" onclick={reset}
								>{m.mobile_filters_reset()}</button
							>
						{/if}
					</div>
				{/if}
				<ul class="space-y-2.5">
					{#each routes as route (route.id)}
						<li>
							<RecordedRouteCard
								{route}
								selected={selected === route.id}
								onselect={() => (selected = route.id)}
							/>
						</li>
					{/each}
				</ul>
				{#if offset < total && routes.length > 0}
					<button
						class="mt-3 min-h-11 w-full surface px-3 text-sm font-semibold text-accent-300 hover:border-accent-500 disabled:opacity-50"
						onclick={() => loadPage(filters, true)}
						disabled={busy}>{busy ? m.routes_loading() : m.routes_more_routes()}</button
					>
				{/if}
			{/if}
		</div>
	</section>

	<aside class="min-w-0 space-y-3">
		<div
			class="rounded-xl border border-accent-500/15 bg-accent-500/8 px-3 py-2.5 text-xs leading-relaxed text-zinc-300"
		>
			<p class="flex gap-2">
				<svg
					class="mt-0.5 size-4 shrink-0 text-accent-300"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="1.8"
					aria-hidden="true"><circle cx="12" cy="12" r="9" /><path d="M12 10v6m0-9v1" /></svg
				>
				<span
					><strong class="block text-zinc-100">{m.routes_how_title()}</strong>{m.routes_how()}</span
				>
			</p>
		</div>
		{#if desktop.current && selected}
			<div class="surface p-3.5 xl:p-4" aria-busy={detailBusy}>
				{@render details(true)}
			</div>
		{/if}
	</aside>
</div>

{#if !desktop.current}
	<dialog
		bind:this={dialog}
		class="route-sheet border border-zinc-800 bg-panel text-zinc-100"
		aria-label={m.routes_detail_label()}
		onclose={() => (selected = '')}
	>
		<div
			class="sticky top-0 z-10 flex items-center justify-between border-b border-zinc-800 bg-panel px-3 py-0.5"
		>
			<h2 class="min-w-0 text-lg leading-tight font-semibold tracking-tight">
				{detail?.name ?? m.routes_detail_label()}
			</h2>
			<button
				type="button"
				class="grid size-10 place-items-center rounded-lg text-zinc-300 hover:bg-zinc-800"
				aria-label={m.mobile_close()}
				onclick={() => dialog?.close()}
			>
				<svg
					class="size-5"
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="1.8"
					aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg
				>
			</button>
		</div>
		<div class="p-3 sm:p-4" aria-busy={detailBusy}>{@render details(false)}</div>
	</dialog>
{/if}

{#snippet details(showTitle: boolean)}
	{#if detail}
		{#key detail.id}<RecordedRouteDetail route={detail} {showTitle} />{/key}
	{:else if detailFailed}
		<p role="alert" class="py-2 text-sm">
			{m.routes_detail_failed()}
			<button class="ml-1 text-accent-300 underline" onclick={() => detailRetry++}
				>{m.routes_retry()}</button
			>
		</p>
	{:else}
		<p role="status" class="py-2 text-sm text-quiet">{m.routes_detail_loading()}</p>
	{/if}
{/snippet}

{#snippet searchBox()}
	<label class="relative block min-w-0">
		<span class="sr-only">{m.routes_search()}</span>
		<svg
			class="pointer-events-none absolute top-3 left-2.5 size-4 text-quiet"
			viewBox="0 0 24 24"
			fill="none"
			stroke="currentColor"
			stroke-width="1.8"
			aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5" /><path d="m16 16 4 4" /></svg
		>
		<input
			type="search"
			class={control + ' pl-8'}
			placeholder={m.routes_search()}
			bind:value={search}
			oninput={filterChanged}
		/>
	</label>
{/snippet}

{#snippet riderFilter()}
	<select aria-label={m.routes_rider()} class={control} bind:value={rider} onchange={filterChanged}>
		<option value="">{m.routes_all_riders()}</option>
		{#each members as member (member.id)}
			<option value={member.username}>{riderName(member)}</option>
		{/each}
	</select>
{/snippet}

{#snippet sortFilter()}
	<select aria-label={m.routes_sort()} class={control} bind:value={sort} onchange={filterChanged}>
		<option value="rides">{m.routes_most_ridden()}</option>
		{#if rider}<option value="rider_rides">{m.routes_rider_rides()}</option>{/if}
		<option value="riders">{m.routes_most_riders()}</option>
		<option value="recent">{m.routes_recently_ridden()}</option>
		<option value="distance">{m.routes_distance()}</option>
		<option value="elevation">{m.routes_elevation()}</option>
		<option value="speed">{m.routes_best_speed()}</option>
	</select>
{/snippet}

{#snippet kindFilter()}
	<select
		aria-label={m.routes_route_type()}
		class={control}
		bind:value={kind}
		onchange={filterChanged}
	>
		<option value="">{m.routes_all_types()}</option>
		<option value="loop">{m.routes_loop()}</option>
		<option value="out_and_back">{m.routes_out_and_back()}</option>
		<option value="point_to_point">{m.routes_point_to_point()}</option>
	</select>
{/snippet}

{#snippet distanceFilter()}
	<select
		aria-label={m.routes_distance()}
		class={control}
		bind:value={distance}
		onchange={filterChanged}
	>
		<option value="">{m.routes_all_distances()}</option>
		<option value="-40">{m.routes_distance_under()}</option>
		<option value="40-80">{m.routes_distance_mid()}</option>
		<option value="80-120">{m.routes_distance_long()}</option>
		<option value="120-">{m.routes_distance_far()}</option>
	</select>
{/snippet}

{#snippet unriddenFilter()}
	<label class="inline-flex min-h-8 items-center gap-2 text-sm text-zinc-300">
		<input
			type="checkbox"
			bind:checked={unridden}
			onchange={filterChanged}
			class="accent-accent-500"
		/>
		{m.routes_unridden()}
	</label>
{/snippet}

<style>
	@media (min-width: 64rem) {
		.catalog-layout {
			grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr);
		}
	}
	.route-sheet {
		position: fixed;
		inset: auto 0 var(--mobile-tabbar-size, 0px);
		margin: 0 auto;
		width: min(100%, 43rem);
		max-width: none;
		max-height: min(80dvh, calc(100dvh - var(--mobile-tabbar-size, 0px) - 2rem));
		overflow: auto;
		overscroll-behavior: contain;
		border-radius: 1.25rem 1.25rem 0 0;
	}
	.route-sheet::backdrop {
		background: rgb(0 0 0 / 0.22);
	}
</style>
