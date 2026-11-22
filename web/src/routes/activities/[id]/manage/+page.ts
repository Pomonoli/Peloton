import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, params }) => {
	if (!(await parent()).isOwner) redirect(302, `/activities/${params.id}`);
};
