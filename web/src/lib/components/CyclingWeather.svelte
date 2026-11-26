<script lang="ts">
	import { onMount } from 'svelte';
	import { intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import { currentWeather, weatherRefreshMs, windToward } from '#lib/weather.js';

	let weather = $state<Awaited<ReturnType<typeof currentWeather>>>();
	let failed = $state(false);
	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 0 }));
	const compassLabels = [
		m.weather_north,
		m.weather_northeast,
		m.weather_east,
		m.weather_southeast,
		m.weather_south,
		m.weather_southwest,
		m.weather_west,
		m.weather_northwest
	];
	// Open-Meteo uses WMO weather codes.
	const conditions = [
		{ codes: [0], icon: '☀️', nightIcon: '🌙', label: m.weather_clear },
		{ codes: [1, 2], icon: '🌤️', nightIcon: '☁️', label: m.weather_partly_cloudy },
		{ codes: [3], icon: '☁️', label: m.weather_overcast },
		{ codes: [45, 48], icon: '🌫️', label: m.weather_fog },
		{
			codes: [51, 53, 55, 56, 57, 61, 63, 65, 66, 67, 80, 81, 82],
			icon: '🌧️',
			label: m.weather_rain
		},
		{ codes: [71, 73, 75, 77, 85, 86], icon: '🌨️', label: m.weather_snow },
		{ codes: [95, 96, 99], icon: '⛈️', label: m.weather_thunder }
	];
	const condition = $derived(
		conditions.find(
			({ codes }) => weather?.weather_code !== undefined && codes.includes(weather.weather_code)
		)
	);

	onMount(() => {
		let mounted = true;
		async function refresh() {
			try {
				const current = await currentWeather();
				if (mounted) {
					weather = current;
					failed = false;
				}
			} catch {
				if (mounted) {
					weather = undefined;
					failed = true;
				}
			}
		}
		void refresh();
		const timer = setInterval(refresh, weatherRefreshMs);
		return () => {
			mounted = false;
			clearInterval(timer);
		};
	});
</script>

<div
	role="status"
	class="surface flex min-h-18 w-28 flex-col items-end justify-center px-2 py-2 text-right text-xs text-quiet sm:w-34 sm:px-3"
>
	{#if weather}
		{@const bearing = windToward(weather.wind_direction_10m)}
		{@const direction = compassLabels[Math.round(bearing / 45) % 8] ?? m.weather_north}
		{@const speed = number.format(weather.wind_speed_10m)}
		<div class="flex flex-col items-end gap-0.5">
			<div class="flex items-center gap-1.5 whitespace-nowrap">
				{#if condition}
					<span role="img" aria-label={condition.label()} class="text-2xl leading-none"
						>{(weather.is_day === 0 && condition.nightIcon) || condition.icon}</span
					>
				{/if}
				<span
					class="text-lg leading-none font-semibold text-zinc-100 tabular-nums min-[22.5rem]:text-xl sm:text-2xl"
				>
					{m.weather_temperature({ temperature: number.format(weather.temperature_2m) })}
				</span>
			</div>
			<span
				role="img"
				aria-label={m.weather_wind({ direction: direction(), speed })}
				class="flex items-center gap-1 font-medium whitespace-nowrap text-zinc-300 tabular-nums sm:text-sm"
			>
				<svg
					aria-hidden="true"
					class="size-3.5 text-accent-300"
					style:rotate={`${bearing}deg`}
					viewBox="0 0 16 16"
					fill="none"
					stroke="currentColor"
					stroke-width="1.6"
					stroke-linecap="round"
					stroke-linejoin="round"
				>
					<path d="M8 13V3m-4 4 4-4 4 4" />
				</svg>
				{m.weather_wind_speed({ speed })}
			</span>
		</div>
		{#if weather.fallback}
			<span class="mt-1 text-[11px] leading-tight">{m.weather_fallback()}</span>
		{/if}
	{:else}
		<span>{failed ? m.weather_unavailable() : m.weather_loading()}</span>
	{/if}
</div>
{#if weather}
	<a
		href="https://open-meteo.com/en/licence"
		class="mr-2 text-[11px] leading-tight text-quiet underline underline-offset-2 sm:mr-3"
		>Open-Meteo</a
	>
{/if}
