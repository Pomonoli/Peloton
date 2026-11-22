import { error, redirect } from '@sveltejs/kit';
import { loadMemberPage } from '#lib/member.js';
import { m } from '#lib/paraglide/messages.js';
import { recordPath, type CommunityRecord } from '#lib/records.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async (event) => {
	const { member } = await loadMemberPage(event);
	const { type, metric, kind, value } = event.params;
	const bucket = { activity_type: type, metric, window_kind: kind, window_value: Number(value) };
	const response = await event.fetch('/api' + recordPath(bucket));
	if (response.status === 401) redirect(302, '/login');
	if (!response.ok) error(response.status, m.record_board_failed());
	const { records }: { records: CommunityRecord[] } = await response.json();
	return { member, bucket, records };
};
