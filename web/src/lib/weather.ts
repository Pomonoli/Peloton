interface Weather {
	temperature_2m: number;
	wind_speed_10m: number;
	wind_direction_10m: number;
	fallback: boolean;
}

const fallback = { latitude: 51.165, longitude: 4.139, fallback: true };
export const weatherRefreshMs = 20 * 60_000;
let locationRequest: Promise<typeof fallback> | undefined;
let pending: Promise<Weather> | undefined;
let cache: { weather: Weather; expires: number } | undefined;

function viewerLocation() {
	return (locationRequest ??= new Promise<typeof fallback>((resolve) => {
		if (!navigator.geolocation) {
			resolve(fallback);
			return;
		}
		const timeout = setTimeout(() => resolve(fallback), 5000);
		navigator.geolocation.getCurrentPosition(
			({ coords }) => {
				clearTimeout(timeout);
				resolve({
					latitude: Math.round(coords.latitude * 100) / 100,
					longitude: Math.round(coords.longitude * 100) / 100,
					fallback: false
				});
			},
			() => {
				clearTimeout(timeout);
				resolve(fallback);
			},
			{ timeout: 5000, maximumAge: 60 * 60_000 }
		);
	}));
}

export function compassDirection(degrees: number) {
	const directions = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];
	return directions[((Math.round(degrees / 45) % 8) + 8) % 8] ?? directions[0]!;
}

export async function currentWeather(): Promise<Weather> {
	if (cache && cache.expires > Date.now()) return cache.weather;
	return (pending ??= (async () => {
		try {
			const location = await viewerLocation();
			const url = new URL('https://api.open-meteo.com/v1/forecast');
			url.search = new URLSearchParams({
				latitude: String(location.latitude),
				longitude: String(location.longitude),
				current: 'temperature_2m,wind_speed_10m,wind_direction_10m',
				temperature_unit: 'celsius',
				wind_speed_unit: 'kmh'
			}).toString();
			const response = await fetch(url, {
				signal: AbortSignal.timeout(8000),
				referrerPolicy: 'no-referrer'
			});
			if (!response.ok) throw new Error('Weather request failed');
			const { current } = await response.json();
			if (
				!Number.isFinite(current?.temperature_2m) ||
				!Number.isFinite(current?.wind_speed_10m) ||
				!Number.isFinite(current?.wind_direction_10m)
			)
				throw new Error('Missing current weather');
			const weather = { ...current, fallback: location.fallback } as Weather;
			cache = { weather, expires: Date.now() + weatherRefreshMs };
			return weather;
		} finally {
			pending = undefined;
		}
	})());
}
