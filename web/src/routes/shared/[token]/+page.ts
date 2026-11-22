import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, parent, url }) => {
	const { user } = await parent();
	if (!user) redirect(302, `/login?redirect=${encodeURIComponent(url.pathname)}`);
	return { token: params.token };
};
