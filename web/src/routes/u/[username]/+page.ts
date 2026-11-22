import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent }) => {
	const { user } = await parent();
	if (!user) redirect(302, '/login');
	return { user, username: params.username };
};
