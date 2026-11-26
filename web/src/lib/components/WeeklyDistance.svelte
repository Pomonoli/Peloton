<script lang="ts">
	import { intlLocale } from '#lib/format.js';
	import { m } from '#lib/paraglide/messages.js';

	let { weeks }: { weeks: { start: string; distance_m: number }[] } = $props();
	let active = $state<string | null>(null);
	const number = $derived(new Intl.NumberFormat(intlLocale(), { maximumFractionDigits: 3 }));
	const date = $derived(
		new Intl.DateTimeFormat(intlLocale(), {
			day: 'numeric',
			month: 'short',
			timeZone: 'UTC'
		})
	);
	const tick = $derived.by(() => {
		const target = Math.max(0.1, ...weeks.map((week) => week.distance_m / 4000));
		const base = 10 ** Math.floor(Math.log10(target));
		return ([1, 2, 5, 10].find((n) => n * base >= target) ?? 10) * base;
	});
	const points = $derived(
		weeks.map((week) => {
			const start = new Date(week.start + 'T00:00:00Z');
			const end = new Date(start.getTime() + 6 * 86_400_000);
			const label = date.format(start);
			return {
				...week,
				label,
				detail: m.overview_weekly_value({
					start: label,
					end: date.format(end),
					distance: number.format(week.distance_m / 1000)
				})
			};
		})
	);
	const detail = $derived(points.find((point) => point.start === active)?.detail ?? '');
	const hasDistance = $derived(weeks.some((week) => week.distance_m > 0));
</script>

<section class="min-w-0 surface p-3 sm:p-4" aria-labelledby="weekly-distance">
	<div class="flex flex-wrap items-baseline justify-between gap-x-3">
		<h2 id="weekly-distance" class="text-sm font-bold">{m.overview_weekly_distance()}</h2>
		<p class="text-xs text-quiet">{m.overview_weekly_calendar()}</p>
	</div>
	<p class="mb-0.5 min-h-5 text-right text-xs text-accent-300" aria-live="polite">
		{detail || (!hasDistance ? m.overview_no_cycling_distance() : '')}
	</p>
	<div class="flex h-32 gap-2 sm:h-43 xl:h-49">
		<div
			class="flex shrink-0 flex-col justify-between text-right text-xs tabular-nums text-quiet"
			aria-hidden="true"
		>
			{#each [4, 3, 2, 1, 0] as multiplier (multiplier)}
				<span>{number.format(tick * multiplier)} km</span>
			{/each}
		</div>
		<div class="relative grid min-w-0 flex-1 grid-cols-12 items-stretch gap-1 sm:gap-2">
			<div
				class="pointer-events-none absolute inset-x-0 top-2 bottom-2 flex flex-col justify-between"
				aria-hidden="true"
			>
				{#each [0, 1, 2, 3, 4] as line (line)}<div class="border-t border-zinc-800"></div>{/each}
			</div>
			{#each points as point (point.start)}
				<button
					type="button"
					aria-label={point.detail}
					onpointerenter={() => (active = point.start)}
					onpointerleave={() => {
						if (active === point.start) active = null;
					}}
					onfocus={() => (active = point.start)}
					onclick={() => (active = point.start)}
					class="group z-10 flex min-w-0 items-end justify-center rounded-sm pt-2 pb-2 focus-visible:outline-2 focus-visible:outline-accent-400"
				>
					<span
						class="distance-bar w-full max-w-12 rounded-t-[4px]"
						class:selected={point.start === active}
						style:height={point.distance_m > 0
							? `${(point.distance_m / (4000 * tick)) * 100}%`
							: '2px'}
					></span>
				</button>
			{/each}
		</div>
	</div>
	<div
		class="mt-2 grid grid-cols-6 gap-1 pl-12 text-center text-[10px] text-quiet sm:grid-cols-12"
		aria-hidden="true"
	>
		{#each points as point, i (point.start)}
			<span class={i % 2 === 0 ? 'hidden sm:inline' : ''}>{point.label}</span>
		{/each}
	</div>
</section>

<style>
	.distance-bar {
		background: color-mix(in srgb, var(--accent-primary) 80%, var(--surface-card));
	}
	.distance-bar.selected {
		background: var(--accent-hover);
	}
</style>
