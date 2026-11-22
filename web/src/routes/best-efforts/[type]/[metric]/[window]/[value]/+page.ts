import { loadMemberPage } from '#lib/member.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async (event) => {
	const subject = await loadMemberPage(event);
	const { params } = event;
	return {
		...subject,
		type: params.type,
		metric: params.metric,
		window: params.window,
		value: Number(params.value)
	};
};
