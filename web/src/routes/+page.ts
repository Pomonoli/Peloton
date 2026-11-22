import { loadMemberPage } from '#lib/member.js';
import type { PageLoad } from './$types';

// The dashboard data comes from /api/overview, fetched client-side. This loader
// just threads the user through from the layout.
export const load: PageLoad = async (event) => {
	const { user, features } = await event.parent();
	const subject = event.url.searchParams.get('member')
		? await loadMemberPage(event)
		: { member: null };
	return { user, features, ...subject };
};
