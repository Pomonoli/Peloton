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
	import Icon from '#lib/components/Icon.svelte';
	import type { IconName } from '#lib/icons.js';

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

	type NavItem = { href: string; label: string; icon: IconName };
	type NavGroup = { title: string; items: NavItem[] };

	const isAdmin = $derived(data.permissions?.includes('admin') ?? false);
	const accountItems = $derived<NavItem[]>([
		{ href: '/health', label: m.health_athlete_settings(), icon: 'health' },
		{ href: '/gear', label: m.nav_gear(), icon: 'gear' },
		{ href: '/connections', label: m.nav_connections(), icon: 'connections' },
		{ href: '/settings', label: m.nav_settings(), icon: 'settings' }
	]);
	const canModerate = $derived(data.permissions?.includes('moderate') ?? false);

	const groups = $derived<NavGroup[]>([
		{
			title: '',
			items: [
				{ href: '/', label: m.nav_home(), icon: 'overview' },
				{ href: '/activities', label: m.nav_activities(), icon: 'activities' }
			]
		},
		{
			title: m.nav_explore(),
			items: [
				{ href: '/heatmap', label: m.member_heatmap(), icon: 'heatmap' },
				{ href: '/segments', label: m.nav_segments(), icon: 'segments' },
				{ href: '/explore/routes', label: m.routes_title(), icon: 'routes' },
				{ href: '/routes/waasland', label: m.nav_route_planner(), icon: 'planner' }
			]
		},
		{
			title: m.nav_insights(),
			items: [
				{ href: '/stats', label: m.nav_stats(), icon: 'stats' },
				{ href: '/analysis', label: m.nav_analysis(), icon: 'analysis' },
				{ href: '/records', label: m.member_records(), icon: 'records' }
			]
		},
		{
			title: m.nav_social(),
			items: [
				{ href: '/riders', label: m.friends_title(), icon: 'friends' },
				{ href: '/head-to-head', label: m.comparison_title(), icon: 'swap' }
			]
		},
		{
			title: 'Account',
			items: [
				...accountItems.slice(0, -1),
				{ href: '/review', label: 'Review queue', icon: 'review' },
				...accountItems.slice(-1)
			]
		},
		...(isAdmin
			? [
					{
						title: 'Admin',
						items: [{ href: '/admin', label: m.nav_admin(), icon: 'admin' as const }]
					}
				]
			: canModerate
				? [
						{
							title: 'Admin',
							items: [
								{ href: '/admin/moderation', label: 'Moderation', icon: 'moderation' as const }
							]
						}
					]
				: [])
	]);

	type Destination = { label: string; icon: IconName; pages: [string, ...string[]] };
	const destinations = $derived<Destination[]>([
		{ label: m.nav_home(), icon: 'overview', pages: ['/'] },
		{ label: m.nav_activities(), icon: 'activities', pages: ['/activities', '/review'] },
		{
			label: m.nav_explore(),
			icon: 'compass',
			pages: ['/heatmap', '/segments', '/explore/routes', '/routes/waasland']
		},
		{ label: m.nav_insights(), icon: 'stats', pages: ['/stats', '/analysis', '/records'] },
		{ label: m.nav_social(), icon: 'friends', pages: ['/riders', '/head-to-head'] }
	]);
	const subsectionLabels = $derived<Record<string, string>>({
		'/heatmap': m.member_heatmap(),
		'/segments': m.nav_segments(),
		'/explore/routes': m.routes_title(),
		'/routes/waasland': m.nav_route_planner(),
		'/stats': m.nav_stats(),
		'/analysis': m.nav_analysis(),
		'/records': m.member_records(),
		'/riders': m.friends_title(),
		'/head-to-head': m.comparison_title()
	});
	const profileItems = $derived([
		...accountItems,
		...(groups.find((group) => group.title === 'Admin')?.items ?? [])
	]);

	function isActive(href: string): boolean {
		const current = page.url.pathname;
		return (
			current === href ||
			(href !== '/' && current.startsWith(href + '/')) ||
			(href === '/segments' && current.startsWith('/segment/')) ||
			(href === '/records' && current.startsWith('/best-efforts/'))
		);
	}

	const destination = $derived(destinations.find((item) => item.pages.some(isActive)));

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
	<a href={subsectionURL('/')} class="app-brand">
		<BrandMark />
		<span>{APP_NAME}</span>
	</a>
{/snippet}

