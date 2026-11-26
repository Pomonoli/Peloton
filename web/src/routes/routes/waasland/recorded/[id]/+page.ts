import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, params, url }) => {
	if (!(await parent()).user) {
		redirect(302, '/login?redirect=' + encodeURIComponent(url.pathname));
	}
	return { routeId: params.id };
};
