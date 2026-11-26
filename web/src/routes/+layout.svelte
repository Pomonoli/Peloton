<script lang="ts">
	import '../app.css';
	import { page } from '$app/state';
	import { afterNavigate, goto } from '$app/navigation';
	import { APP_NAME } from '$app/env/public';
	import { m } from '#lib/paraglide/messages.js';
	import { setPrefs } from '#lib/prefs.svelte.js';
	import LoadingBar from '#lib/components/LoadingBar.svelte';
	import { personalDataURL, memberURL, type MemberView } from '#lib/member.js';
	import BrandMark from '#lib/components/BrandMark.svelte';

	let { children, data } = $props();

	const viewedMember = $derived.by<MemberView | null>(() => {
		if (page.data.viewedMember) return page.data.viewedMember;
		if (!page.url.searchParams.has('member')) return null;
		return typeof page.data.member === 'object' ? page.data.member : null;
	});
	const member = $derived(
		['/riders', '/head-to-head'].includes(page.url.pathname)
			? null
			: (viewedMember ?? page.data.activityOwner)
	);

	// Email-verification banner (logged-in users with an unverified email).
	let resendState = $state<'idle' | 'sending' | 'sent' | 'error'>('idle');
	async function resendVerification() {
		resendState = 'sending';
		try {
			const res = await fetch('/auth/email/verify/send', { method: 'POST' });
			resendState = res.ok ? 'sent' : 'error';
		} catch {
			resendState = 'error';
		}
	}

	// Seed the site-wide format preferences from the logged-in user, and keep
	// them in sync whenever the user data changes (e.g. after editing settings).
	$effect(() => {
		if (data.user) {
			setPrefs({
				units: data.user.units,
				dateFormat: data.user.dateFormat
			});
		}
	});

	// Icons are single multi-subpath `d` strings (24×24, stroked).
	const icons = {
		overview: 'M3 11.5 12 4l9 7.5 M5 10v10h5v-6h4v6h5V10',
		activities: 'M8 6h13 M8 12h13 M8 18h13 M3.5 6h.01 M3.5 12h.01 M3.5 18h.01',
		planner: 'M4 4h6l4 16h6 M4 20h6l4-16h6 M4 4v16 M20 4v16',
		heatmap:
			'M12 22c4.4 0 7-2.9 7-6.5 0-3-1.7-5-3.5-7-.5 2-1.5 3-2.5 3.5.5-3-1-6.5-4-9 .3 3-1 4.5-2.5 6.5C5 11.5 5 13.7 5 15.5 5 19.1 7.6 22 12 22z',
		compass: 'M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0z M15.8 8.2l-2.3 5.3-5.3 2.3 2.3-5.3z',
		analysis: 'M3 3v18h18 M7 14l4-4 3 3 5-6',
		stats: 'M6 20v-8 M12 20V6 M18 20v-5',
		records:
			'M8 21h8 M12 17v4 M7 4h10v5a5 5 0 0 1-10 0z M7 6H4.5a2.5 2.5 0 0 0 2.6 4 M17 6h2.5a2.5 2.5 0 0 1-2.6 4',
		segments: 'm8 3 4 8 5-5 5 15H2L8 3',

		health:
			'M19.5 12.6 12 20l-7.5-7.4A5 5 0 1 1 12 6.3a5 5 0 1 1 7.5 6.3 M6.7 12h1.5l1.3-2 2.5 4.5 1.3-2h2',
		connections:
			'M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7.1-7.1L12 4.8 M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7.1 7.1L12 19.2',
		gear: 'M5.5 17.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7z M18.5 17.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7z M5.5 14l3-7h7l3 7 M8.5 7l1.5 3h6 M12 14V7',
		review:
			'M22 12h-6l-2 3h-4l-2-3H2 M5.4 5.1 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.4-6.9A2 2 0 0 0 16.8 4H7.2a2 2 0 0 0-1.8 1.1z',
		settings: 'M4 21v-7 M4 10V3 M12 21v-9 M12 8V3 M20 21v-5 M20 12V3 M2 14h4 M10 8h4 M18 16h4',
		admin: 'M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z',
		moderation: 'M4 15s1-1 4-1 5 2 8 2 4-1 4-1V3s-1 1-4 1-5-2-8-2-4 1-4 1z M4 22v-7',
		friends:
			'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2 M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0 M22 21v-2a4 4 0 0 0-3-4 M16 3.2a4 4 0 0 1 0 7.6',
		logout: 'M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4 M16 17l5-5-5-5 M21 12H9'
	};

	type NavItem = { href: string; label: string; icon: string };
	type NavGroup = { title: string; items: NavItem[] };

	const isAdmin = $derived(data.permissions?.includes('admin') ?? false);
	const accountItems = $derived<NavItem[]>([
		{ href: '/health', label: m.health_athlete_settings(), icon: icons.health },
		{ href: '/gear', label: m.nav_gear(), icon: icons.gear },
		{ href: '/connections', label: m.nav_connections(), icon: icons.connections },
		{ href: '/settings', label: m.nav_settings(), icon: icons.settings }
	]);
	const canModerate = $derived(data.permissions?.includes('moderate') ?? false);

	const groups = $derived<NavGroup[]>([
		{
			title: '',
			items: [
				{ href: '/', label: m.nav_overview(), icon: icons.overview },
				{ href: '/activities', label: m.nav_activities(), icon: icons.activities },
				{ href: '/routes/waasland', label: m.nav_waas_planner(), icon: icons.planner },
				{ href: '/heatmap', label: 'Heatmap', icon: icons.heatmap },
				{ href: '/analysis', label: m.nav_analysis(), icon: icons.analysis },
				{ href: '/stats', label: m.nav_stats(), icon: icons.stats },
				{ href: '/records', label: 'Records', icon: icons.records },
				{ href: '/segments', label: m.nav_segments(), icon: icons.segments },
				{ href: '/head-to-head', label: m.comparison_title(), icon: icons.friends },
				{ href: '/riders', label: m.friends_title(), icon: icons.friends }
			]
		},
		{
			title: 'Account',
			items: [
				...accountItems.slice(0, -1),
				{ href: '/review', label: 'Review queue', icon: icons.review },
				...accountItems.slice(-1)
			]
		},
		...(isAdmin
			? [{ title: 'Admin', items: [{ href: '/admin', label: m.nav_admin(), icon: icons.admin }] }]
			: canModerate
				? [
						{
							title: 'Admin',
							items: [{ href: '/admin/moderation', label: 'Moderation', icon: icons.moderation }]
						}
					]
				: [])
	]);

	type Destination = { label: string; icon: string; pages: [string, ...string[]] };
	const destinations = $derived<Destination[]>([
		{ label: m.nav_home(), icon: icons.overview, pages: ['/'] },
		{ label: m.nav_activities(), icon: icons.activities, pages: ['/activities', '/review'] },
		{
			label: m.nav_explore(),
			icon: icons.compass,
			pages: ['/heatmap', '/segments', '/routes/waasland']
		},
		{ label: m.nav_insights(), icon: icons.stats, pages: ['/stats', '/analysis', '/records'] },
		{ label: m.nav_social(), icon: icons.friends, pages: ['/riders', '/head-to-head'] }
	]);
	const subsectionLabels = $derived<Record<string, string>>({
		'/heatmap': m.member_heatmap(),
		'/segments': m.nav_segments(),
		'/routes/waasland': m.nav_route_planner(),
		'/stats': m.nav_stats(),
		'/analysis': m.nav_analysis(),
		'/records': m.member_records(),
		'/riders': m.friends_title(),
		'/head-to-head': m.comparison_title()
	});
	const profileItems = $derived([...accountItems, ...(groups[2]?.items ?? [])]);

	function isActive(href: string): boolean {
		const current = page.url.pathname;
		return current === href || (href !== '/' && current.startsWith(href + '/'));
	}

	const destination = $derived(
		destinations.find((item) => item.pages.some(isActive)) ??
			(isActive('/segment')
				? destinations[2]
				: isActive('/best-efforts')
					? destinations[3]
					: undefined)
	);

	let lastSubsection = $state<Record<string, string>>({});
	let profileMenu = $state<HTMLElement>();
	let brokenAvatar = $state('');
	afterNavigate(() => {
		profileMenu?.hidePopover?.();
		const section = destination;
		const path = section?.pages.find(isActive);
		if (section && path && path in subsectionLabels) {
			lastSubsection[section.pages[0]] = path;
		}
	});

	function subsectionURL(href: string) {
		if (href === '/head-to-head' && viewedMember) {
			const query = new URLSearchParams({
				viewing: viewedMember.username,
				second: viewedMember.username
			});
			return href + '?' + query;
		}
		if (
			![
				'/',
				'/activities',
				'/heatmap',
				'/segments',
				'/analysis',
				'/stats',
				'/records',
				'/head-to-head',
				'/gear',
				'/riders'
			].includes(href)
		)
			return href;
		return memberURL(href, viewedMember);
	}

	const initials = $derived(
		(data.user?.displayName?.trim() || data.user?.username || '')
			.split(/\s+/)
			.map((w: string) => w[0])
			.slice(0, 2)
			.join('')
			.toUpperCase()
	);
	const roleLabel = $derived(isAdmin ? 'Admin' : canModerate ? 'Moderator' : '');