{#snippet memberIndicator()}
	{#if viewedMember}
		<div class="flex min-w-0 max-w-full items-center surface pl-2 text-xs">
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
				class="sidebar-link"
			>
				<Icon name={item.icon} size={18} />
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
					<Icon name="logout" size={18} />
				</button>
			</form>
		</div>
	</div>
{/snippet}

<LoadingBar />

<a class="skip-link" href="#main-content">{m.skip_content()}</a>

<div class="flex min-h-dvh" class:mobile-shell={!!data.user}>
	{#if data.user}
		<!-- Desktop sidebar -->
		<aside class="app-sidebar sticky top-0 flex h-dvh w-56 shrink-0 flex-col max-md:hidden xl:w-60">
			<div class="flex items-center px-5 py-5">
				{@render brand()}
			</div>
			<nav aria-label={m.nav_primary()} class="flex-1 overflow-y-auto px-3 pb-4">
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
			<header class="mobile-topbar flex items-center justify-between gap-2 md:hidden">
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
								<Icon name={item.icon} size={18} />
								{item.label}
							</a>
						{/each}
					</nav>
					<form method="POST" action="/auth/logout" class="mt-1 border-t border-zinc-800 pt-1">
						<button
							type="submit"
							class="flex min-h-11 w-full items-center gap-3 rounded-lg px-3 text-sm text-zinc-300 hover:bg-zinc-800/60"
						>
							<Icon name="logout" size={18} />
							{m.menu_logout()}
						</button>
					</form>
				</div>
			</header>
			{#if destination && destination.pages[0] in subsectionLabels && !isActive('/segment')}
				<nav aria-label={destination.label} class="mobile-subnav md:hidden">
					{#each destination.pages as href (href)}
						{@const current =
							isActive(href) ||
							(href === '/segments' && isActive('/segment')) ||
							(href === '/records' && isActive('/best-efforts'))}
						<a
							href={subsectionURL(href)}
							aria-current={current ? 'page' : undefined}
							class="tab-item"
						>
							{subsectionLabels[href]}
						</a>
					{/each}
				</nav>
			{/if}
		{:else}
			<!-- Logged-out header (login, signup, shared/public pages) -->
			<header class="border-b border-zinc-800 bg-zinc-900/80 backdrop-blur">
				<div class="mx-auto flex max-w-6xl flex-wrap items-center gap-2 px-4 py-2 md:px-6">
					{@render brand()}
					<span class="text-xs text-zinc-500 max-md:hidden">{m.app_subtitle()}</span>
					<a
						href="/routes/waasland"
						class="ml-auto inline-flex min-h-11 items-center rounded-md px-1 text-xs font-semibold text-accent-300 hover:underline sm:text-sm"
						>{m.nav_waas_planner()}</a
					>
					{#if !isActive('/login')}
						<a href="/login" class="action action-primary">{m.menu_signin()}</a>
					{/if}
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

		<main id="main-content" tabindex="-1" class="app-content">
			{#key member?.id}
				{@render children()}
			{/key}
		</main>
	</div>
</div>

{#if data.user}
	<nav
		aria-label={m.nav_primary()}
		class="mobile-tabbar fixed inset-x-0 bottom-0 z-40 grid grid-cols-5 md:hidden"
	>
		{#each destinations as item (item.pages[0])}
			{@const root = item.pages[0]}
			<a
				href={subsectionURL(lastSubsection[root] ?? root)}
				aria-current={destination?.pages[0] === root ? 'page' : undefined}
				class="mobile-tab flex min-h-14 min-w-0 flex-col items-center justify-center gap-0.5 rounded-lg font-semibold text-quiet aria-[current=page]:text-accent-300"
			>
				<span class="mobile-tab-icon grid h-7 w-12 place-items-center rounded-full">
					<Icon name={item.icon} size={21} />
				</span>
				<span class="text-[11px] leading-4">{item.label}</span>
			</a>
		{/each}
	</nav>
{/if}
