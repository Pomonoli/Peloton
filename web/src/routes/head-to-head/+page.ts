import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, url }) => {
	const { user } = await parent();
	if (!user) redirect(302, '/login');
	return {
		user,
		first: url.searchParams.get('first') ?? '',
		member: url.searchParams.get('second') ?? url.searchParams.get('member') ?? ''
	};
};