</script>

{#snippet brand()}
	<a
		href={subsectionURL('/')}
		class="flex shrink-0 items-center gap-2 font-semibold tracking-tight"
	>
		<BrandMark />
		<span class="text-accent-300">{APP_NAME}</span>
	</a>
{/snippet}

{#snippet memberIndicator()}
	{#if viewedMember}
		<div
			class="flex min-w-0 max-w-full items-center rounded-lg border border-zinc-800 bg-panel pl-2 text-xs"
		>
			<span class="truncate text-accent-300">
				{m.member_header({ name: viewedMember.display_name || viewedMember.username })}
			</span>
			<button
				type="button"
				onclick={() => goto(personalDataURL(page.url, !!page.data.activityOwner), { reset: false })}
				aria-label={m.member_close()}
				class="grid size-10 shrink-0 place-items-center rounded-lg text-base text-quiet hover:text-accent-300"
				>×</button
			>
		</div>
	{/if}
{/snippet}

{#snippet navList()}
	{#each groups as group (group.title)}
		{#if group.title}
			<div class="px-3 pb-1 pt-4 text-[11px] font-medium uppercase tracking-wider text-zinc-500">
				{group.title}
			</div>
		{/if}
		{#each group.items as item (item.href)}
			{@const active = isActive(item.href)}
			<a
				href={subsectionURL(item.href)}
				aria-current={active ? 'page' : undefined}
				class="flex items-center gap-2.5 rounded px-3 py-1.5 text-sm transition-colors {active
					? 'bg-accent-500/14 text-accent-300'
					: 'text-zinc-400 hover:text-zinc-100'}"
			>
				<svg
					class="h-[18px] w-[18px] shrink-0"
					class:text-accent-400={active}
					viewBox="0 0 24 24"
					fill="none"
					stroke="currentColor"
					stroke-width="1.8"
					stroke-linecap="round"
					stroke-linejoin="round"
				>
					<path d={item.icon} />
				</svg>
				<span class="truncate">{item.label}</span>
			</a>
		{/each}
	{/each}
{/snippet}

{#snippet userCard()}
	<div class="border-t border-zinc-800 p-3">
		<div class="flex items-center gap-2.5 rounded-md bg-zinc-800/60 px-2.5 py-2">
			<span
				class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-accent-500/20 text-xs font-semibold text-accent-300"
			>
				{initials}
			</span>
			<span class="min-w-0 flex-1">
				<span class="block truncate text-sm text-zinc-200">{data.user?.displayName}</span>
				{#if roleLabel}
					<span class="block text-[11px] text-zinc-500">{roleLabel}</span>
				{/if}
			</span>
			<form method="POST" action="/auth/logout">
				<button
					type="submit"
					class="rounded p-1.5 text-zinc-500 transition-colors hover:text-accent-300"
					aria-label={m.menu_logout()}
					title={m.menu_logout()}
				>
					<svg
						class="h-4 w-4"
						viewBox="0 0 24 24"
						fill="none"
						stroke="currentColor"
						stroke-width="1.8"
						stroke-linecap="round"
						stroke-linejoin="round"
					>
						<path d={icons.logout} />
					</svg>
				</button>
			</form>
		</div>
	</div>
{/snippet}

{#snippet navIcon(icon: string, className: string)}
	<svg
		class={className}
		aria-hidden="true"
		viewBox="0 0 24 24"
		fill="none"
		stroke="currentColor"
		stroke-width="1.8"
		stroke-linecap="round"
		stroke-linejoin="round"><path d={icon} /></svg
	>
{/snippet}

<LoadingBar />

<div class="flex min-h-dvh" class:mobile-shell={!!data.user}>
	{#if data.user}
		<!-- Desktop sidebar -->
		<aside
			class="sticky top-0 flex h-dvh w-56 shrink-0 flex-col border-r border-zinc-800 bg-zinc-900/80 max-md:hidden"
		>
			<div class="flex items-center px-4 py-4">
				{@render brand()}
			</div>
			<nav class="flex-1 overflow-y-auto px-2 pb-4">
				{@render navList()}
			</nav>
			{@render userCard()}
		</aside>
	{/if}

	<div class="flex min-w-0 flex-1 flex-col">
		{#if data.user}
			{#if viewedMember}
				<header class="hidden justify-end border-b border-zinc-800 px-6 py-1.5 md:flex">
					{@render memberIndicator()}
				</header>
			{/if}
			<header
				class="mobile-topbar flex items-center justify-between gap-2 border-b border-zinc-800 bg-zinc-900 px-4 md:hidden"
			>
				{@render brand()}
				{@render memberIndicator()}
				<button
					type="button"
					class="grid size-11 shrink-0 place-items-center rounded-full"
					popovertarget="mobile-profile"
					aria-label={m.nav_profile()}
				>
					<span
						class="grid size-8 place-items-center overflow-hidden rounded-full bg-accent-500/12 text-xs font-bold text-accent-300 ring-1 ring-accent-500/20"
						aria-hidden="true"
					>
						{#if data.user.avatarUrl && brokenAvatar !== data.user.avatarUrl}
							<img
								src={data.user.avatarUrl}
								alt=""
								class="size-full object-cover"
								referrerpolicy="no-referrer"
								onerror={() => (brokenAvatar = data.user?.avatarUrl ?? '')}
							/>
						{:else}
							{initials}
						{/if}
					</span>
				</button>
				<div
					id="mobile-profile"
					popover="auto"
					bind:this={profileMenu}
					class="mobile-profile rounded-2xl border border-zinc-800 bg-zinc-900 p-1.5 shadow-xl md:hidden"
				>
					<div class="border-b border-zinc-800 px-3 py-2.5">
						<p class="truncate text-sm font-semibold">
							{data.user.displayName || data.user.username}
						</p>
						<p class="truncate text-xs text-quiet">@{data.user.username}</p>
					</div>
					<nav aria-label={m.nav_profile()} class="mt-1">
						{#each profileItems as item (item.href)}
							<a
								href={item.href}
								aria-current={isActive(item.href) ? 'page' : undefined}
								class="flex min-h-11 items-center gap-3 rounded-lg px-3 text-sm text-zinc-300 hover:bg-zinc-800/60 aria-[current=page]:bg-accent-500/10 aria-[current=page]:text-accent-300"
							>
								{@render navIcon(item.icon, 'size-[18px]')}
								{item.label}
							</a>
						{/each}
					</nav>
					<form method="POST" action="/auth/logout" class="mt-1 border-t border-zinc-800 pt-1">
						<button
							type="submit"
							class="flex min-h-11 w-full items-center gap-3 rounded-lg px-3 text-sm text-zinc-300 hover:bg-zinc-800/60"
						>
							{@render navIcon(icons.logout, 'size-[18px]')}
							{m.menu_logout()}
						</button>
					</form>
				</div>
			</header>
			{#if destination && destination.pages[0] in subsectionLabels}
				<nav
					aria-label={destination.label}
					class="mobile-subnav flex gap-1 border-b border-zinc-800 bg-zinc-900 px-3 md:hidden"
				>
					{#each destination.pages as href (href)}
						{@const current =
							isActive(href) ||
							(href === '/segments' && isActive('/segment')) ||
							(href === '/records' && isActive('/best-efforts'))}
						<a
							href={subsectionURL(href)}
							aria-current={current ? 'page' : undefined}
							class="flex min-h-11 items-center justify-center border-b-2 border-transparent px-3 text-sm font-semibold text-quiet aria-[current=page]:border-accent-500 aria-[current=page]:text-accent-300"
						>
							{subsectionLabels[href]}
						</a>
					{/each}
				</nav>
			{/if}
		{:else}
			<!-- Logged-out header (login, signup, shared/public pages) -->
			<header class="border-b border-zinc-800 bg-zinc-900/80 backdrop-blur">
				<div class="mx-auto flex max-w-6xl items-center gap-3 px-6 py-3 max-md:px-4">
					{@render brand()}
					<span class="text-xs text-zinc-500 max-md:hidden">{m.app_subtitle()}</span>
					<a href="/routes/waasland" class="ml-auto text-sm text-accent-300 hover:underline"
						>{m.nav_waas_planner()}</a
					>
					<a
						href="/login"
						class="ml-auto rounded border border-accent-500 bg-accent-500/20 px-3 py-1.5 text-xs text-accent-300 hover:bg-accent-500/30"
					>
						{m.menu_signin()}
					</a>
				</div>
			</header>
		{/if}

		{#if data.user && data.user.email && !data.user.emailVerified}
			<div class="border-b border-amber-700/40 bg-amber-950/30 px-6 py-2 text-xs text-amber-200">
				<div class="mx-auto flex w-full max-w-6xl flex-wrap items-center gap-x-3 gap-y-1">
					<span>Please verify your email address ({data.user.email}).</span>
					{#if resendState === 'sent'}
						<span class="text-emerald-300">Verification email sent — check your inbox.</span>
					{:else if resendState === 'error'}
						<span class="text-red-300">Couldn't send — try again shortly.</span>
					{:else}
						<button
							type="button"
							disabled={resendState === 'sending'}
							onclick={resendVerification}
							class="rounded border border-amber-700/50 px-2 py-0.5 text-amber-200 hover:border-amber-500 hover:text-amber-100 disabled:opacity-50"
						>
							{resendState === 'sending' ? 'Sending…' : 'Resend verification'}
						</button>
					{/if}
				</div>
			</div>
		{/if}

		<main class="mx-auto w-full min-w-0 max-w-6xl flex-1 px-6 py-8 max-md:px-3.5 max-md:py-3">
			{#key member?.id}
				{@render children()}
			{/key}
		</main>
	</div>
</div>

{#if data.user}
	<nav
		aria-label={m.nav_primary()}
		class="mobile-tabbar fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 border-t border-zinc-800 bg-zinc-900/95 backdrop-blur-xl md:hidden"
	>
		{#each destinations as item (item.pages[0])}
			{@const root = item.pages[0]}
			<a
				href={subsectionURL(lastSubsection[root] ?? root)}
				aria-current={destination?.pages[0] === root ? 'page' : undefined}
				class="mobile-tab flex min-h-14 min-w-0 flex-col items-center justify-center gap-0.5 rounded-lg font-semibold text-quiet aria-[current=page]:text-accent-300"
			>
				<span class="mobile-tab-icon grid h-7 w-12 place-items-center rounded-full">
					{@render navIcon(item.icon, 'size-[21px]')}
				</span>
				<span class="text-[11px] leading-4">{item.label}</span>
			</a>
		{/each}
	</nav>
{/if}
