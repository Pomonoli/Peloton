import { error } from '@sveltejs/kit';
import { connectClients } from '#lib/connect.js';
import { m } from '#lib/paraglide/messages.js';
import { StreamChannel } from '#lib/proto/cairn/v1/activity_pb.js';
import { streamToView } from '#lib/ride-stream.js';
import { analyzeTrack } from '#lib/ride-track.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ parent, fetch, url }) => {
	const { activity } = await parent();
	if (activity.type !== 'ride' || !activity.primaryStreamSourceId)
		error(404, m.ride_replay_no_gps());
	const client = connectClients(fetch, url.origin).activity;
	const response = await client
		.getActivityStream({
			activitySourceId: activity.primaryStreamSourceId,
			maxResolutionHz: 1,
			channels: [
				StreamChannel.LATITUDE,
				StreamChannel.LONGITUDE,
				StreamChannel.DISTANCE,
				StreamChannel.ALTITUDE,
				StreamChannel.SPEED
			]
		})
		.catch(() => error(503, m.ride_replay_load_failed()));
	const track = analyzeTrack(streamToView(response.stream)?.track ?? []);
	return { replayTrack: track };
};
