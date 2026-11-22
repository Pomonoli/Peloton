import { expect, it, vi } from 'vitest';
import { findMember, loadMemberPage, memberLinks, memberProfile, memberURL } from './member';

const member = { id: 'member-id', username: 'a rider', display_name: 'A Rider' };

it('preserves activity filters when linking to a member', () => {
	expect(memberURL('/activities?type=ride', member)).toBe('/activities?type=ride&member=a%20rider');
	expect(memberURL('/gear', member)).toBe('/gear?member=a%20rider');
	expect(memberURL('/gear')).toBe('/gear');
	expect(memberProfile(member)).toBe('/u/a%20rider');
	expect(memberLinks(member).map(({ href }) => href)).toEqual([
		'/',
		'/activities',
		'/stats',
		'/records',
		'/analysis',
		'/heatmap',
		'/segments',
		'/athlete',
		'/health',
		'/gear'
	]);
	expect(memberLinks(member).every(({ url }) => url.endsWith('member=a%20rider'))).toBe(true);
});

it('loads the selected rider and treats selecting yourself as the usual self view', async () => {
	const fetch = vi.fn(async () => Response.json(member));
	const parent = vi.fn(async () => ({ user: { id: 'viewer' } }));
	const url = new URL('https://peloton.test/activities?member=a%20rider');
	expect(await loadMemberPage({ fetch, parent, url })).toEqual({ member });
	expect(fetch).toHaveBeenCalledWith('/api/members/a%20rider');
	expect(
		await loadMemberPage({ fetch, parent, url: new URL('https://peloton.test/activities') })
	).toEqual({ member: null });
	expect(await loadMemberPage({ fetch, parent: async () => ({ user: member }), url })).toEqual({
		member: null
	});
});

it('requires a current user and handles unavailable member cards', async () => {
	const url = new URL('https://peloton.test/activities?member=unknown');
	const fetch = vi.fn<typeof globalThis.fetch>();
	await expect(
		loadMemberPage({ fetch, parent: async () => ({ user: null }), url })
	).rejects.toMatchObject({ status: 302, location: '/login' });
	expect(fetch).not.toHaveBeenCalled();
	for (const [httpStatus, status] of [
		[401, 302],
		[404, 404],
		[503, 500]
	] as const) {
		fetch.mockResolvedValueOnce(new Response(null, { status: httpStatus }));
		await expect(findMember(fetch, 'unknown')).rejects.toMatchObject({ status });
	}
});
