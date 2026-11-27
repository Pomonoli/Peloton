export type FeedActivity = {
	id: string;
	title: string;
	type: string;
	discipline: string;
	start_time: string;
	timezone: string;
	moving_duration_s: number;
	elapsed_duration_s: number;
	avg_speed_mps: number | null;
	distance_m: number | null;
	elevation_gain_m: number | null;
	tss: number | null;
	start_place: string;
	gear_unset: boolean;
	rider_id?: string;
};
