export type Profile = 'climb' | 'cobbles' | 'flow' | 'gravel';
export type Departure = 'sn' | 'wm' | 'ti' | 'te';
export type Direction = 'auto' | 'cw' | 'ccw';
export type Point = [latitude: number, longitude: number, elevation: number, distance: number];
export interface Options {
	profile: Profile;
	start: Departure;
	length: number;
	tolerance: number;
	direction: Direction;
	places: string[];
	goal: 'max' | 'varied' | 'range' | 'profile';
	minGain: number;
	maxGain: number;
	laps: number;
	surface: 'smooth' | 'any';
	zones: Record<string, 'free' | 'include' | 'avoid'>;
}
export interface Place {
	id: string;
	name: string;
	point: number[];
	ferry?: boolean;
}
export interface Section {
	id: string;
	name: string;
	points: Point[];
	meters: number;
	gain: number;
	passes: number;
	kind?: string;
}
export interface Chunk {
	id: string;
	from: string;
	to: string;
	points: Point[];
	meters: number;
	gain: number;
	scheldeRound?: boolean;
	customRoute?: boolean;
	elevationSource?: string;
}
export interface Plan {
	chunks: Chunk[];
	meters: number;
	gain: number;
	profile: Profile;
	distanceOff: number;
	gainOff: number;
	reverse: number;
	repeat: number;
	selectedZones: string[];
	direction?: 'cw' | 'ccw';
	visits?: Place[];
}
export interface Ride extends Plan {
	points: Point[];
	streets: Section[];
	sections: Section[];
	climbs: Section[];
	matchedSurface: number;
	uniqueSurface: number;
	spine: Record<string, number>;
	events: { meters: number; kind: string }[];
	longestGap: number;
}
export type Routes = Record<string, Chunk>;
export interface Planner {
	findRoutes(options: Options, routes: Routes, circuits: string[][], standalone?: Routes): Plan[];
	assemble(plan: Plan): Ride;
	gpx(ride: Ride, start: Departure): string;
	decodePriorityRoutes(data: unknown): Routes;
	catalogOptions(data: unknown, options: Options, name: string): Routes;
	climbZones: { id: string; name: string }[];
	hubs: Record<Departure, string>;
	rideNames: Record<Profile, string>;
}
export interface Wind {
	speed: number;
	direction: number;
	compass: string;
	gusts: number;
	timestamp: number;
}
export interface RideOptions {
	places: Place[];
	departures: Record<Departure, number[]>;
	customize(
		plans: Plan[],
		options: Options,
		signal: AbortSignal,
		progress: (message: string) => void
	): Promise<Plan[]>;
	weather(start: Departure, signal: AbortSignal): Promise<Wind>;
	messagesFor(plan: Plan, options: Options): string[];
	visitPlaces(points: Point[], places: string[]): { meters: number }[];
	orientation(points: Point[]): 'cw' | 'ccw';
}
