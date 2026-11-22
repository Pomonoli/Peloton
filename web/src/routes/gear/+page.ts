import { loadMemberPage } from '#lib/member.js';
import { redirect } from '@sveltejs/kit';
import { connectClients, isUnauthenticated } from '#lib/connect.js';
import { ActivityType } from '#lib/proto/cairn/v1/activity_pb.js';
import { enumName } from '#lib/proto-helpers.js';
import { gearToView, refToView } from '#lib/gear.js';
import type { PageLoad } from './$types';

export type GearDefaultView = { activityType: string; gearId: string };

export const load: PageLoad = async (event) => {
	const { fetch, url } = event;
	const { member } = await loadMemberPage(event);
	const clients = connectClients(fetch, url.origin);
	try {
		const [gear, pending, defaults] = await Promise.all([
			clients.gear.listGear({
				includeRetired: true,
				...(member ? { member: member.username } : {})
			}),
			member ? null : clients.gear.listPendingGearRefs({}),
			member ? null : clients.gear.listGearDefaults({})
		]);
		return {
			member,
			gear: gear.gear.map(gearToView),
			pending: (pending?.refs ?? []).map(refToView),
			defaults: (defaults?.defaults ?? []).map((d) => ({
				activityType: enumName(ActivityType, d.activityType),
				gearId: d.gearId
			})) as GearDefaultView[]
		};
	} catch (err) {
		if (isUnauthenticated(err)) redirect(302, '/login');
		throw err;
	}
};
