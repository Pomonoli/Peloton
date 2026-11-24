<script lang="ts">
	import { onMount } from 'svelte';
	import { intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';
	import { compassDirection, currentWeather, weatherRefreshMs } from '#lib/weather.js';

	let weather = $state<Awaited<ReturnType<typeof currentWeather>>>();
	let failed = $state(false);
	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 0 }));

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

<div role="status" class="flex flex-wrap items-baseline gap-x-2 text-xs text-quiet">
	{#if weather}
		<span class="text-base font-semibold text-accent-300"
			>{m.weather_temperature({ temperature: number.format(weather.temperature_2m) })}</span
		>
		<span
			>{m.weather_wind({
				direction: compassDirection(weather.wind_direction_10m),
				speed: number.format(weather.wind_speed_10m)
			})}</span
		>
		{#if weather.fallback}<span>{m.weather_fallback()}</span>{/if}
		<a href="https://open-meteo.com/" class="underline">Open-Meteo</a>
	{:else}
		<span>{failed ? m.weather_unavailable() : m.weather_loading()}</span>
	{/if}
</div>
