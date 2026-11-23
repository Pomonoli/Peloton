<script lang="ts">
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
	let loading = $state(true);
	let failed = $state(false);
	let controller: AbortController | undefined;
	let brokenAvatars = $state<string[]>([]);
	const normalized = $derived(query.trim().toLocaleLowerCase(intlLocale()).replace(/^@/, ''));
	const filtered = $derived(
		members.filter((member) =>
			[member.display_name, member.username].some((value) =>
				value.toLocaleLowerCase(intlLocale()).includes(normalized)
			)
		)
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
			if (!request.signal.aborted) members = next.members;
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

<section class="space-y-5">
	<header class="flex flex-wrap items-end justify-between gap-3">
		<h1 class="text-2xl font-semibold tracking-tight">{m.friends_title()}</h1>
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
		<p class="rounded-lg border border-zinc-800 bg-panel p-4 text-sm text-quiet">
			{m.friends_empty()}
		</p>
	{:else}
		<ul class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
			{#each filtered as member (member.id)}
				{@const profile = `/u/${encodeURIComponent(member.username)}`}
				{@const name = member.display_name.trim() || member.username}
				<li
					class="flex min-w-0 flex-col gap-4 rounded-xl border border-zinc-800 bg-panel p-4 shadow-sm"
				>
					<a href={profile} class="group flex items-center gap-3">
						<span
							class="flex size-11 shrink-0 items-center justify-center overflow-hidden rounded-full bg-zinc-800 font-medium text-zinc-300"
							aria-hidden="true"
						>
							{#if member.avatar_url && !brokenAvatars.includes(member.id)}
								<img
									src={member.avatar_url}
									alt=""
									loading="lazy"
									referrerpolicy="no-referrer"
									onerror={() => (brokenAvatars = [...brokenAvatars, member.id])}
									class="size-full object-cover"
								/>
							{:else}
								{name.slice(0, 1).toLocaleUpperCase(intlLocale())}
							{/if}
						</span>
						<span class="min-w-0">
							<span class="block truncate font-medium group-hover:underline">{name}</span>
							<span class="block truncate text-sm text-quiet">@{member.username}</span>
						</span>
					</a>
					<details class="border-t border-zinc-800 pt-2">
						<summary class="cursor-pointer text-sm text-accent-400"
							>{m.member_cycling_data()}</summary
						>
						<MemberNav member={page.data.user?.id === member.id ? null : member} />
					</details>
					<dl class="grid grid-cols-2 gap-3 text-sm">
						<div>
							<dt class="text-quiet">{m.friends_cycling_distance()}</dt>
							<dd class="font-medium tabular-nums">{formatDistance(member.cycling_distance_m)}</dd>
						</div>
						<div>
							<dt class="text-quiet">{m.friends_total_activities()}</dt>
							<dd class="font-medium tabular-nums">
								{new Intl.NumberFormat(intlLocale()).format(member.activity_count)}
							</dd>
						</div>
						<div class="col-span-2">
							<dt class="text-quiet">{m.friends_recent_ride()}</dt>
							<dd class="mt-0.5">
								{#if member.latest_ride}
									<a
										href={`/activities/${member.latest_ride.id}`}
										class="break-words text-accent-300 hover:underline"
									>
										{member.latest_ride.title || m.ride_activity()} · {formatDateOnly(
											member.latest_ride.start_time
										)}
									</a>
								{:else}
									<span class="text-quiet">{m.friends_no_rides()}</span>
								{/if}
							</dd>
						</div>
					</dl>
					<a
						href={`/activities?member=${encodeURIComponent(member.username)}`}
						class="mt-auto text-sm text-accent-300 hover:underline"
						>{m.community_activity_history()}</a
					>
				</li>
			{/each}
		</ul>
	{/if}
</section>
