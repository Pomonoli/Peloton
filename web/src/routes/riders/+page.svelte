<script lang="ts">
	import { memberProfile, memberURL } from '#lib/member.js';
	import { page } from '$app/state';
	import MemberNav from '#lib/components/MemberNav.svelte';
	import { onMount } from 'svelte';
	import { formatDateOnly, formatDistance, intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';

	interface Member {
		id: string;
		username: string;
		display_name: string;
		avatar_url: string;
		activity_count: number;
		cycling_distance_m: number;
		latest_ride: { id: string; title: string; start_time: string } | null;
	}

	let members = $state<Member[]>([]);
	let query = $state('');
	let expanded = $state('');
	type Sort = 'name' | 'cycling_distance_m' | 'activity_count';
	let sort = $state<Sort>('name');
	let descending = $state(false);
	const number = $derived(new Intl.NumberFormat(intlLocale()));
	const collator = $derived(new Intl.Collator(intlLocale(), { sensitivity: 'base' }));
	const columns = $derived([
		{ key: 'name', label: m.riders_name() },
		{ key: 'cycling_distance_m', label: m.friends_cycling_distance() },
		{ key: 'activity_count', label: m.friends_total_activities() }
	] satisfies { key: Sort; label: string }[]);

	function sortBy(key: Sort) {
		descending = sort === key ? !descending : key !== 'name';
		sort = key;
	}

	function riderName(member: Member) {
		return member.display_name.trim() || member.username;
	}
	let loading = $state(true);
	let failed = $state(false);
	let controller: AbortController | undefined;
	let brokenAvatars = $state<string[]>([]);
	const normalized = $derived(query.trim().toLocaleLowerCase(intlLocale()).replace(/^@/, ''));
	const filtered = $derived(
		members
			.filter((member) =>
				[member.display_name, member.username].some((value) =>
					value.toLocaleLowerCase(intlLocale()).includes(normalized)
				)
			)
			.sort((a, b) => {
				const order =
					sort === 'name' ? collator.compare(riderName(a), riderName(b)) : a[sort] - b[sort];
				return (descending ? -order : order) || collator.compare(riderName(a), riderName(b));
			})
	);

	async function load() {
		controller?.abort();
		const request = new AbortController();
		controller = request;
		failed = false;
		loading = true;
		try {
			const response = await fetch('/api/riders', { signal: request.signal });
			if (!response.ok) throw new Error();
			const next = (await response.json()) as { members: Member[] };
			if (!request.signal.aborted)
				members = next.members.filter((member) => member.username.trim().toLowerCase() !== 'admin');
		} catch {
			if (!request.signal.aborted) failed = true;
		} finally {
			if (!request.signal.aborted) loading = false;
		}
	}

	onMount(() => {
		void load();
		return () => controller?.abort();
	});
</script>

<svelte:head><title>{m.friends_title()}</title></svelte:head>

<section class="space-y-3">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="page-title">{m.friends_title()}</h1>
			{#if !loading && !failed}<p class="text-xs text-quiet">
					{m.riders_count({ count: filtered.length })}
				</p>{/if}
		</div>
		<label class="w-full sm:w-64">
			<span class="sr-only">{m.friends_search()}</span>
			<input
				type="search"
				placeholder={m.friends_search()}
				bind:value={query}
				class="min-h-11 w-full rounded-lg border border-zinc-700 bg-panel px-3 text-sm"
			/>
		</label>
	</header>

	{#if loading}
		<p role="status" class="text-sm text-quiet">{m.ride_loading()}</p>
	{:else if failed}
		<p role="status" class="text-sm text-quiet">{m.friends_failed()}</p>
		<button type="button" onclick={load} class="min-h-10 text-sm text-accent-300 hover:underline"
			>{m.community_retry()}</button
		>
	{:else if filtered.length === 0}
		<p class="surface p-4 text-sm text-quiet">
			{m.friends_empty()}
		</p>
	{:else}
		<div class="overflow-hidden surface">
			<table class="w-full table-fixed text-sm">
				<thead class="border-b border-zinc-800 bg-zinc-950/75 text-xs text-quiet">
					<tr>
						{#each columns as column (column.key)}
							<th
								scope="col"
								aria-sort={sort === column.key
									? descending
										? 'descending'
										: 'ascending'
									: undefined}
								class={column.key === 'name'
									? 'w-[44%] px-2 text-left md:w-[36%] md:px-4'
									: 'px-1 text-right md:w-[14%] md:px-3'}
							>
								<button
									type="button"
									onclick={() => sortBy(column.key)}
									class="min-h-11 font-semibold hover:text-accent-300"
								>
									{column.label}{sort === column.key ? (descending ? ' ↓' : ' ↑') : ''}
								</button>
							</th>
						{/each}
						<th scope="col" class="hidden px-3 text-left font-semibold md:table-cell"
							>{m.friends_recent_ride()}</th
						>
						<th scope="col" class="w-10 md:w-12"
							><span class="sr-only">{m.member_cycling_data()}</span></th
						>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each filtered as member (member.id)}
						{@const name = riderName(member)}
						{@const open = expanded === member.id}
						<tr class="hover:bg-zinc-950/75">
							<th scope="row" class="px-2 py-1 text-left font-normal md:px-4">
								<a href={memberProfile(member)} class="group flex min-h-11 items-center gap-2">
									<span
										class="relative grid size-8 shrink-0 place-items-center overflow-hidden rounded-full bg-accent-500/12 font-semibold text-accent-300 md:size-9"
										aria-hidden="true"
									>
										{name.slice(0, 1).toLocaleUpperCase(intlLocale())}
										{#if member.avatar_url && !brokenAvatars.includes(member.id)}
											<img
												src={member.avatar_url}
												alt=""
												loading="lazy"
												referrerpolicy="no-referrer"
												onerror={() => (brokenAvatars = [...brokenAvatars, member.id])}
												class="absolute inset-0 size-full object-cover"
											/>
										{/if}
									</span>
									<span class="min-w-0">
										<span class="block truncate font-semibold group-hover:text-accent-300"
											>{name}</span
										>
										<span class="block truncate text-xs text-quiet">@{member.username}</span>
									</span>
								</a>
							</th>
							<td class="px-1 py-2 text-right text-xs font-semibold tabular-nums sm:text-sm md:px-3"
								>{formatDistance(member.cycling_distance_m)}</td
							>
							<td class="px-1 py-2 text-right text-xs font-semibold tabular-nums sm:text-sm md:px-3"
								>{number.format(member.activity_count)}</td
							>
							<td class="hidden px-3 py-2 md:table-cell">{@render recentRide(member)}</td>
							<td>
								<button
									type="button"
									class="grid min-h-11 w-full place-items-center rounded-lg text-accent-300"
									aria-label={m.riders_details({ name })}
									aria-expanded={open}
									onclick={() => (expanded = open ? '' : member.id)}
								>
									<svg
										class="size-4"
										class:rotate-180={open}
										aria-hidden="true"
										viewBox="0 0 24 24"
										fill="none"
										stroke="currentColor"
										stroke-width="2"
										stroke-linecap="round"
										stroke-linejoin="round"><path d="m6 9 6 6 6-6" /></svg
									>
								</button>
							</td>
						</tr>
						{#if open}
							<tr class="bg-zinc-950/60">
								<td colspan="5" class="px-3 py-2.5 sm:px-4">
									<div class="mb-1 text-xs md:hidden">
										<span class="mr-2 text-quiet">{m.friends_recent_ride()}</span
										>{@render recentRide(member)}
									</div>
									<MemberNav member={page.data.user?.id === member.id ? null : member} />
									<a
										href={memberURL('/activities', member)}
										class="inline-flex min-h-10 items-center text-sm font-semibold text-accent-300 hover:underline"
										>{m.community_activity_history()}</a
									>
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>

{#snippet recentRide(member: Member)}
	{#if member.latest_ride}
		<a
			href={`/activities/${member.latest_ride.id}`}
			class="break-words text-xs text-accent-300 hover:underline"
		>
			{member.latest_ride.title || m.ride_activity()} · {formatDateOnly(
				member.latest_ride.start_time
			)}
		</a>
	{:else}
		<span class="text-xs text-quiet">{m.friends_no_rides()}</span>
	{/if}
{/snippet}
