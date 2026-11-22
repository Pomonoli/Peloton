import { error, redirect } from '@sveltejs/kit';
import { m } from '#lib/paraglide/messages.js';

export interface MemberView {
	id: string;
	username: string;
	display_name: string;
}

export function memberURL(path: string, member?: MemberView | null) {
	if (!member) return path;
	const separator = path.includes('?') ? '&' : '?';
	return `${path}${separator}member=${encodeURIComponent(member.username)}`;
}

export function memberLinks(member?: MemberView | null) {
	return [
		{ href: '/', label: m.nav_overview() },
		{ href: '/activities', label: m.nav_activities() },
		{ href: '/stats', label: m.nav_stats() },
		{ href: '/records', label: m.member_records() },
		{ href: '/analysis', label: m.nav_analysis() },
		{ href: '/heatmap', label: m.member_heatmap() },
		{ href: '/segments', label: m.nav_segments() },
		...(member ? [{ href: '/head-to-head', label: m.comparison_title() }] : []),
		{ href: '/athlete', label: m.nav_athlete() },
		{ href: '/health', label: m.member_health() },
		{ href: '/gear', label: m.nav_gear() }
	].map((link) => ({ ...link, url: memberURL(link.href, member) }));
}

export function memberProfile(member: MemberView) {
	return `/u/${encodeURIComponent(member.username)}`;
}

export async function findMember(fetch: typeof globalThis.fetch, key: string) {
	const response = await fetch(`/api/members/${encodeURIComponent(key)}`);
	if (response.status === 401) redirect(302, '/login');
	if (response.status === 404) error(404, m.community_member_missing());
	if (!response.ok) error(500, m.community_profile_failed());
	return (await response.json()) as MemberView;
}

export async function loadMemberPage<T extends { user: { id: string } | null }>({
	fetch,
	url,
	parent
}: {
	fetch: typeof globalThis.fetch;
	url: URL;
	parent: () => Promise<T>;
}) {
	const data = await parent();
	const key = url.searchParams.get('member')?.trim();
	if (!data.user) redirect(302, '/login');
	const member = key ? await findMember(fetch, key) : null;
	return { member: member?.id === data.user.id ? null : member };
}
