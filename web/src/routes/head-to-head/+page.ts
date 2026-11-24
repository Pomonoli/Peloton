import { redirect } from '@sveltejs/kit';
import { findMember } from '#lib/member.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, parent, url }) => {
	const { user } = await parent();
	if (!user) redirect(302, '/login');
	const viewing = url.searchParams.get('viewing');
	const viewed = viewing ? await findMember(fetch, viewing) : null;
	return {
		user,
		first: url.searchParams.get('first') ?? '',
		member: url.searchParams.get('second') ?? url.searchParams.get('member') ?? '',
		viewedMember: viewed?.id === user.id ? null : viewed
	};
};
